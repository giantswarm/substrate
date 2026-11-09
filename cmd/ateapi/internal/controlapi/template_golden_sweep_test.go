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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *fakeTemplateStore) ListActors(_ context.Context, atespace string, _ store.ListOptions) (store.ListResponse[*ateapipb.Actor], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listGoldensErr != nil {
		return store.ListResponse[*ateapipb.Actor]{}, s.listGoldensErr
	}
	var resp store.ListResponse[*ateapipb.Actor]
	for _, actor := range s.goldenActors {
		if actor.GetMetadata().GetAtespace() == atespace {
			resp.Items = append(resp.Items, proto.CloneOf(actor))
		}
	}
	return resp, nil
}

func (s *fakeTemplateStore) ListTags(_ context.Context, atespace string, _ store.ListOptions) (store.ListResponse[*ateapipb.Tag], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listGoldensErr != nil {
		return store.ListResponse[*ateapipb.Tag]{}, s.listGoldensErr
	}
	var resp store.ListResponse[*ateapipb.Tag]
	for _, tag := range s.goldenTags {
		if tag.GetMetadata().GetAtespace() == atespace {
			resp.Items = append(resp.Items, proto.CloneOf(tag))
		}
	}
	return resp, nil
}

// sweepControl deletes golden actors and tags from a fakeTemplateStore's rows,
// as the Control service's deletes do, and records each delete.
type sweepControl struct {
	goldenActorControl
	store *fakeTemplateStore

	mu sync.Mutex
	// deleteActorErr and deleteTagErr fail the delete of the named actor or
	// tag.
	deleteActorErr map[string]error
	deleteTagErr   map[string]error
	actorDeletes   []*ateapipb.DeleteActorRequest
	tagDeletes     []*ateapipb.DeleteTagRequest
}

func (c *sweepControl) DeleteActor(_ context.Context, req *ateapipb.DeleteActorRequest) (*ateapipb.Actor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actorDeletes = append(c.actorDeletes, req)
	if err := c.deleteActorErr[req.GetActor().GetName()]; err != nil {
		return nil, err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	i := slices.IndexFunc(c.store.goldenActors, func(actor *ateapipb.Actor) bool {
		return resources.ActorRefFromActor(actor) == resources.ActorRefFromObjectRef(req.GetActor())
	})
	if i < 0 {
		return nil, apierror.NotFound("no actor")
	}
	actor := c.store.goldenActors[i]
	c.store.goldenActors = slices.Delete(c.store.goldenActors, i, i+1)
	return actor, nil
}

func (c *sweepControl) DeleteTag(_ context.Context, req *ateapipb.DeleteTagRequest) (*ateapipb.Tag, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tagDeletes = append(c.tagDeletes, req)
	if err := c.deleteTagErr[req.GetTag().GetName()]; err != nil {
		return nil, err
	}
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	i := slices.IndexFunc(c.store.goldenTags, func(tag *ateapipb.Tag) bool {
		return resources.TagRefFromTag(tag) == resources.TagRefFromObjectRef(req.GetTag())
	})
	if i < 0 {
		return nil, apierror.NotFound("no tag")
	}
	tag := c.store.goldenTags[i]
	c.store.goldenTags = slices.Delete(c.store.goldenTags, i, i+1)
	return tag, nil
}

const (
	liveTemplateUID       = "6f1c2a64-3c5e-4f0e-9a51-1b6f0c9d2e01"
	deletedTemplateUID    = "0b9e8d7c-6a5f-4e3d-8c2b-1a0f9e8d7c6b"
	youngOrphanUID        = "2d4f6a8c-0e1b-4c3d-9e5f-7a9b1c3d5e7f"
	sweepGrace            = 10 * time.Minute
	sweepTestTemplateName = "live"
)

// sweepTestTemplate is a template row with the given uid.
func sweepTestTemplate(uid string) *ateapipb.ActorTemplate {
	return &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: sweepTestTemplateName, Uid: uid}}
}

// sweepTestActor is a golden actor named after uid, created at created.
func sweepTestActor(uid string, created time.Time) *ateapipb.Actor {
	return &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: uid, CreateTime: timestamppb.New(created)},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: testAtespace, Name: sweepTestTemplateName},
	}
}

// sweepTestTag is a tag of the golden actor named after uid, named name,
// created at created.
func sweepTestTag(uid, name string, created time.Time) *ateapipb.Tag {
	return &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: name, CreateTime: timestamppb.New(created)},
		SourceActor: &ateapipb.ObjectRef{Atespace: resources.GoldenActorAtespace, Name: uid},
	}
}

func deletedNames[T interface{ GetName() string }](refs []T) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.GetName())
	}
	slices.Sort(names)
	return names
}

func (c *sweepControl) deletes() (actors, tags []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	actorRefs := make([]*ateapipb.ObjectRef, 0, len(c.actorDeletes))
	for _, req := range c.actorDeletes {
		if !req.GetAnyState() {
			return []string{"a golden actor delete without AnyState"}, nil
		}
		actorRefs = append(actorRefs, req.GetActor())
	}
	tagRefs := make([]*ateapipb.ObjectRef, 0, len(c.tagDeletes))
	for _, req := range c.tagDeletes {
		tagRefs = append(tagRefs, req.GetTag())
	}
	return deletedNames(actorRefs), deletedNames(tagRefs)
}

func TestSweepOrphanedGoldens(t *testing.T) {
	old := time.Now().Add(-2 * sweepGrace)
	young := time.Now().Add(-sweepGrace / 2)
	retaken := retakenGoldenTagName(deletedTemplateUID)

	t.Run("removes the golden actor and tags of a deleted template, keeps the rest", func(t *testing.T) {
		records := logRecords(t, goldenSweptMessage)
		st := newFakeTemplateStore(sweepTestTemplate(liveTemplateUID))
		st.goldenActors = []*ateapipb.Actor{
			sweepTestActor(liveTemplateUID, old),
			sweepTestActor(deletedTemplateUID, old),
			sweepTestActor(youngOrphanUID, young),
			// Not made by the golden flow: no template uid, or no template.
			{Metadata: &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: "scratch", CreateTime: timestamppb.New(old)}, ActorTemplate: &ateapipb.ObjectRef{Atespace: testAtespace, Name: "t"}},
			{Metadata: &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: "7e5d3c1b-9f8e-4d7c-a6b5-4c3d2e1f0a9b", CreateTime: timestamppb.New(old)}},
		}
		noCreateTime := sweepTestTag(deletedTemplateUID, deletedTemplateUID+"-x", old)
		noCreateTime.Metadata.CreateTime = nil
		st.goldenTags = []*ateapipb.Tag{
			sweepTestTag(liveTemplateUID, liveTemplateUID, old),
			sweepTestTag(liveTemplateUID, retakenGoldenTagName(liveTemplateUID), old),
			sweepTestTag(deletedTemplateUID, deletedTemplateUID, old),
			sweepTestTag(deletedTemplateUID, retaken, old),
			sweepTestTag(youngOrphanUID, youngOrphanUID, young),
			noCreateTime,
			// Not made by the golden flow: a name that is not the source's
			// uid, a source outside the reserved atespace.
			sweepTestTag(deletedTemplateUID, "user-tag", old),
			{Metadata: &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: deletedTemplateUID, CreateTime: timestamppb.New(old)}, SourceActor: &ateapipb.ObjectRef{Atespace: testAtespace, Name: deletedTemplateUID}},
		}
		control := &sweepControl{store: st}
		r := NewActorTemplateReconciler(st, control, 7*time.Second, sweepGrace)

		r.sweepOrphanedGoldens(t.Context())

		actors, tags := control.deletes()
		if diff := cmp.Diff([]string{deletedTemplateUID}, actors); diff != "" {
			t.Errorf("golden actors deleted (-want +got):\n%s", diff)
		}
		wantTags := []string{deletedTemplateUID, retaken}
		slices.Sort(wantTags)
		if diff := cmp.Diff(wantTags, tags); diff != "" {
			t.Errorf("golden tags deleted (-want +got):\n%s", diff)
		}
		if len(*records) != 1 {
			t.Fatalf("logged %d sweep lines, want one for the deleted template: %v", len(*records), *records)
		}
		want := map[string]string{
			"templateUID": deletedTemplateUID,
			"removed":     "actor ate-golden/" + deletedTemplateUID + ", tag ate-golden/" + deletedTemplateUID + ", tag ate-golden/" + retaken,
		}
		if diff := cmp.Diff(want, (*records)[0].attrs); diff != "" {
			t.Errorf("sweep line (-want +got):\n%s", diff)
		}

		// The next sweep finds nothing more to remove.
		r.sweepOrphanedGoldens(t.Context())
		if actorsAgain, tagsAgain := control.deletes(); len(actorsAgain) != len(actors) || len(tagsAgain) != len(tags) {
			t.Errorf("second sweep deleted again: actors %v, tags %v", actorsAgain, tagsAgain)
		}
	})

	t.Run("a golden younger than the grace period is removed once it is older", func(t *testing.T) {
		st := newFakeTemplateStore()
		st.goldenTags = []*ateapipb.Tag{sweepTestTag(youngOrphanUID, youngOrphanUID, young)}
		control := &sweepControl{store: st}
		NewActorTemplateReconciler(st, control, 7*time.Second, sweepGrace).sweepOrphanedGoldens(t.Context())
		if _, tags := control.deletes(); len(tags) != 0 {
			t.Fatalf("deleted %v, younger than the grace period", tags)
		}
		NewActorTemplateReconciler(st, control, 7*time.Second, sweepGrace/4).sweepOrphanedGoldens(t.Context())
		if _, tags := control.deletes(); !slices.Equal(tags, []string{youngOrphanUID}) {
			t.Fatalf("deleted %v once older than the grace period, want [%s]", tags, youngOrphanUID)
		}
	})

	t.Run("a failed delete is retried by the next sweep, the rest removed", func(t *testing.T) {
		st := newFakeTemplateStore()
		st.goldenActors = []*ateapipb.Actor{sweepTestActor(deletedTemplateUID, old)}
		st.goldenTags = []*ateapipb.Tag{sweepTestTag(deletedTemplateUID, deletedTemplateUID, old)}
		control := &sweepControl{store: st, deleteTagErr: map[string]error{deletedTemplateUID: errors.New("object storage unreachable")}}
		r := NewActorTemplateReconciler(st, control, 7*time.Second, sweepGrace)

		r.sweepOrphanedGoldens(t.Context())
		if len(st.goldenActors) != 0 || len(st.goldenTags) != 1 {
			t.Fatalf("after a failed tag delete: actors %v, tags %v; want the actor removed, the tag kept", st.goldenActors, st.goldenTags)
		}
		control.mu.Lock()
		control.deleteTagErr = nil
		control.mu.Unlock()
		r.sweepOrphanedGoldens(t.Context())
		if len(st.goldenTags) != 0 {
			t.Fatalf("the next sweep left %v", st.goldenTags)
		}
	})

	t.Run("a failed listing removes nothing", func(t *testing.T) {
		st := newFakeTemplateStore()
		st.goldenTags = []*ateapipb.Tag{sweepTestTag(deletedTemplateUID, deletedTemplateUID, old)}
		st.listGoldensErr = errors.New("store unavailable")
		control := &sweepControl{store: st}
		NewActorTemplateReconciler(st, control, 7*time.Second, sweepGrace).sweepOrphanedGoldens(t.Context())
		if actors, tags := control.deletes(); len(actors)+len(tags) != 0 {
			t.Fatalf("deleted actors %v, tags %v on a failed listing", actors, tags)
		}
	})
}

// TestSweepOrphanedGoldens_OneReplicaAtATime sweeps against the PostgreSQL
// store's lease: while another replica holds the sweep lease, a sweep removes
// nothing; once it is released, the sweep removes the leftover golden.
func TestSweepOrphanedGoldens_OneReplicaAtATime(t *testing.T) {
	ctx := t.Context()
	persistence := newTestPersistence(t)
	storetest.MustCreateTag(t, ctx, persistence, &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: deletedTemplateUID},
		SourceActor: &ateapipb.ObjectRef{Atespace: resources.GoldenActorAtespace, Name: deletedTemplateUID},
		Scope:       ateapipb.TagScope_TAG_SCOPE_PUBLISHED,
		Status: &ateapipb.TagStatus{
			ActorTemplateUid: deletedTemplateUID,
			Snapshot:         &ateapipb.ExternalSnapshot{SnapshotUri: "gs://ate-snapshots/team-a/atespaces/ate-golden/tags/swept", ContentScope: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL},
		},
	})
	control := &recordingTagDeletes{}
	r := NewActorTemplateReconciler(persistence, control, 7*time.Second, 0)

	other, err := persistence.AcquireLease(ctx, goldenSweepLeaseKey, "other-replica")
	if err != nil {
		t.Fatalf("acquiring the sweep lease as another replica: %v", err)
	}
	r.sweepOrphanedGoldens(ctx)
	if got := control.names(); len(got) != 0 {
		t.Fatalf("swept %v while another replica held the lease", got)
	}

	other.Close()
	r.sweepOrphanedGoldens(ctx)
	if got := control.names(); !slices.Equal(got, []string{deletedTemplateUID}) {
		t.Fatalf("swept %v once the lease was free, want [%s]", got, deletedTemplateUID)
	}
}

// recordingTagDeletes records the golden tags a sweep deletes.
type recordingTagDeletes struct {
	goldenActorControl
	mu   sync.Mutex
	tags []string
}

func (c *recordingTagDeletes) DeleteTag(_ context.Context, req *ateapipb.DeleteTagRequest) (*ateapipb.Tag, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tags = append(c.tags, req.GetTag().GetName())
	return &ateapipb.Tag{}, nil
}

func (c *recordingTagDeletes) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.tags)
}

func TestGoldenTemplateUIDs(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"golden actor", goldenActorTemplateUID(sweepTestActor(liveTemplateUID, time.Now())), liveTemplateUID},
		{"actor outside the reserved atespace", goldenActorTemplateUID(&ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: liveTemplateUID}, ActorTemplate: &ateapipb.ObjectRef{Name: "t"}}), ""},
		{"actor not named after a uid", goldenActorTemplateUID(&ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: "scratch"}, ActorTemplate: &ateapipb.ObjectRef{Name: "t"}}), ""},
		{"actor named after a uid in another spelling", goldenActorTemplateUID(&ateapipb.Actor{Metadata: &ateapipb.ResourceMetadata{Atespace: resources.GoldenActorAtespace, Name: "{" + liveTemplateUID + "}"}, ActorTemplate: &ateapipb.ObjectRef{Name: "t"}}), ""},
		{"first golden tag", goldenTagTemplateUID(sweepTestTag(liveTemplateUID, liveTemplateUID, time.Now())), liveTemplateUID},
		{"golden tag taken again", goldenTagTemplateUID(sweepTestTag(liveTemplateUID, retakenGoldenTagName(liveTemplateUID), time.Now())), liveTemplateUID},
		{"tag of another name", goldenTagTemplateUID(sweepTestTag(liveTemplateUID, "user-tag", time.Now())), ""},
		{"tag outside the reserved atespace", goldenTagTemplateUID(&ateapipb.Tag{Metadata: &ateapipb.ResourceMetadata{Atespace: testAtespace, Name: liveTemplateUID}, SourceActor: &ateapipb.ObjectRef{Atespace: resources.GoldenActorAtespace, Name: liveTemplateUID}}), ""},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: template uid = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
