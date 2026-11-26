// Copyright 2026 The Agent Substrate Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workersync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.ate.dev/substrate/api/ateapi/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

// BusyWorkerLabel is "true" on a worker pod while BusyMarker marks it. The
// marker reads it back to know which pods it marked.
const BusyWorkerLabel = "ate.dev/busy"

// busyFieldManager owns the marking on the pod. Releasing the marking under
// server-side apply drops only what this manager set, so an annotation the
// WorkerPool's pod template also sets stays on the pod.
const busyFieldManager = "ate-controller-busy-worker"

// BusyMarker sets a fixed set of annotations on the worker pods that host
// Actors, and removes them once the pods host none. A cluster autoscaler that
// honors a pod annotation, such as Karpenter's karpenter.sh/do-not-disrupt,
// then leaves a node alone while a worker on it runs an Actor, and still
// consolidates the nodes of idle workers.
//
// A worker counts as busy while its status reports allocated Actors. An Actor
// placed on an unmarked worker stays unprotected for at most one period.
type BusyMarker struct {
	client      ateapi.ControlClient
	pods        corev1client.PodsGetter
	podInformer cache.SharedIndexInformer
	annotations map[string]string
	period      time.Duration
}

// NewBusyMarker creates a BusyMarker. podInformer is the worker pod informer
// from WorkerPodInformer. annotations must not be empty.
func NewBusyMarker(client ateapi.ControlClient, pods corev1client.PodsGetter, podInformer cache.SharedIndexInformer, annotations map[string]string, period time.Duration) *BusyMarker {
	return &BusyMarker{
		client:      client,
		pods:        pods,
		podInformer: podInformer,
		annotations: annotations,
		period:      period,
	}
}

// Start runs the marking every period until ctx is done, once the pod
// informer has synced.
func (m *BusyMarker) Start(ctx context.Context) {
	go func() {
		if !cache.WaitForCacheSync(ctx.Done(), m.podInformer.HasSynced) {
			slog.ErrorContext(ctx, "BusyMarker: failed to sync informer cache")
			return
		}
		wait.UntilWithContext(ctx, func(ctx context.Context) {
			if err := m.sync(ctx); err != nil {
				slog.WarnContext(ctx, "BusyMarker: sync failed, retrying next period", slog.Any("err", err))
			}
		}, m.period)
	}()
}

// sync converges the marking of every worker pod with the registry.
func (m *BusyMarker) sync(ctx context.Context) error {
	busy, err := m.busyPods(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, obj := range m.podInformer.GetStore().List() {
		pod := obj.(*corev1.Pod)
		if pod.DeletionTimestamp != nil {
			continue
		}
		want := busy[pod.UID]
		if want == (pod.Labels[BusyWorkerLabel] == "true") {
			continue
		}
		if err := m.mark(ctx, pod, want); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// busyPods returns the UIDs of the worker pods whose Worker hosts an Actor.
func (m *BusyMarker) busyPods(ctx context.Context) (map[types.UID]bool, error) {
	busy := map[types.UID]bool{}
	var pageToken string
	for {
		page, err := m.client.ListWorkers(ctx, &ateapi.ListWorkersRequest{Options: &ateapi.ListOptions{PageSize: 1000, PageToken: pageToken}})
		if err != nil {
			return nil, fmt.Errorf("listing workers: %w", err)
		}
		for _, w := range page.GetWorkers() {
			if w.GetStatus().GetAllocated().GetActors() > 0 {
				busy[types.UID(w.GetWorkerPodUid())] = true
			}
		}
		if page.GetNextPageToken() == "" {
			return busy, nil
		}
		pageToken = page.GetNextPageToken()
	}
}

// mark applies the marking to pod, or releases it. The apply names the pod's
// UID, so it never lands on a replacement pod of the same name.
func (m *BusyMarker) mark(ctx context.Context, pod *corev1.Pod, busy bool) error {
	ac := corev1ac.Pod(pod.Name, pod.Namespace).WithUID(pod.UID)
	if busy {
		ac.WithLabels(map[string]string{BusyWorkerLabel: "true"}).WithAnnotations(m.annotations)
	}
	slog.InfoContext(ctx, "BusyMarker: marking worker pod",
		slog.String("pod", pod.Namespace+"/"+pod.Name), slog.Bool("busy", busy))
	_, err := m.pods.Pods(pod.Namespace).Apply(ctx, ac, metav1.ApplyOptions{FieldManager: busyFieldManager, Force: true})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("applying marking to pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}
