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
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

const (
	doNotDisrupt = "karpenter.sh/do-not-disrupt"
	busyTestPool = "pool1"
)

// setupBusyMarkerTest returns a marker over a fake Kubernetes holding pods,
// with its pod cache seeded from the same pods so sync can run synchronously.
func setupBusyMarkerTest(t *testing.T, api *fakeControl, pods ...*corev1.Pod) (*BusyMarker, *fake.Clientset, cache.Indexer) {
	t.Helper()
	k8s := fake.NewClientset()
	for _, p := range pods {
		if _, err := k8s.CoreV1().Pods(p.Namespace).Create(context.Background(), p.DeepCopy(), metav1.CreateOptions{}); err != nil {
			t.Fatalf("creating pod %s: %v", p.Name, err)
		}
	}
	_, informer := WorkerPodInformer(k8s)
	for _, p := range pods {
		seedPod(t, informer.GetIndexer(), p)
	}
	m := NewBusyMarker(api, k8s.CoreV1(), informer, map[string]string{doNotDisrupt: "true"}, time.Second)
	return m, k8s, informer.GetIndexer()
}

// putWorker registers a Worker for pod with the given allocated Actor count.
func putWorker(api *fakeControl, pod *corev1.Pod, actors int32) {
	api.put(&ateapipb.Worker{
		Metadata:        &ateapipb.ResourceMetadata{Name: string(pod.UID)},
		WorkerNamespace: pod.Namespace,
		WorkerPool:      busyTestPool,
		WorkerPod:       pod.Name,
		WorkerPodUid:    string(pod.UID),
		Status: &ateapipb.WorkerStatus{
			State:     ateapipb.WorkerState_WORKER_STATE_ACTIVE,
			Allocated: &ateapipb.WorkerResources{Actors: actors},
		},
	})
}

// livePod reads pod back from the fake Kubernetes and refreshes the marker's
// cache with it, as the informer would.
func livePod(t *testing.T, k8s *fake.Clientset, cached cache.Indexer, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	got, err := k8s.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting pod %s: %v", pod.Name, err)
	}
	if err := cached.Update(got); err != nil {
		t.Fatalf("updating cached pod %s: %v", pod.Name, err)
	}
	return got
}

func busyTestPod(name string, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			UID:         types.UID("uid-" + name),
			Labels:      map[string]string{workerPodLabel: busyTestPool},
			Annotations: annotations,
		},
	}
}

func TestBusyMarker_MarksBusyAndUnmarksIdle(t *testing.T) {
	ctx := context.Background()
	api := newFakeControl()
	busy := busyTestPod("busy", nil)
	idle := busyTestPod("idle", nil)
	putWorker(api, busy, 2)
	putWorker(api, idle, 0)
	m, k8s, cached := setupBusyMarkerTest(t, api, busy, idle)

	if err := m.sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got := livePod(t, k8s, cached, busy)
	if got.Annotations[doNotDisrupt] != "true" || got.Labels[BusyWorkerLabel] != "true" {
		t.Errorf("busy pod: annotations %v labels %v, want %s and %s marked", got.Annotations, got.Labels, doNotDisrupt, BusyWorkerLabel)
	}
	if got.Labels[workerPodLabel] != busyTestPool {
		t.Errorf("busy pod lost its pool label: %v", got.Labels)
	}
	if got := livePod(t, k8s, cached, idle); got.Annotations[doNotDisrupt] != "" || got.Labels[BusyWorkerLabel] != "" {
		t.Errorf("idle pod marked: annotations %v labels %v", got.Annotations, got.Labels)
	}

	// The worker's last Actor leaves: the marking goes, the pool label stays.
	putWorker(api, busy, 0)
	if err := m.sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got = livePod(t, k8s, cached, busy)
	if _, ok := got.Annotations[doNotDisrupt]; ok {
		t.Errorf("idle again: %s still set: %v", doNotDisrupt, got.Annotations)
	}
	if _, ok := got.Labels[BusyWorkerLabel]; ok {
		t.Errorf("idle again: %s still set: %v", BusyWorkerLabel, got.Labels)
	}
	if got.Labels[workerPodLabel] != busyTestPool {
		t.Errorf("idle again: pool label lost: %v", got.Labels)
	}
}

func TestBusyMarker_SkipsTerminatingPods(t *testing.T) {
	ctx := context.Background()
	api := newFakeControl()
	terminating := busyTestPod("terminating", nil)
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"test/keep"}
	putWorker(api, terminating, 1)
	m, k8s, _ := setupBusyMarkerTest(t, api, terminating)

	k8s.ClearActions()
	if err := m.sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	for _, a := range k8s.Actions() {
		if a.GetVerb() == "patch" {
			t.Errorf("terminating pod patched: %v", a)
		}
	}
}

func TestBusyMarker_ListErrorReturned(t *testing.T) {
	api := newFakeControl()
	api.setListHook(func(*ateapipb.ListWorkersRequest) error { return context.DeadlineExceeded })
	m, _, _ := setupBusyMarkerTest(t, api, busyTestPod("p", nil))
	if err := m.sync(context.Background()); err == nil {
		t.Fatal("sync succeeded on a failing ListWorkers")
	}
}

func TestBusyMarker_UnmarkKeepsTemplateAnnotation(t *testing.T) {
	ctx := context.Background()
	api := newFakeControl()
	// The pool's pod template already sets the annotation: releasing the
	// marking leaves it, since the marker never owned it alone.
	pod := busyTestPod("pinned", map[string]string{doNotDisrupt: "true"})
	putWorker(api, pod, 1)
	m, k8s, cached := setupBusyMarkerTest(t, api, pod)

	if err := m.sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := livePod(t, k8s, cached, pod); got.Labels[BusyWorkerLabel] != "true" {
		t.Fatalf("busy pod not marked: %v", got.Labels)
	}
	putWorker(api, pod, 0)
	if err := m.sync(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got := livePod(t, k8s, cached, pod)
	if _, ok := got.Labels[BusyWorkerLabel]; ok {
		t.Errorf("idle pod still labeled: %v", got.Labels)
	}
	if got.Annotations[doNotDisrupt] != "true" {
		t.Errorf("template annotation removed: %v", got.Annotations)
	}
}
