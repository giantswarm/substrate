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
	"errors"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/objectstore/objectstoretest"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
)

// goldenResnapshotFixture is a template with a golden tag and an actor created
// from it that never suspended, so it still borrows the golden's snapshot.
type goldenResnapshotFixture struct {
	persistence store.Interface
	w           *ActorWorkflow
	objects     *objectstoretest.Fake
	tmpl        *ateapipb.ActorTemplate
	golden      *ateapipb.Tag
	actorRef    resources.ActorRef
}

func newGoldenResnapshotFixture(t *testing.T) *goldenResnapshotFixture {
	t.Helper()
	ctx := context.Background()
	persistence := newTestPersistence(t)
	w := NewActorWorkflow(persistence, nil, nil, nil, nil, nil, nil, "", nil, time.Minute, 0, objectstoretest.New())
	w.goldenResnapshotBudget = 5 * time.Second
	w.goldenResnapshotPoll = 10 * time.Millisecond
	f := &goldenResnapshotFixture{persistence: persistence, w: w, objects: w.objectStore.(*objectstoretest.Fake), actorRef: resources.ActorRef{Atespace: "team-a", Name: "conversation"}}

	storetest.MustCreateAtespace(t, ctx, persistence, "ns")
	storetest.MustCreateAtespace(t, ctx, persistence, resources.GoldenActorAtespace)
	tmpl, err := persistence.CreateActorTemplate(ctx, &ateapipb.ActorTemplate{
		Metadata:       &ateapipb.ResourceMetadata{Atespace: "ns", Name: "agent"},
		SnapshotConfig: &ateapipb.SnapshotConfig{StorageLocation: testStorageLocation},
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	f.tmpl = tmpl
	f.golden = f.takeGolden(t, tmpl.GetMetadata().GetUid())

	seedWorkflowActor(t, ctx, persistence, f.actorRef, "ns", "agent", ateapipb.ActorState_ACTOR_STATE_SUSPENDED, func(a *ateapipb.Actor) {
		a.Status.ExternalSnapshot = &ateapipb.ExternalSnapshot{
			SnapshotUri:      f.golden.GetStatus().GetSnapshot().GetSnapshotUri(),
			ContentScope:     ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL,
			ActorTemplateUid: tmpl.GetMetadata().GetUid(),
		}
	})
	return f
}

// takeGolden does what the reconciler does once a golden is complete: it
// stores a golden tag named name whose snapshot has objects, and records it as
// the template's golden.
func (f *goldenResnapshotFixture) takeGolden(t *testing.T, name string) *ateapipb.Tag {
	t.Helper()
	ctx := context.Background()
	tagRef := resources.TagRef{Atespace: resources.GoldenActorAtespace, Name: name}
	tag, err := f.persistence.CreateTag(ctx, &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: tagRef.Atespace, Name: tagRef.Name},
		SourceActor: goldenActorRefFor(f.tmpl),
		Scope:       ateapipb.TagScope_TAG_SCOPE_PUBLISHED,
		Status:      &ateapipb.TagStatus{ActorTemplateUid: f.tmpl.GetMetadata().GetUid(), StorageLocation: testStorageLocation},
	})
	if err != nil {
		t.Fatalf("create golden tag %s: %v", tagRef, err)
	}
	uri := mustTagSnapshotURI(t, f.tmpl, tagRef.Atespace, tag.GetMetadata().GetUid())
	f.objects.PutSnapshot(t, uri, "manifest.json", "memory.zst")
	tag, err = f.persistence.UpdateTag(ctx, tagRef, store.PreconditionFrom(tag), func(toUpdate *ateapipb.Tag) error {
		toUpdate.Status.Snapshot = &ateapipb.ExternalSnapshot{SnapshotUri: uri.String(), ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL}
		return nil
	})
	if err != nil {
		t.Fatalf("record golden tag %s snapshot: %v", tagRef, err)
	}
	f.updateGoldenStatus(t, func(s *ateapipb.GoldenSnapshotStatus) {
		s.GoldenTag = &ateapipb.ObjectRef{Atespace: tagRef.Atespace, Name: tagRef.Name}
		s.SnapshotLayout = 1
		s.ErrorMessage = ""
	})
	return tag
}

func (f *goldenResnapshotFixture) updateGoldenStatus(t *testing.T, mutate func(*ateapipb.GoldenSnapshotStatus)) {
	t.Helper()
	ctx := context.Background()
	tmplRef := resources.ActorTemplateRefFromActorTemplate(f.tmpl)
	current, err := f.persistence.GetActorTemplate(ctx, tmplRef)
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	updated, err := f.persistence.UpdateActorTemplate(ctx, tmplRef, store.PreconditionFrom(current), func(dbTemplate *ateapipb.ActorTemplate) error {
		if dbTemplate.Status == nil {
			dbTemplate.Status = &ateapipb.ActorTemplateStatus{}
		}
		if dbTemplate.Status.GoldenSnapshotStatus == nil {
			dbTemplate.Status.GoldenSnapshotStatus = &ateapipb.GoldenSnapshotStatus{}
		}
		mutate(dbTemplate.Status.GoldenSnapshotStatus)
		return nil
	})
	if err != nil {
		t.Fatalf("update template golden status: %v", err)
	}
	f.tmpl = updated
}

// loseGolden collects the golden's snapshot as a tag delete does.
func (f *goldenResnapshotFixture) loseGolden(t *testing.T) {
	t.Helper()
	if _, err := f.w.DeleteTag(context.Background(), resources.TagRefFromTag(f.golden), store.DeletePreconditions{}); err != nil {
		t.Fatalf("delete golden tag: %v", err)
	}
}

func (f *goldenResnapshotFixture) ensure(t *testing.T) (*ateapipb.Actor, resumeSnapshotSource, error) {
	t.Helper()
	ctx := context.Background()
	actor, tmpl, src, err := f.w.loadActorForResume(ctx, f.actorRef)
	if err != nil {
		t.Fatalf("loadActorForResume: %v", err)
	}
	return f.w.ensureGoldenSourcePresent(ctx, f.actorRef, actor, tmpl, src)
}

func (f *goldenResnapshotFixture) storedTemplate(t *testing.T) *ateapipb.ActorTemplate {
	t.Helper()
	tmpl, err := f.persistence.GetActorTemplate(context.Background(), resources.ActorTemplateRefFromActorTemplate(f.tmpl))
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	return tmpl
}

func (f *goldenResnapshotFixture) storedActor(t *testing.T) *ateapipb.Actor {
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

	_, src, err := f.ensure(t)
	if err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", err)
	}
	if got, want := src.SnapshotURI.String(), f.golden.GetStatus().GetSnapshot().GetSnapshotUri(); got != want {
		t.Errorf("restore source = %s, want the golden %s", got, want)
	}
	if after := f.storedActor(t); after.GetMetadata().GetVersion() != before.GetMetadata().GetVersion() {
		t.Errorf("actor was written (version %d -> %d), want it untouched", before.GetMetadata().GetVersion(), after.GetMetadata().GetVersion())
	}
}

// TestEnsureGoldenSource_OwnSnapshotNeedsNoGolden leaves an actor that owns
// its snapshot alone when its template's golden is gone: its own durable data
// is what it restores.
func TestEnsureGoldenSource_OwnSnapshotNeedsNoGolden(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	actor := f.storedActor(t)
	own := mustActorSnapshotURI(t, f.tmpl, actor, "after-first-turn")
	f.objects.PutSnapshot(t, own, "manifest.json")
	mustUpdateActorStatus(t, context.Background(), f.persistence, actor, func(s *ateapipb.ActorStatus) {
		s.ExternalSnapshot = &ateapipb.ExternalSnapshot{SnapshotUri: own.String(), ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL, ActorTemplateUid: f.tmpl.GetMetadata().GetUid()}
	})
	f.loseGolden(t)

	_, src, err := f.ensure(t)
	if err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", err)
	}
	if src.SnapshotURI != own {
		t.Errorf("restore source = %s, want the actor's own %s", src.SnapshotURI, own)
	}
	if got := f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus().GetGoldenTag(); got == nil {
		t.Error("the template's golden record was cleared, want a resume that needs no golden to leave it")
	}
}

// TestEnsureGoldenSource_RebasesOntoCurrentGolden moves an actor whose
// borrowed golden was replaced, as a golden taken again in a newer layout is,
// onto the template's current golden.
func TestEnsureGoldenSource_RebasesOntoCurrentGolden(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	current := f.takeGolden(t, retakenGoldenTagName(f.tmpl.GetMetadata().GetUid()))
	f.loseGolden(t)

	actor, src, err := f.ensure(t)
	if err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", err)
	}
	want := current.GetStatus().GetSnapshot().GetSnapshotUri()
	if src.SnapshotURI.String() != want || src.TemplateReplaced {
		t.Errorf("restore source = %s (template replaced %v), want the current golden %s, full", src.SnapshotURI, src.TemplateReplaced, want)
	}
	stored := f.storedActor(t).GetStatus().GetExternalSnapshot()
	if stored.GetSnapshotUri() != want || stored.GetActorTemplateUid() != f.tmpl.GetMetadata().GetUid() {
		t.Errorf("stored external snapshot = %v, want the current golden %s under template %s", stored, want, f.tmpl.GetMetadata().GetUid())
	}
	if actor.GetMetadata().GetVersion() != f.storedActor(t).GetMetadata().GetVersion() {
		t.Error("returned actor is not the stored one")
	}
}

// TestEnsureGoldenSource_TakesLostGoldenAgain sends a template whose only
// golden is gone back to be taken again, waits for the new golden and moves
// the actor onto it.
func TestEnsureGoldenSource_TakesLostGoldenAgain(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.loseGolden(t)

	type result struct {
		src resumeSnapshotSource
		err error
	}
	resumed := make(chan result, 1)
	ctx := context.Background()
	actor, tmpl, src, err := f.w.loadActorForResume(ctx, f.actorRef)
	if err != nil {
		t.Fatalf("loadActorForResume: %v", err)
	}
	go func() {
		_, src, err := f.w.ensureGoldenSourcePresent(ctx, f.actorRef, actor, tmpl, src)
		resumed <- result{src, err}
	}()

	// The reconciler's part: once the record is cleared, a new golden is
	// taken and recorded under the first golden's name.
	deadline := time.Now().Add(4 * time.Second)
	for f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil {
		if time.Now().After(deadline) {
			t.Fatal("the template's lost golden record was never cleared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	retaken := f.takeGolden(t, f.tmpl.GetMetadata().GetUid())

	got := <-resumed
	if got.err != nil {
		t.Fatalf("ensureGoldenSourcePresent: %v", got.err)
	}
	if want := retaken.GetStatus().GetSnapshot().GetSnapshotUri(); got.src.SnapshotURI.String() != want {
		t.Errorf("restore source = %s, want the golden taken again %s", got.src.SnapshotURI, want)
	}
	if stored := f.storedActor(t).GetStatus().GetExternalSnapshot().GetSnapshotUri(); stored != retaken.GetStatus().GetSnapshot().GetSnapshotUri() {
		t.Errorf("stored external snapshot = %s, want the golden taken again", stored)
	}
}

// TestEnsureGoldenSource_BudgetSpent answers Unavailable, a refusal a retry
// outlives, when the golden is not taken again within the budget, and leaves
// the template sent back to the reconciler.
func TestEnsureGoldenSource_BudgetSpent(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.w.goldenResnapshotBudget = 50 * time.Millisecond
	f.loseGolden(t)

	_, _, err := f.ensure(t)
	if got := apierror.Code(err); got != codes.Unavailable {
		t.Fatalf("ensureGoldenSourcePresent code = %v, want Unavailable (err: %v)", got, err)
	}
	st, _ := apierror.FromError(err)
	if apierror.IsNotResumable(st.Err()) {
		t.Errorf("refusal %v is marked not resumable, want one a retry outlives", err)
	}
	tmpl := f.storedTemplate(t)
	if s := tmpl.GetStatus().GetGoldenSnapshotStatus(); s.GetGoldenTag() != nil || s.GetErrorMessage() != "" || goldenTemplateDone(tmpl) {
		t.Errorf("golden status = %v, want the lost record cleared and the template left for the reconciler", s)
	}
	if _, err := f.persistence.GetTag(context.Background(), resources.TagRefFromTag(f.golden)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetTag(lost golden) = %v, want NotFound", err)
	}
}

// TestEnsureGoldenSource_WaitsForGoldenInFlight waits, rather than refusing,
// for a golden being taken or taken again for a cause a retry outlives.
func TestEnsureGoldenSource_WaitsForGoldenInFlight(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status func(*ateapipb.GoldenSnapshotStatus)
	}{
		{name: "being taken", status: func(s *ateapipb.GoldenSnapshotStatus) { s.GoldenTag = nil }},
		{name: "failed for a transient cause", status: func(s *ateapipb.GoldenSnapshotStatus) {
			s.GoldenTag = nil
			s.ErrorMessage = reasonUnexpectedState + ": golden actor was ACTOR_STATE_PAUSED"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newGoldenResnapshotFixture(t)
			f.w.goldenResnapshotBudget = 50 * time.Millisecond
			f.loseGolden(t)
			f.updateGoldenStatus(t, tt.status)

			_, _, err := f.ensure(t)
			if got := apierror.Code(err); got != codes.Unavailable {
				t.Fatalf("ensureGoldenSourcePresent code = %v, want Unavailable (err: %v)", got, err)
			}
		})
	}
}

// TestEnsureGoldenSource_GoldenFailedForGood refuses the resume as not
// resumable when the golden cannot be taken again: the actor has nothing to
// restore. The refusal reaches the caller of ResumeActor.
func TestEnsureGoldenSource_GoldenFailedForGood(t *testing.T) {
	f := newGoldenResnapshotFixture(t)
	f.loseGolden(t)
	failure := reasonGoldenActorInvalid + ": invalid container image"
	f.updateGoldenStatus(t, func(s *ateapipb.GoldenSnapshotStatus) {
		s.GoldenTag = nil
		s.ErrorMessage = failure
	})

	_, _, err := f.w.resumeActorOnce(context.Background(), f.actorRef, nil)
	st, _ := apierror.FromError(err)
	if st.Code() != codes.FailedPrecondition || !apierror.IsNotResumable(st.Err()) {
		t.Fatalf("resumeActorOnce = %v, want a not-resumable FailedPrecondition", err)
	}
	var reason string
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			reason = info.GetReason()
		}
	}
	if reason != reasonGoldenSnapshotUnavailable {
		t.Errorf("ErrorInfo reason = %q, want %q", reason, reasonGoldenSnapshotUnavailable)
	}
	if got := f.storedActor(t).GetStatus().GetState(); got != ateapipb.ActorState_ACTOR_STATE_SUSPENDED {
		t.Errorf("actor state = %v, want SUSPENDED: a refused resume crashes nothing", got)
	}
	if got := f.storedTemplate(t).GetStatus().GetGoldenSnapshotStatus().GetErrorMessage(); got != failure {
		t.Errorf("golden error = %q, want the failure %q kept", got, failure)
	}
}
