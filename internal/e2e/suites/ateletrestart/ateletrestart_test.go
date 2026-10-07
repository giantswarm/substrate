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

// Package ateletrestart restarts the atelet on a running actor's node and
// requires the next suspend to commit a snapshot within the budget a client
// gives a suspend after a turn. A routine DaemonSet roll (a release, an image
// cache change) restarts every atelet while actors keep running on their
// workers, so the first suspend on each node after it must behave like any
// other.
//
// It deletes the atelet pod of the node, so it runs only where no other suite
// uses that atelet concurrently: CI runs it as its own step. Locally:
//
//	E2E_ATELET_RESTART=1 hack/run-e2e-kind.sh ./internal/e2e/suites/ateletrestart -v -args --no-color
package ateletrestart

import (
	"context"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/portforward"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// suspendBudget is the deadline a client puts on a suspend after a turn
// (kagent's quiesce). A suspend that outlives it leaves the client without
// the snapshot it just wrote.
const suspendBudget = 30 * time.Second

// ateletReadyTimeout bounds the DaemonSet replacing the deleted atelet pod.
const ateletReadyTimeout = 3 * time.Minute

func TestFirstSuspendAfterAteletRestart(t *testing.T) {
	if os.Getenv("E2E_ATELET_RESTART") == "" {
		t.Skip("restarts the atelet of the actor's node: needs E2E_ATELET_RESTART=1, with no other suite running")
	}
	env, err := e2e.CheckEnv("BUCKET_NAME")
	if err != nil {
		t.Fatalf("CheckEnv failed: %v", err)
	}
	ctx := context.Background()
	clients := e2e.GetClients()

	atespace, template := e2e.DeployProbe(t, env["BUCKET_NAME"], "ateletrestart")
	ref := &ateapipb.ObjectRef{Atespace: atespace, Name: "restart"}
	if _, err := clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: atespace, Name: ref.GetName()},
		ActorTemplate: e2e.TemplateRef(template),
	}}); err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, _ = clients.SubstrateAPI.SuspendActor(cleanupCtx, &ateapipb.SuspendActorRequest{Actor: ref})
		if _, err := clients.SubstrateAPI.DeleteActor(cleanupCtx, &ateapipb.DeleteActorRequest{Actor: ref}); err != nil && status.Code(err) != codes.NotFound {
			t.Logf("cleanup: DeleteActor %s/%s: %v", atespace, ref.GetName(), err)
		}
	})

	// Two rounds: the first suspend after the restart writes a snapshot the
	// second round resumes from, so the snapshot is proven usable and the
	// restart is proven on an actor restored by the new atelet as well.
	for round := 1; round <= 2; round++ {
		running := resumeRunning(t, ctx, clients, ref)
		node := running.GetStatus().GetWorkerAssignment().GetNodeName()
		if node == "" {
			t.Fatalf("round %d: running actor has no worker node: %v", round, running.GetStatus())
		}
		restartAtelet(t, ctx, clients, node)

		suspendCtx, cancel := context.WithTimeout(ctx, suspendBudget)
		start := time.Now()
		suspended, err := clients.SubstrateAPI.SuspendActor(suspendCtx, &ateapipb.SuspendActorRequest{Actor: ref})
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			t.Fatalf("round %d: first suspend after the atelet restart on %s failed after %v: %v", round, node, elapsed, err)
		}
		if got := suspended.GetActor().GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
			t.Fatalf("round %d: suspend returned state %v, want SUSPENDED", round, got)
		}
		if suspended.GetActor().GetStatus().GetExternalSnapshot().GetSnapshotUri() == "" {
			t.Fatalf("round %d: suspend returned no external snapshot", round)
		}
		t.Logf("round %d: first suspend after the atelet restart on %s committed a snapshot in %v", round, node, elapsed)
	}
}

// resumeRunning resumes the actor and returns its record once RUNNING.
func resumeRunning(t *testing.T, ctx context.Context, clients *e2e.Clients, ref *ateapipb.ObjectRef) *ateapipb.Actor {
	t.Helper()
	if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: ref}); err != nil {
		t.Fatalf("ResumeActor: %v", err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		actor, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: ref})
		if err == nil && actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_RUNNING {
			return actor
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out waiting for actor %s/%s to run", ref.GetAtespace(), ref.GetName())
	return nil
}

// restartAtelet deletes the atelet pod on node and waits until the DaemonSet's
// replacement is ready, the way a rollout restarts it.
func restartAtelet(t *testing.T, ctx context.Context, clients *e2e.Clients, node string) {
	t.Helper()
	pods := clients.K8s.CoreV1().Pods(e2e.SystemNamespace())
	old := ateletOnNode(t, ctx, clients, node, "")
	if err := pods.Delete(ctx, old.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("deleting atelet pod %s: %v", old.Name, err)
	}
	t.Logf("deleted atelet pod %s on %s", old.Name, node)

	deadline := time.Now().Add(ateletReadyTimeout)
	for time.Now().Before(deadline) {
		if pod := ateletOnNode(t, ctx, clients, node, old.UID); pod != nil && portforward.IsPodReady(pod) {
			t.Logf("atelet pod %s on %s is ready", pod.Name, node)
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timed out after %v waiting for a new atelet pod on %s", ateletReadyTimeout, node)
}

// ateletOnNode returns the live atelet pod on node other than the one with
// UID skip, or nil when there is none yet. With an empty skip it requires
// exactly one.
func ateletOnNode(t *testing.T, ctx context.Context, clients *e2e.Clients, node string, skip types.UID) *corev1.Pod {
	t.Helper()
	list, err := clients.K8s.CoreV1().Pods(e2e.SystemNamespace()).List(ctx, metav1.ListOptions{
		LabelSelector: "app=atelet",
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(),
	})
	if err != nil {
		t.Fatalf("listing atelet pods on %s: %v", node, err)
	}
	var live []*corev1.Pod
	for i := range list.Items {
		pod := &list.Items[i]
		if pod.UID != skip && pod.DeletionTimestamp == nil {
			live = append(live, pod)
		}
	}
	if skip == "" && len(live) != 1 {
		t.Fatalf("found %d atelet pods on %s, want 1", len(live), node)
	}
	if len(live) == 0 {
		return nil
	}
	return live[0]
}
