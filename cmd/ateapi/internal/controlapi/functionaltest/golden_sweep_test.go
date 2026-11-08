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

package functionaltest

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/controlapi"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestGoldenSweepRemovesGoldenOfInterruptedDelete deletes a template while
// object storage is unreachable: the row is gone, its golden tag stays, and a
// retry of the delete answers NotFound. Once storage is back, the template
// resync's sweep removes the golden tag and its snapshot; the golden of a
// template whose row exists is kept.
func TestGoldenSweepRemovesGoldenOfInterruptedDelete(t *testing.T) {
	ns := namespaceForTest("golden-sweep")
	tc := setupTest(t, ns)
	defer tc.cleanup()
	var storageDown atomic.Bool
	tc.objectStore.OnDelete = func(string, string) error {
		if storageDown.Load() {
			return errors.New("object storage unreachable")
		}
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	selector := &ateapipb.Selector{MatchLabels: map[string]string{poolLabelKey: ns}}
	createWorkerPool(t, tc, ns, "pool1", selector.GetMatchLabels())
	kept := createTemplateWithSelector(t, tc, "kept-template", selector)
	swept := createTemplateWithSelector(t, tc, "swept-template", selector)
	createWorkerPod(t, tc, ns, "worker-1", "node1", "pool1")
	createWorkerPod(t, tc, ns, "worker-2", "node1", "pool1")
	controlapi.NewActorTemplateReconciler(tc.persistence, tc.service, time.Second, time.Second).Start(ctx)

	goldenOf := func(tmpl *ateapipb.ActorTemplate) (*ateapipb.ObjectRef, string) {
		t.Helper()
		ref := resources.ActorTemplateRefFromActorTemplate(tmpl).ToObjectRef()
		var golden *ateapipb.ObjectRef
		if err := wait.PollUntilContextTimeout(ctx, 50*time.Millisecond, 60*time.Second, true, func(ctx context.Context) (bool, error) {
			current, err := tc.client.GetActorTemplate(ctx, &ateapipb.GetActorTemplateRequest{ActorTemplate: ref})
			golden = current.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag()
			return golden != nil, err
		}); err != nil {
			t.Fatalf("waiting for the golden tag of %s: %v", tmpl.GetMetadata().GetName(), err)
		}
		tag, err := tc.client.GetTag(ctx, &ateapipb.GetTagRequest{Tag: golden})
		if err != nil {
			t.Fatal(err)
		}
		return golden, tag.GetStatus().GetSnapshot().GetSnapshotUri()
	}
	keptGolden, keptURI := goldenOf(kept)
	sweptGolden, sweptURI := goldenOf(swept)

	// The delete removes the row, then fails on the golden tag's snapshot.
	storageDown.Store(true)
	sweptRef := resources.ActorTemplateRefFromActorTemplate(swept).ToObjectRef()
	_, err := tc.client.DeleteActorTemplate(ctx, &ateapipb.DeleteActorTemplateRequest{ActorTemplate: sweptRef})
	if err == nil || !strings.Contains(err.Error(), "is deleted, its golden tag") {
		t.Fatalf("DeleteActorTemplate with object storage down: %v, want the golden tag named as left behind", err)
	}
	_, err = tc.client.DeleteActorTemplate(ctx, &ateapipb.DeleteActorTemplateRequest{ActorTemplate: sweptRef})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("retried DeleteActorTemplate: %v, want NotFound", err)
	}
	if _, err := tc.client.GetTag(ctx, &ateapipb.GetTagRequest{Tag: sweptGolden}); err != nil {
		t.Fatalf("golden tag after the interrupted delete: %v, want it left behind", err)
	}

	// Storage is back: the sweep removes the golden left behind.
	storageDown.Store(false)
	if err := wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := tc.client.GetTag(ctx, &ateapipb.GetTagRequest{Tag: sweptGolden})
		if status.Code(err) == codes.NotFound {
			return true, nil
		}
		return false, err
	}); err != nil {
		t.Fatalf("waiting for the sweep to remove golden tag %v: %v", sweptGolden, err)
	}
	assertSnapshotCollected(t, tc, sweptURI)

	if _, err := tc.client.GetTag(ctx, &ateapipb.GetTagRequest{Tag: keptGolden}); err != nil {
		t.Fatalf("golden tag of the template that exists: %v, want it kept", err)
	}
	assertSnapshotPresent(t, tc, keptURI)
}
