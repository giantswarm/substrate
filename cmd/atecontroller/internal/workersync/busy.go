// Copyright 2026 Google LLC
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
	"fmt"
	"log/slog"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
)

// BusyWorkerLabel is set to "true" on a worker pod while it hosts at least one
// Actor, alongside the BusyMarker's annotations. It is what the marker reads
// back to know which pods it has marked, and lets an operator list the busy
// workers (kubectl get pods -l ate.dev/busy=true).
const BusyWorkerLabel = "ate.dev/busy"

// busyFieldManager owns the marking on the pod. The marking is applied with
// server-side apply, so dropping it releases only what this manager set: an
// annotation the WorkerPool's pod template also sets stays on the pod.
const busyFieldManager = "ate-controller-busy-worker"

// BusyMarker marks the worker pods that host Actors with a fixed set of
// annotations, and unmarks them once they host none. A cluster autoscaler
// that honors a pod annotation, such as Karpenter's
// karpenter.sh/do-not-disrupt, then leaves a node alone while one of its
// workers runs an Actor, and still consolidates the nodes of idle workers.
// The Actors in flight on a worker are lost when its pod goes, since a
// worker's SIGTERM stops them, so a voluntary disruption is best avoided
// while a worker is busy rather than handled by its drain.
//
// The marker reads the registry every period: a worker counts as busy while
// its status reports a non-zero allocated Actor count. An Actor placed on an
// unmarked worker stays unprotected for at most one period.
type BusyMarker struct {
	client      ateapipb.ControlClient
	pods        corev1client.PodsGetter
	podInformer cache.SharedIndexInformer
	annotations map[string]string
	period      time.Duration
}

// NewBusyMarker creates a BusyMarker. podInformer is the worker pod informer
// the WorkerPoolSyncer runs on (WorkerPodInformer); annotations must not be
// empty.
func NewBusyMarker(client ateapipb.ControlClient, pods corev1client.PodsGetter, podInformer cache.SharedIndexInformer, annotations map[string]string, period time.Duration) *BusyMarker {
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
			// Already going: the marking can no longer keep it.
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
	if len(errs) > 0 {
		return fmt.Errorf("marking %d worker pod(s), first: %w", len(errs), errs[0])
	}
	return nil
}

// busyPods returns the UIDs of the worker pods whose Worker hosts an Actor.
func (m *BusyMarker) busyPods(ctx context.Context) (map[types.UID]bool, error) {
	busy := map[types.UID]bool{}
	var pageToken string
	for {
		page, err := m.client.ListWorkers(ctx, &ateapipb.ListWorkersRequest{PageSize: 1000, PageToken: pageToken})
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
// UID, so it never lands on a same-named replacement.
func (m *BusyMarker) mark(ctx context.Context, pod *corev1.Pod, busy bool) error {
	ac := corev1ac.Pod(pod.Name, pod.Namespace).WithUID(pod.UID)
	if busy {
		ac.WithLabels(map[string]string{BusyWorkerLabel: "true"}).WithAnnotations(m.annotations)
	}
	slog.InfoContext(ctx, "BusyMarker: marking worker pod",
		slog.String("pod", pod.Namespace+"/"+pod.Name), slog.Bool("busy", busy))
	_, err := m.pods.Pods(pod.Namespace).Apply(ctx, ac, metav1.ApplyOptions{FieldManager: busyFieldManager, Force: true})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		// Gone or replaced: nothing left to mark.
		return nil
	}
	if err != nil {
		return fmt.Errorf("applying marking to pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}
