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
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

const (
	templateListPageSize = 100

	// templateWorkerCount is the number of goroutines draining the work
	// queue.
	templateWorkerCount = 5

	// goldenSnapshotWarmup is the default wall-clock delay between resuming
	// the golden actor and taking its snapshot, for templates without a
	// wakeup probe on every container.
	goldenSnapshotWarmup = 20 * time.Second
)

const (
	reasonGoldenTagConflict  = "GoldenTagConflict"
	reasonGoldenActorInvalid = "GoldenActorInvalid"
	reasonGoldenActorCrashed = "GoldenActorCrashed"
	reasonUnexpectedState    = "GoldenActorUnexpectedState"
	// reasonGoldenSnapshotLost fails a template whose golden snapshot, recorded
	// by a release before golden tags, cannot be made into a golden tag: the
	// golden actor that held it is gone, holds no snapshot, or refuses the copy.
	reasonGoldenSnapshotLost = "GoldenSnapshotLost"
)

// maxGoldenErrorMessageLen is the maxLength of
// GoldenSnapshotStatus.error_message.
const maxGoldenErrorMessageLen = 4096

// templateReconcilerStore enumerates the exact storage methods needed by
// ActorTemplateReconciler and nothing more.
type templateReconcilerStore interface {
	GetActorTemplate(ctx context.Context, templateRef resources.ActorTemplateRef) (*ateapipb.ActorTemplate, error)
	ListActorTemplates(ctx context.Context, atespace string, opts store.ListOptions) (store.ListResponse[*ateapipb.ActorTemplate], error)
	UpdateActorTemplate(ctx context.Context, templateRef resources.ActorTemplateRef, precondition store.Precondition, mutate func(dbTemplate *ateapipb.ActorTemplate) error) (*ateapipb.ActorTemplate, error)
	// UpdateActor repairs the golden actor row a release before golden tags
	// left without the template its snapshot was built under.
	UpdateActor(ctx context.Context, actorRef resources.ActorRef, precondition store.Precondition, mutate func(toUpdate *ateapipb.Actor) error) (*ateapipb.Actor, error)
	AcquireLease(ctx context.Context, key string) (*store.Lease, error)
}

// goldenActorControl is the in-process slice of the Control service the
// reconciler drives golden actors through. *RPCService satisfies it.
type goldenActorControl interface {
	GetTag(ctx context.Context, req *ateapipb.GetTagRequest) (*ateapipb.Tag, error)
	CreateTag(ctx context.Context, req *ateapipb.CreateTagRequest) (*ateapipb.Tag, error)
	DeleteTag(ctx context.Context, req *ateapipb.DeleteTagRequest) (*ateapipb.Tag, error)
	DeleteActor(ctx context.Context, req *ateapipb.DeleteActorRequest) (*ateapipb.Actor, error)
	CreateAtespace(ctx context.Context, req *ateapipb.CreateAtespaceRequest) (*ateapipb.Atespace, error)
	CreateActor(ctx context.Context, req *ateapipb.CreateActorRequest) (*ateapipb.Actor, error)
	GetActor(ctx context.Context, req *ateapipb.GetActorRequest) (*ateapipb.Actor, error)
	ResumeActor(ctx context.Context, req *ateapipb.ResumeActorRequest) (*ateapipb.ResumeActorResponse, error)
	SuspendActor(ctx context.Context, req *ateapipb.SuspendActorRequest) (*ateapipb.SuspendActorResponse, error)
}

// ActorTemplateReconciler drives stored ActorTemplates through the golden
// actor state machine.
type ActorTemplateReconciler struct {
	persistence    templateReconcilerStore
	control        goldenActorControl
	queue          workqueue.TypedRateLimitingInterface[resources.ActorTemplateRef]
	resyncInterval time.Duration
}

func NewActorTemplateReconciler(persistence templateReconcilerStore, control goldenActorControl, resyncInterval time.Duration) *ActorTemplateReconciler {
	return &ActorTemplateReconciler{
		persistence:    persistence,
		control:        control,
		resyncInterval: resyncInterval,
		// Create rate-limiting queue with exponential backoff
		queue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[resources.ActorTemplateRef]()),
	}
}

// Start launches the queue workers and the resync producer; there is no event
// source for stored templates, so the periodic list is the event source.
func (r *ActorTemplateReconciler) Start(ctx context.Context) {
	go func() {
		defer r.queue.ShutDown()
		for range templateWorkerCount {
			go wait.UntilWithContext(ctx, r.runWorker, time.Second)
		}
		wait.UntilWithContext(ctx, r.resync, r.resyncInterval)
	}()
}

// resync lists ActorTemplate and adds them to the work queue.
func (r *ActorTemplateReconciler) resync(ctx context.Context) {
	pageToken := ""
	for {
		// TODO: need sharding
		page, err := r.persistence.ListActorTemplates(ctx, "", store.ListOptions{PageSize: templateListPageSize, PageToken: pageToken})
		if err != nil {
			slog.ErrorContext(ctx, "Failed to list actor templates", slog.Any("err", err))
			return
		}
		for _, tmpl := range page.Items {
			ref := resources.ActorTemplateRefFromActorTemplate(tmpl)
			if goldenSnapshotDone(tmpl.GetStatus().GetGoldenSnapshotStatus()) {
				slog.DebugContext(ctx, "Skipping actor template with terminal golden snapshot status", slog.String("ActorTemplate", ref.String()))
			} else {
				r.queue.Add(ref)
				slog.InfoContext(ctx, "Added actor template to work queue", slog.String("ActorTemplate", ref.String()))
			}
		}
		if page.NextPageToken == "" {
			return
		}
		pageToken = page.NextPageToken
	}
}

func (r *ActorTemplateReconciler) runWorker(ctx context.Context) {
	for r.processNextWorkItem(ctx) {
	}
}

func (r *ActorTemplateReconciler) processNextWorkItem(ctx context.Context) bool {
	ref, quit := r.queue.Get()
	if quit {
		return false
	}
	defer r.queue.Done(ref)

	requeueAfter, err := r.reconcileOne(ctx, ref)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to reconcile actor template, requeueing",
			slog.Any("template", ref),
			slog.Any("err", err))
		r.queue.AddRateLimited(ref)
		return true
	}
	r.queue.Forget(ref)
	if requeueAfter > 0 {
		r.queue.AddAfter(ref, requeueAfter)
	}
	return true
}

// reconcileOne advances one template as far as it can go in this pass, holding
// the template's lease so concurrent replicas don't interleave transitions. The
// pass is level-triggered: each iteration re-derives the next action from the
// observed golden actor rather than stored progress, so every action must be
// reentrant. A positive requeueAfter asks the caller to revisit the template
// once its snapshot deadline (or a transitional actor state) passes.
func (r *ActorTemplateReconciler) reconcileOne(ctx context.Context, ref resources.ActorTemplateRef) (requeueAfter time.Duration, err error) {
	lease, err := r.persistence.AcquireLease(ctx, "lease:actortemplate:"+ref.Atespace+":"+ref.Name)
	if err != nil {
		if errors.Is(err, store.ErrLeaseConflict) {
			// Another replica owns this template for now.
			return 0, nil
		}
		return 0, fmt.Errorf("while acquiring lease: %w", err)
	}
	defer lease.Close()
	ctx = lease.Context()

	tmpl, err := r.persistence.GetActorTemplate(ctx, ref)
	if err != nil {
		// Template was deleted after we enqueued the work item.
		if errors.Is(err, store.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}

	goldenActorRef := &ateapipb.ObjectRef{
		// Golden actors live in the reserved ate-golden atespace, because
		// the suspend workflow relies on the ate-golden system atespace to
		// always take a full snapshot of the golden actor.
		// https://github.com/agent-substrate/substrate/blob/cb7c8385ef2bb489c3d5f7bfa71820fd33935d91/cmd/ateapi/internal/controlapi/workflow_suspend.go#L170-L173
		Atespace: resources.GoldenActorAtespace,
		// Use the template's UID as golden actor's name to prevent collision
		// when templates are recreated with the same name.
		Name: tmpl.GetMetadata().GetUid(),
	}

	// Each iteration observes the golden actor, takes the one action that
	// fact demands, and re-observes; the pass ends at a terminal condition,
	// a deadline wait, or an error the workqueue retries.
	for {
		goldenSnapshotStatus := tmpl.GetStatus().GetGoldenSnapshotStatus()
		if goldenSnapshotStatus.GetErrorMessage() != "" {
			// The snapshot has already failed.
			return 0, nil
		}
		if legacyGoldenSnapshot(goldenSnapshotStatus) {
			return 0, r.migrateLegacyGolden(ctx, tmpl, goldenActorRef)
		}
		if goldenSnapshotStatus.GetGoldenTag() != nil {
			// The golden snapshot exists already.
			return 0, nil
		}

		// A completed tag survives a crash during actor deletion or checkpointing.
		tag, err := r.control.GetTag(ctx, &ateapipb.GetTagRequest{Tag: goldenActorRef})
		if err != nil && status.Code(err) != codes.NotFound {
			return 0, fmt.Errorf("while getting golden tag: %w", err)
		}
		if err == nil {
			if tag.GetStatus().GetActorTemplateUid() != tmpl.GetMetadata().GetUid() || resources.ActorRefFromObjectRef(tag.GetSourceActor()) != resources.ActorRefFromObjectRef(goldenActorRef) {
				return 0, r.fail(ctx, tmpl, reasonGoldenTagConflict, "golden tag belongs to another actor or template")
			}
			if tag.GetStatus().GetSnapshot().GetSnapshotUri() != "" {
				return 0, r.saveGoldenTag(ctx, tmpl, goldenActorRef)
			}
			// CreateTag cannot resume an incomplete copy. Delete it before retrying.
			if _, err := r.control.DeleteTag(ctx, &ateapipb.DeleteTagRequest{Tag: goldenActorRef}); err != nil && status.Code(err) != codes.NotFound {
				return 0, fmt.Errorf("while deleting incomplete golden tag: %w", err)
			}
		}

		actor, err := r.ensureActorExists(ctx, tmpl, goldenActorRef)
		if err != nil {
			if status.Code(err) == codes.InvalidArgument {
				// Invalid template spec; retrying can't help.
				return 0, r.fail(ctx, tmpl, reasonGoldenActorInvalid, err.Error())
			}
			return 0, err
		}

		switch state := actor.GetStatus().GetState(); state {
		case ateapipb.ActorState_ACTOR_STATE_CRASHED:
			return 0, r.fail(ctx, tmpl, reasonGoldenActorCrashed, "golden actor crashed before its snapshot was taken")

		case ateapipb.ActorState_ACTOR_STATE_RUNNING:
			takeAt := goldenSnapshotStatus.GetTakeGoldenSnapshotAt()
			if takeAt == nil {
				// Resumed, but the deadline write was lost (a replica died
				// after ResumeActor). The elapsed warmup is unknowable, so
				// restart the clock.
				slog.WarnContext(ctx, "Golden actor running without a snapshot deadline; restarting warmup", slog.String("ActorTemplate", ref.String()))
				deadline := time.Now().Add(goldenSnapshotWarmupFor(tmpl.GetContainers()))
				if tmpl, err = r.checkpoint(ctx, tmpl, func(snapshotStatus *ateapipb.GoldenSnapshotStatus) {
					snapshotStatus.TakeGoldenSnapshotAt = timestamppb.New(deadline)
				}); err != nil {
					return 0, err
				}
				continue
			}
			// Not time to take the golden snapshot yet; requeue.
			if rem := time.Until(takeAt.AsTime()); rem > 0 {
				return rem, nil
			}
			// Warmup done: suspend the golden actor and record its snapshot.
			err := r.suspendActor(ctx, goldenActorRef)
			if err != nil {
				return 0, err
			}
			return 0, r.tagGoldenActor(ctx, tmpl, goldenActorRef)

		case ateapipb.ActorState_ACTOR_STATE_SUSPENDING:
			// A previous pass died mid-suspend; retry suspend.
			err := r.suspendActor(ctx, goldenActorRef)
			if err != nil {
				return 0, err
			}
			return 0, r.tagGoldenActor(ctx, tmpl, goldenActorRef)

		case ateapipb.ActorState_ACTOR_STATE_RESUMING,
			ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
			// The golden actor was never resumed, or a previous resume didn't
			// finish; ResumeActor is reentrant from both.
			if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() != "" {
				// Golden actors never start from a source snapshot, so an
				// existing snapshot means an earlier suspend completed
				// without being recorded.
				return 0, r.tagGoldenActor(ctx, tmpl, goldenActorRef)
			}
			if _, err := r.control.ResumeActor(ctx, &ateapipb.ResumeActorRequest{Actor: goldenActorRef}); err != nil {
				// A crash during resume is observed as CRASHED on the retry.
				return 0, fmt.Errorf("while resuming golden actor: %w", err)
			}
			deadline := time.Now().Add(goldenSnapshotWarmupFor(tmpl.GetContainers()))
			if tmpl, err = r.checkpoint(ctx, tmpl, func(snapshotStatus *ateapipb.GoldenSnapshotStatus) {
				snapshotStatus.TakeGoldenSnapshotAt = timestamppb.New(deadline)
			}); err != nil {
				return 0, err
			}
		case ateapipb.ActorState_ACTOR_STATE_DELETING, ateapipb.ActorState_ACTOR_STATE_PAUSED, ateapipb.ActorState_ACTOR_STATE_PAUSING, ateapipb.ActorState_ACTOR_STATE_REVERTING:
			// Nothing in the golden flow deletes, pauses, or reverts the actor before
			// the snapshot is taken; someone else interfered.
			return 0, r.fail(ctx, tmpl, reasonUnexpectedState, fmt.Sprintf("golden actor in unexpected state %v", state))

		default:
			return r.resyncInterval, nil
		}
	}
}

// suspendActor waits for the golden actor to produce an external snapshot.
// SuspendActor completes an in-flight suspend and is a no-op if already suspended.
func (r *ActorTemplateReconciler) suspendActor(ctx context.Context, goldenRef *ateapipb.ObjectRef) error {
	resp, err := r.control.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: goldenRef})
	if err != nil {
		// A crash during suspend is observed as CRASHED on the retry.
		return fmt.Errorf("while suspending golden actor: %w", err)
	}
	suspended := resp.GetActor().GetStatus().GetExternalSnapshot()
	if suspended.GetSnapshotUri() == "" {
		return fmt.Errorf("suspending golden actor produced no external snapshot")
	}
	return nil
}

// tagGoldenActor copies the snapshot into a tag before releasing the actor's copy.
func (r *ActorTemplateReconciler) tagGoldenActor(ctx context.Context, tmpl *ateapipb.ActorTemplate, ref *ateapipb.ObjectRef) error {
	_, err := r.control.CreateTag(ctx, &ateapipb.CreateTagRequest{Tag: &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: ref.GetAtespace(), Name: ref.GetName()},
		SourceActor: ref,
		Scope:       ateapipb.TagScope_TAG_SCOPE_PUBLISHED,
	}})
	if err != nil {
		return fmt.Errorf("while creating golden tag: %w", err)
	}
	return r.saveGoldenTag(ctx, tmpl, ref)
}

// saveGoldenTag finishes cleanup before recording terminal success, so retries
// can rediscover the tag even if deletion or the status write fails.
func (r *ActorTemplateReconciler) saveGoldenTag(ctx context.Context, tmpl *ateapipb.ActorTemplate, ref *ateapipb.ObjectRef) error {
	if _, err := r.control.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: ref}); err != nil && status.Code(err) != codes.NotFound {
		return fmt.Errorf("while deleting golden actor: %w", err)
	}
	_, err := r.checkpoint(ctx, tmpl, func(snapshotStatus *ateapipb.GoldenSnapshotStatus) {
		snapshotStatus.GoldenTag = ref
	})
	return err
}

// checkpoint commits a golden snapshot status mutation unless a concurrent
// writer already drove the template to a terminal state. The write is guarded
// by the observed template's uid and version, so a stale observation surfaces
// as a conflict for the workqueue to retry.
func (r *ActorTemplateReconciler) checkpoint(ctx context.Context, observed *ateapipb.ActorTemplate, mutate func(*ateapipb.GoldenSnapshotStatus)) (*ateapipb.ActorTemplate, error) {
	ref := resources.ActorTemplateRefFromActorTemplate(observed)
	updated, err := r.persistence.UpdateActorTemplate(ctx, ref, store.PreconditionFrom(observed), func(dbTemplate *ateapipb.ActorTemplate) error {
		if goldenSnapshotDone(dbTemplate.GetStatus().GetGoldenSnapshotStatus()) {
			return fmt.Errorf("actor template reached a terminal golden snapshot state concurrently")
		}
		if dbTemplate.Status == nil {
			dbTemplate.Status = &ateapipb.ActorTemplateStatus{}
		}
		if dbTemplate.Status.GoldenSnapshotStatus == nil {
			dbTemplate.Status.GoldenSnapshotStatus = &ateapipb.GoldenSnapshotStatus{}
		}
		mutate(dbTemplate.Status.GoldenSnapshotStatus)
		return nil
	})

	return updated, err
}

// fail commits the terminal error message, prefixed with a machine-readable
// reason and truncated to fit error_message's bound.
func (r *ActorTemplateReconciler) fail(ctx context.Context, observed *ateapipb.ActorTemplate, reason, msg string) error {
	_, err := r.checkpoint(ctx, observed, func(snapshotStatus *ateapipb.GoldenSnapshotStatus) {
		snapshotStatus.ErrorMessage = truncateUTF8(reason+": "+msg, maxGoldenErrorMessageLen)
	})
	return err
}

// truncateUTF8 cuts s to at most n bytes without splitting a UTF-8 sequence,
// since proto string fields must hold valid UTF-8.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// goldenSnapshotDone reports whether the golden snapshot build reached a
// terminal state: the golden tag was recorded, or the build failed. A golden
// snapshot recorded before golden tags is not done: its migration into a tag
// is still owed.
func goldenSnapshotDone(snapshotStatus *ateapipb.GoldenSnapshotStatus) bool {
	if snapshotStatus.GetErrorMessage() != "" {
		return true
	}
	return snapshotStatus.GetGoldenTag() != nil && !legacyGoldenSnapshot(snapshotStatus)
}

// migrateLegacyGolden rewrites a golden snapshot status a release before golden
// tags recorded into the shape a resume reads today. That release pointed the
// template at the golden actor's own external snapshot and kept the actor
// suspended on it, so the golden tag is made from that actor: its row is given
// the template its snapshot was built under, which that release did not record
// and the tag workflow requires, the snapshot is copied into the tag through
// CreateTag, and the actor is released and the tag recorded as in the golden
// flow. Reentrant: a finished tag from an earlier pass is recorded, an
// unfinished one deleted and made again. A golden that cannot be recovered -
// the actor and the tag are gone, the actor holds no snapshot, or the copy is
// refused - fails the template as GoldenSnapshotLost with what is missing, so a
// reader tells a lost golden from one still awaiting its migration, and the
// migration runs once per template: its outcome, the named tag or the failure,
// is terminal.
func (r *ActorTemplateReconciler) migrateLegacyGolden(ctx context.Context, tmpl *ateapipb.ActorTemplate, goldenActorRef *ateapipb.ObjectRef) error {
	ref := resources.ActorTemplateRefFromActorTemplate(tmpl)
	legacyURI := tmpl.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag().GetAtespace()
	goldenActor := resources.ActorRefFromObjectRef(goldenActorRef)
	log := slog.With(slog.String("ActorTemplate", ref.String()), slog.String("legacySnapshot", legacyURI), slog.String("goldenActor", goldenActor.String()))

	tag, err := r.control.GetTag(ctx, &ateapipb.GetTagRequest{Tag: goldenActorRef})
	switch {
	case err == nil:
		if tag.GetStatus().GetActorTemplateUid() != tmpl.GetMetadata().GetUid() || resources.ActorRefFromObjectRef(tag.GetSourceActor()) != goldenActor {
			return r.failLegacyGolden(ctx, tmpl, reasonGoldenTagConflict, "golden tag belongs to another actor or template")
		}
		if tag.GetStatus().GetSnapshot().GetSnapshotUri() != "" {
			return r.saveMigratedGoldenTag(ctx, log, tmpl, goldenActorRef)
		}
		// CreateTag cannot resume an incomplete copy. Delete it before retrying.
		if _, err := r.control.DeleteTag(ctx, &ateapipb.DeleteTagRequest{Tag: goldenActorRef}); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("while deleting incomplete golden tag: %w", err)
		}
	case status.Code(err) != codes.NotFound:
		return fmt.Errorf("while getting golden tag: %w", err)
	}

	actor, err := r.control.GetActor(ctx, &ateapipb.GetActorRequest{Actor: goldenActorRef})
	if status.Code(err) == codes.NotFound {
		return r.failLegacyGolden(ctx, tmpl, reasonGoldenSnapshotLost, fmt.Sprintf("golden snapshot %s, recorded before golden tags, is lost: neither golden actor %s nor a tag of it exists", legacyURI, goldenActor))
	}
	if err != nil {
		return fmt.Errorf("while getting golden actor: %w", err)
	}
	if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() == "" {
		return r.failLegacyGolden(ctx, tmpl, reasonGoldenSnapshotLost, fmt.Sprintf("golden snapshot %s, recorded before golden tags, is lost: golden actor %s is %v and holds no external snapshot", legacyURI, goldenActor, actor.GetStatus().GetState()))
	}
	if actor.GetStatus().GetExternalSnapshot().GetActorTemplateUid() != tmpl.GetMetadata().GetUid() {
		// The golden actor was created from this template and never
		// repointed, so its snapshot was built under it; the release that
		// took the snapshot recorded no template on it.
		if _, err := r.persistence.UpdateActor(ctx, goldenActor, store.PreconditionFrom(actor), func(toUpdate *ateapipb.Actor) error {
			toUpdate.Status.ExternalSnapshot.ActorTemplateUid = tmpl.GetMetadata().GetUid()
			return nil
		}); err != nil {
			return fmt.Errorf("while recording the template on the golden actor's snapshot: %w", err)
		}
	}
	if _, err := r.control.CreateTag(ctx, &ateapipb.CreateTagRequest{Tag: &ateapipb.Tag{
		Metadata:    &ateapipb.ResourceMetadata{Atespace: goldenActorRef.GetAtespace(), Name: goldenActorRef.GetName()},
		SourceActor: goldenActorRef,
		Scope:       ateapipb.TagScope_TAG_SCOPE_PUBLISHED,
	}}); err != nil {
		if goldenCopyRefused(err) {
			return r.failLegacyGolden(ctx, tmpl, reasonGoldenSnapshotLost, fmt.Sprintf("golden snapshot %s, recorded before golden tags, is lost: copying it from golden actor %s into a golden tag was refused: %v", legacyURI, goldenActor, status.Convert(err).Message()))
		}
		return fmt.Errorf("while creating golden tag from the legacy golden snapshot: %w", err)
	}
	return r.saveMigratedGoldenTag(ctx, log, tmpl, goldenActorRef)
}

// saveMigratedGoldenTag records the migrated golden tag the way the golden
// flow records a new one, and logs the migration once it is recorded.
func (r *ActorTemplateReconciler) saveMigratedGoldenTag(ctx context.Context, log *slog.Logger, tmpl *ateapipb.ActorTemplate, goldenActorRef *ateapipb.ObjectRef) error {
	if err := r.saveGoldenTag(ctx, tmpl, goldenActorRef); err != nil {
		return err
	}
	log.InfoContext(ctx, "Migrated the golden snapshot recorded before golden tags into the golden tag",
		slog.String("goldenTag", resources.ActorRefFromObjectRef(goldenActorRef).String()))
	return nil
}

// failLegacyGolden commits the terminal failure of a golden snapshot recorded
// before golden tags and drops the nameless reference it was read as, so the
// status holds the failure alone.
func (r *ActorTemplateReconciler) failLegacyGolden(ctx context.Context, observed *ateapipb.ActorTemplate, reason, msg string) error {
	ref := resources.ActorTemplateRefFromActorTemplate(observed)
	slog.WarnContext(ctx, "Golden snapshot recorded before golden tags could not be migrated", slog.String("ActorTemplate", ref.String()), slog.String("reason", reason), slog.String("msg", msg))
	_, err := r.checkpoint(ctx, observed, func(snapshotStatus *ateapipb.GoldenSnapshotStatus) {
		snapshotStatus.GoldenTag = nil
		snapshotStatus.ErrorMessage = truncateUTF8(reason+": "+msg, maxGoldenErrorMessageLen)
	})
	return err
}

// legacyGoldenSnapshot reports whether the status still holds the golden
// snapshot a release before golden tags recorded. Field 1 of
// GoldenSnapshotStatus was then an ExternalSnapshot; read as the ObjectRef it
// is now, its snapshot_uri lands in atespace and the name stays empty. A
// golden tag the reconciler records always has a name.
func legacyGoldenSnapshot(snapshotStatus *ateapipb.GoldenSnapshotStatus) bool {
	ref := snapshotStatus.GetGoldenTag()
	return ref != nil && ref.GetName() == ""
}

// goldenSnapshotAwaitingMigration refuses, for the moment, a create on a
// template whose golden snapshot a release before golden tags recorded: the
// ActorTemplate reconciler migrates it into the golden tag the caller needs on
// its next pass, so the refusal is one a retry outlives.
func goldenSnapshotAwaitingMigration(actorTemplate *ateapipb.ActorTemplate) error {
	meta := actorTemplate.GetMetadata()
	return status.Errorf(codes.Unavailable, "the golden snapshot of ActorTemplate %s/%s was recorded before golden tags and awaits its migration into one; retry", meta.GetAtespace(), meta.GetName())
}

// goldenCopyRefused reports whether CreateTag refused to copy the golden actor's
// snapshot for a reason no retry outlives: the actor or its snapshot is gone, or
// the actor is not in a state its snapshot can be tagged from.
func goldenCopyRefused(err error) bool {
	switch status.Code(err) {
	case codes.NotFound, codes.FailedPrecondition, codes.InvalidArgument, codes.DataLoss:
		return true
	default:
		return false
	}
}

// goldenSnapshotWarmupFor returns 0 when every container has a wakeup probe
// (ResumeActor already blocked until the workload reported 200), and the
// default warmup otherwise.
func goldenSnapshotWarmupFor(containers []*ateapipb.Container) time.Duration {
	if len(containers) == 0 {
		return goldenSnapshotWarmup
	}
	for _, container := range containers {
		if container.GetWakeupProbe() == nil {
			return goldenSnapshotWarmup
		}
	}
	return 0
}

// ensureActorExists returns the golden actor, creating it first if it does
// not exist yet.
func (r *ActorTemplateReconciler) ensureActorExists(ctx context.Context, tmpl *ateapipb.ActorTemplate, goldenActorRef *ateapipb.ObjectRef) (*ateapipb.Actor, error) {
	actor, err := r.control.GetActor(ctx, &ateapipb.GetActorRequest{Actor: goldenActorRef})
	if err == nil {
		return actor, nil
	}
	if status.Code(err) != codes.NotFound {
		return nil, fmt.Errorf("while getting golden actor: %w", err)
	}
	// Golden actor has not yet been created. Its reserved atespace is
	// system-owned, so ensure it exists rather than assuming bootstrap did.
	if _, err := r.control.CreateAtespace(ctx, &ateapipb.CreateAtespaceRequest{
		Atespace: &ateapipb.Atespace{Metadata: &ateapipb.ResourceMetadata{Name: goldenActorRef.GetAtespace()}},
	}); err != nil && status.Code(err) != codes.AlreadyExists {
		return nil, fmt.Errorf("while ensuring atespace %q: %w", goldenActorRef.GetAtespace(), err)
	}
	actor, err = r.control.CreateActor(ctx, &ateapipb.CreateActorRequest{
		Actor: &ateapipb.Actor{
			Metadata: &ateapipb.ResourceMetadata{
				Atespace: goldenActorRef.GetAtespace(),
				Name:     goldenActorRef.GetName(),
			},
			ActorTemplate: resources.ActorTemplateRefFromActorTemplate(tmpl).ToObjectRef(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("while creating golden actor: %w", err)
	}
	return actor, nil
}
