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
	"fmt"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/objectstore"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/internal/snapshotlayout"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// loseGoldenTag deletes the test template's golden tag and its snapshot's
// objects behind the control plane's back, as a tag deleted by hand or a
// golden replaced while actors still borrow it leaves them.
func loseGoldenTag(t *testing.T, tc *testContext, tmpl *ateapipb.ActorTemplate) {
	t.Helper()
	ctx := context.Background()
	uri, err := resources.ParseSnapshotURI(goldenSnapshotURI(t))
	if err != nil {
		t.Fatalf("ParseSnapshotURI: %v", err)
	}
	if err := objectstore.DeletePrefix(ctx, tc.objectStore, uri.Prefix()); err != nil {
		t.Fatalf("delete golden snapshot objects: %v", err)
	}
	golden := resources.TagRefFromObjectRef(tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag())
	if _, err := tc.persistence.DeleteTag(ctx, golden, store.DeletePreconditions{}); err != nil {
		t.Fatalf("delete golden tag %s: %v", golden, err)
	}
}

// takeGoldenAgain does the reconciler's part, which does not run in this
// environment: once a resume has cleared the template's lost golden record,
// it publishes a new golden tag with its snapshot and records it. It returns
// the new golden's snapshot URI.
func takeGoldenAgain(ctx context.Context, tc *testContext, tmplRef resources.ActorTemplateRef) (string, error) {
	var tmpl *ateapipb.ActorTemplate
	for {
		var err error
		if tmpl, err = tc.persistence.GetActorTemplate(ctx, tmplRef); err != nil {
			return "", err
		}
		if tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() == nil {
			break
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("the lost golden record of %s was never cleared", tmplRef)
		case <-time.After(20 * time.Millisecond):
		}
	}
	tagRef := resources.TagRef{Atespace: resources.GoldenActorAtespace, Name: tmpl.GetMetadata().GetUid()}
	tag, err := tc.persistence.CreateTag(ctx, &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: tagRef.Atespace, Name: tagRef.Name},
		SourceActor: &ateapipb.ObjectRef{Atespace: resources.GoldenActorAtespace, Name: tmpl.GetMetadata().GetUid()},
		Scope:       ateapipb.TagScope_TAG_SCOPE_PUBLISHED,
		Status:      &ateapipb.TagStatus{StorageLocation: testStorageLocation, ActorTemplateUid: tmpl.GetMetadata().GetUid()},
	})
	if err != nil {
		return "", err
	}
	uri, err := resources.NewTagSnapshotURI(testStorageLocation, tagRef.Atespace, tag.GetMetadata().GetUid())
	if err != nil {
		return "", err
	}
	if err := tc.objectStore.WriteSnapshot(uri.String(), "manifest.json"); err != nil {
		return "", err
	}
	if _, err := tc.persistence.UpdateTag(ctx, tagRef, store.PreconditionFrom(tag), func(toUpdate *ateapipb.Tag) error {
		toUpdate.Status.Snapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri.String(), ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL}
		return nil
	}); err != nil {
		return "", err
	}
	if _, err := tc.persistence.UpdateActorTemplate(ctx, tmplRef, store.PreconditionFrom(tmpl), func(dbTemplate *ateapipb.ActorTemplate) error {
		dbTemplate.Status.GoldenSnapshotStatus.GoldenTag = &ateapipb.ObjectRef{Atespace: tagRef.Atespace, Name: tagRef.Name}
		dbTemplate.Status.GoldenSnapshotStatus.SnapshotLayout = snapshotlayout.Current
		return nil
	}); err != nil {
		return "", err
	}
	return uri.String(), nil
}

// TestResumeActor_GoldenLostIsTakenAgain drives the resume of an actor created
// before its template's golden was lost end to end: the resume sends the
// template back for its golden, waits for the new one and restores it in full,
// and the actor runs.
func TestResumeActor_GoldenLostIsTakenAgain(t *testing.T) {
	ns := namespaceForTest("ns-golden-lost")
	tc := setupTest(t, ns)
	defer tc.cleanup()

	tmpl := createTemplate(t, tc, ns)
	createWorkerPod(t, tc, ns, "worker-1", "node1", "pool1")

	actorRef := &ateapipb.ObjectRef{Atespace: testAtespace, Name: "id1"}
	if _, err := tc.client.CreateActor(context.Background(), &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: actorRef.GetName()},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: testAtespace, Name: "tmpl1"},
	}}); err != nil {
		t.Fatalf("CreateActor failed: %v", err)
	}
	loseGoldenTag(t, tc, tmpl)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type taken struct {
		uri string
		err error
	}
	reconciled := make(chan taken, 1)
	go func() {
		uri, err := takeGoldenAgain(ctx, tc, resources.ActorTemplateRefFromActorTemplate(tmpl))
		reconciled <- taken{uri, err}
	}()

	resumed, err := tc.client.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: actorRef})
	if err != nil {
		t.Fatalf("ResumeActor failed: %v", err)
	}
	golden := <-reconciled
	if golden.err != nil {
		t.Fatalf("taking the golden again: %v", golden.err)
	}
	if got := resumed.GetActor().GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_RUNNING {
		t.Errorf("actor state = %v, want RUNNING", got)
	}
	restoreReq := tc.fakeAtelet.lastRestoreRequest()
	if restoreReq == nil {
		t.Fatal("resume sent no Restore request to atelet")
	}
	if got := restoreReq.GetExternalConfig().GetSnapshotUri(); got != golden.uri {
		t.Errorf("restore snapshot uri = %q, want the golden taken again %q", got, golden.uri)
	}
	if got := restoreReq.GetScope(); got != ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL {
		t.Errorf("restore scope = %v, want SNAPSHOT_SCOPE_FULL", got)
	}
}

// TestResumeActor_GoldenLostForGood refuses, over the wire, the resume of an
// actor whose golden is gone when its template's golden cannot be taken again:
// not resumable, reason GOLDEN_SNAPSHOT_UNAVAILABLE.
func TestResumeActor_GoldenLostForGood(t *testing.T) {
	ns := namespaceForTest("ns-golden-lost-for-good")
	tc := setupTest(t, ns)
	defer tc.cleanup()

	tmpl := createTemplate(t, tc, ns)
	createWorkerPod(t, tc, ns, "worker-1", "node1", "pool1")

	actorRef := &ateapipb.ObjectRef{Atespace: testAtespace, Name: "id1"}
	if _, err := tc.client.CreateActor(context.Background(), &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: actorRef.GetName()},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: testAtespace, Name: "tmpl1"},
	}}); err != nil {
		t.Fatalf("CreateActor failed: %v", err)
	}
	loseGoldenTag(t, tc, tmpl)
	if _, err := tc.persistence.UpdateActorTemplate(context.Background(), resources.ActorTemplateRefFromActorTemplate(tmpl), store.PreconditionFrom(tmpl), func(dbTemplate *ateapipb.ActorTemplate) error {
		dbTemplate.Status.GoldenSnapshotStatus = &ateapipb.GoldenSnapshotStatus{ErrorMessage: "GoldenActorInvalid: invalid container image"}
		return nil
	}); err != nil {
		t.Fatalf("fail the template's golden: %v", err)
	}

	_, err := tc.client.ResumeActor(context.Background(), &ateapipb.ResumeActorRequest{Actor: actorRef})
	if status.Code(err) != codes.FailedPrecondition || !apierror.IsNotResumable(err) {
		t.Fatalf("ResumeActor = %v, want a not-resumable FailedPrecondition", err)
	}
	st, _ := status.FromError(err)
	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.GetReason()
		}
	}
	if reason != "GOLDEN_SNAPSHOT_UNAVAILABLE" {
		t.Errorf("ErrorInfo reason = %q, want GOLDEN_SNAPSHOT_UNAVAILABLE", reason)
	}
}
