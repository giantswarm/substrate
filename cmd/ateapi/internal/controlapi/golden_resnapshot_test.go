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

package controlapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.ate.dev/substrate/api/ateapi/v1"
	"go.ate.dev/substrate/cmd/ateapi/internal/store"
	"go.ate.dev/substrate/internal/apierror"
	"go.ate.dev/substrate/internal/resources"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

// goldenResnapshotFixture is a template with a golden tag and an actor created
// from it that never suspended, so it still borrows the golden's snapshot.
type goldenResnapshotFixture struct {
	persistence store.Interface
	w           *ActorWorkflow
	tmpl        *ateapi.ActorTemplate
	golden      *ateapi.Tag
	actorRef    resources.ActorRef
}

func newGoldenResnapshotFixture(t *testing.T) *goldenResnapshotFixture {
	t.Helper()
	ctx := context.Background()
	persistence := newTestPersistence(t)
	w, _ := newFinalizeWorkflow(persistence)
	w.goldenResnapshotBudget = 5 * time.Second
	w.goldenResnapshotPoll = 10 * time.Millisecond
	f := &goldenResnapshotFixture{persistence: persistence, w: w, actorRef: resources.ActorRef{Atespace: "team-a", Name: "conversation"}}

	tmpl, err := persistence.CreateActorTemplate(ctx, &ateapi.ActorTemplate{
		Metadata:       &ateapi.ResourceMetadata{Atespace: "team-a", Name: "agent"},
		SnapshotConfig: &ateapi.SnapshotConfig{StorageLocation: testStorageLocation},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	f.tmpl = tmpl
	f.golden = f.takeGolden(t)

	borrowed := proto.CloneOf(f.golden.GetStatus().GetSnapshot())
	borrowed.ActorTemplateUid = tmpl.GetMetadata().GetUid()
	seedWorkflowActor(t, ctx, persistence, f.actorRef, "team-a", "agent", ateapi.ActorState_ACTOR_STATE_SUSPENDED, func(a *ateapi.Actor) {
		a.Status.Snapshots = []*ateapi.Snapshot{borrowed}
		a.Status.LastAssignedGeneration = 1
	})
	return f
}

// takeGolden does what the reconciler does once a golden is complete: it
// stores the template's golden tag, named after its UID in its atespace, with
// a completed snapshot, and records it as the template's golden.
func (f *goldenResnapshotFixture) takeGolden(t *testing.T) *ateapi.Tag {
	t.Helper()
	ctx := context.Background()
	tagRef := resources.TagRef{Atespace: f.tmpl.GetMetadata().GetAtespace(), Name: f.tmpl.GetMetadata().GetUid()}
	snapshotUUID := uuid.NewString()
	tag, err := f.persistence.CreateTag(ctx, &ateapi.Tag{
		Metadata:    &ateapi.ResourceMetadata{Atespace: tagRef.Atespace, Name: tagRef.Name},
		SourceActor: &ateapi.ObjectRef{Atespace: tagRef.Atespace, Name: tagRef.Name},
		Scope:       ateapi.TagScope_TAG_SCOPE_ATESPACE,
		Status: &ateapi.TagStatus{
			ActorTemplateUid: f.tmpl.GetMetadata().GetUid(),
			Snapshot: &ateapi.Snapshot{
				Uuid:       snapshotUUID,
				Generation: 1,
				Owner:      ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG,
				Fidelity:   ateapi.SnapshotFidelity_SNAPSHOT_FIDELITY_MEMORY,
				DurableSnapshot: &ateapi.SnapshotStorage{
					State:  ateapi.SnapshotStorageState_SNAPSHOT_STORAGE_STATE_COMPLETED,
					Object: &ateapi.ObjectSnapshot{SnapshotUri: mustTagSnapshotURI(t, f.tmpl, tagRef.Atespace, snapshotUUID).String()},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create golden tag %s: %v", tagRef, err)
	}
	f.updateGoldenStatus(t, func(s *ateapi.GoldenSnapshotStatus) {
		s.GoldenTag = &ateapi.ObjectRef{Atespace: tagRef.Atespace, Name: tagRef.Name}
		s.ErrorMessage = ""
	})
	return tag
}

func (f *goldenResnapshotFixture) updateGoldenStatus(t *testing.T, mutate func(*ateapi.GoldenSnapshotStatus)) {
	t.Helper()
	ctx := context.Background()
	tmplRef := resources.ActorTemplateRefFromActorTemplate(f.tmpl)
	current, err := f.persistence.GetActorTemplate(ctx, tmplRef)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	updated, err := f.persistence.UpdateActorTemplate(ctx, tmplRef, store.PreconditionFrom(current), func(dbTemplate *ateapi.ActorTemplate) error {
		if dbTemplate.Status == nil {
			dbTemplate.Status = &ateapi.ActorTemplateStatus{}
		}
		if dbTemplate.Status.GoldenSnapshotStatus == nil {
			dbTemplate.Status.GoldenSnapshotStatus = &ateapi.GoldenSnapshotStatus{}
		}
		mutate(dbTemplate.Status.GoldenSnapshotStatus)
		return nil
	})
	if err != nil {
		t.Fatalf("update template golden status: %v", err)
	}
	f.tmpl = updated
}

// loseGolden deletes the golden tag and its snapshot, as a tag delete does.
func (f *goldenResnapshotFixture) loseGolden(t *testing.T) {
	t.Helper()
	if _, err := f.w.DeleteTag(context.Background(), resources.TagRefFromTag(f.golden), store.DeletePreconditions{}); err != nil {
		t.Fatalf("delete golden tag: %v", err)
	}
}

func (f *goldenResnapshotFixture) ensure(t *testing.T) (*ateapi.Actor, error) {
	t.Helper()
	ctx := context.Background()
	actor, tmpl, err := f.w.loadActorForResume(ctx, f.actorRef)
	if err != nil {
		t.Fatalf("loadActorForResume: %v", err)
	}
	return f.w.ensureGoldenSourcePresent(ctx, f.actorRef, actor, tmpl)
}

func (f *goldenResnapshotFixture) storedTemplate(t *testing.T) *ateapi.ActorTemplate {
	t.Helper()
	tmpl, err := f.persistence.GetActorTemplate(context.Background(), resources.ActorTemplateRefFromActorTemplate(f.tmpl))
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	return tmpl
}

func (f *goldenResnapshotFixture) storedActor(t *testing.T) *ateapi.Actor {
	t.Helper()
	actor, err := f.persistence.GetActor(context.Background(), f.actorRef)
	if err != nil {
		t.Fatalf("get actor: %v", err)
	}
	return actor
}

// TestEnsureGoldenSource_GoldenPresent leaves an actor whose borrowed golden
// is still there as it is.
func TestEnsureGoldenSource_GoldenPresent(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	before := f.storedActor(t)

	if _, err := f.ensure(t); err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", err)
	}
	if after := f.storedActor(t); after.GetMetadata().GetVersion() != before.GetMetadata().GetVersion() {
		t.Errorf("actor was written (version %d -> %d), want it untouched", before.GetMetadata().GetVersion(), after.GetMetadata().GetVersion())
	}
}

// TestEnsureGoldenSource_OwnSnapshotNeedsNoGolden leaves an actor that owns
// its snapshot alone when its template's golden is gone.
func TestEnsureGoldenSource_OwnSnapshotNeedsNoGolden(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	mustUpdateActorStatus(t, context.Background(), f.persistence, f.storedActor(t), func(s *ateapi.ActorStatus) {
		s.Snapshots[0].Owner = ateapi.SnapshotOwner_SNAPSHOT_OWNER_ACTOR
	})
	f.loseGolden(t)

	if _, err := f.ensure(t); err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", err)
	}
	if got := f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus().GetGoldenTag(); got == nil {
		t.Error("the template's golden record was cleared, want a resume that needs no golden to leave it")
	}
}

// TestEnsureGoldenSource_TakesLostGoldenAgain sends a template whose only
// golden is gone back to be taken again, waits for the new golden and moves
// the actor onto it as a new generation.
func TestEnsureGoldenSource_TakesLostGoldenAgain(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.loseGolden(t)

	type result struct {
		actor *ateapi.Actor
		err   error
	}
	resumed := make(chan result, 1)
	ctx := context.Background()
	actor, tmpl, err := f.w.loadActorForResume(ctx, f.actorRef)
	if err != nil {
		t.Fatalf("loadActorForResume: %v", err)
	}
	go func() {
		actor, err := f.w.ensureGoldenSourcePresent(ctx, f.actorRef, actor, tmpl)
		resumed <- result{actor, err}
	}()

	// The reconciler's part: once the record is cleared, a new golden is
	// taken and recorded.
	deadline := time.Now().Add(4 * time.Second)
	for f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil {
		if time.Now().After(deadline) {
			t.Fatal("the template's lost golden record was never cleared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	retaken := f.takeGolden(t)

	got := <-resumed
	if got.err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", got.err)
	}
	snap := snapshotToResume(f.storedActor(t).GetStatus())
	if snap.GetUuid() != retaken.GetStatus().GetSnapshot().GetUuid() || snap.GetOwner() != ateapi.SnapshotOwner_SNAPSHOT_OWNER_TAG || snap.GetGeneration() != 2 {
		t.Errorf("snapshot to resume = %v, want the golden taken again as generation 2, owned by the tag", snap)
	}
	if n := len(f.storedActor(t).GetStatus().GetSnapshots()); n != 1 {
		t.Errorf("actor holds %d snapshots, want the lost one replaced", n)
	}
}

// TestEnsureGoldenSource_BudgetSpent answers Unavailable when the golden is
// not taken again within the budget, and leaves the template sent back to the
// reconciler.
func TestEnsureGoldenSource_BudgetSpent(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.w.goldenResnapshotBudget = 50 * time.Millisecond
	f.loseGolden(t)

	_, err := f.ensure(t)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ensureGoldenSourcePresent code = %v, want Unavailable (err: %v)", got, err)
	}
	if s := f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus(); s.GetGoldenTag() != nil || goldenSnapshotDone(s) {
		t.Errorf("golden status = %v, want the lost record cleared and the template left for the reconciler", s)
	}
}

// TestEnsureGoldenSource_GoldenFailed refuses the resume when the golden
// cannot be taken again: the actor has nothing to restore.
func TestEnsureGoldenSource_GoldenFailed(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.loseGolden(t)
	f.updateGoldenStatus(t, func(s *ateapi.GoldenSnapshotStatus) {
		s.GoldenTag = nil
		s.ErrorMessage = reasonGoldenActorInvalid + ": invalid container image"
	})

	_, _, err := f.w.ResumeActor(context.Background(), f.actorRef)
	if got := apierror.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("ResumeActor code = %v, want FailedPrecondition (err: %v)", got, err)
	}
	if got := f.storedActor(t).GetStatus().GetState(); got != ateapi.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("actor state = %v, want SUSPENDED: a refused resume crashes nothing", got)
	}
}
