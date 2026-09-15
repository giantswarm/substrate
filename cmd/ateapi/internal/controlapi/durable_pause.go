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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/agent-substrate/substrate/internal/objectstore"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
)

// A pause checkpoints the workload onto its worker's node and nowhere else:
// the node holds the only copy of the actor's state, and a node that goes
// away — preempted, scaled down, rolled — takes it along while the record
// still says PAUSED there. The uploader below makes every pause durable
// shortly after it completes: the local checkpoint is copied to the actor's
// snapshot storage the way a suspend of a paused actor copies it, and the
// copy is recorded as the actor's external snapshot, marked with the local
// snapshot it came from. The actor stays PAUSED; its local copy stays the
// fast resume path on its node, the uploaded one lets it resume on any other
// worker and be suspended without the node.

const (
	// pauseUploadResyncInterval is how often stored actors are listed for
	// pauses that still lack a durable copy: a pause finished right before a
	// restart, an upload that exhausted its retries, actors paused before
	// uploads existed.
	pauseUploadResyncInterval = time.Minute
	pauseUploadListPageSize   = 100
	// pauseUploadWorkerCount bounds the uploads in flight per replica; an
	// upload is one atelet RPC that streams the checkpoint files.
	pauseUploadWorkerCount = 2
	// pauseUploadMaxRetries bounds the rate-limited requeues of one failing
	// upload before the resync takes over.
	pauseUploadMaxRetries = 5
	// pauseUploadResyncGrace is how long the resync leaves a fresh pause to
	// the upload its own PauseActor queued — in this replica or another —
	// before enqueuing the actor itself.
	pauseUploadResyncGrace = 30 * time.Second
	// localCheckpointPruneTimeout bounds the best-effort prune of a local
	// checkpoint, so a node that stopped answering cannot stall the commit
	// that no longer needs it.
	localCheckpointPruneTimeout = 10 * time.Second
)

// durablePauseCopy returns the actor's external snapshot when it is the
// uploaded copy of the local snapshot the actor holds, nil otherwise — no
// local snapshot, no external snapshot, or an external snapshot from an
// earlier suspend. The two hold the same state, so the actor may restore from
// either; anything older must never stand in for the pause.
func durablePauseCopy(actor *ateapipb.Actor) *ateapipb.ExternalSnapshot {
	local, external := actor.GetStatus().GetLocalSnapshotInfo(), actor.GetStatus().GetExternalSnapshot()
	if local.GetSnapshotName() == "" || external.GetSnapshotUri() == "" || external.GetSourceLocalSnapshotName() != local.GetSnapshotName() {
		return nil
	}
	return external
}

// needsDurablePauseCopy reports whether the actor is paused on a node-local
// snapshot that has no durable copy yet.
func needsDurablePauseCopy(actor *ateapipb.Actor) bool {
	st := actor.GetStatus()
	return st.GetState() == ateapipb.ActorState_ACTOR_STATE_PAUSED &&
		st.GetLocalSnapshotInfo().GetSnapshotName() != "" &&
		len(st.GetLocalSnapshotInfo().GetNodeVmsWithLocalSnapshots()) > 0 &&
		durablePauseCopy(actor) == nil
}

// pauseUploader uploads pause snapshots in the background. PauseActor enqueues
// the actor it paused; a periodic resync enqueues every paused actor whose
// snapshot still lacks a durable copy.
type pauseUploader struct {
	w     *ActorWorkflow
	queue workqueue.TypedRateLimitingInterface[resources.ActorRef]
}

func newPauseUploader(w *ActorWorkflow) *pauseUploader {
	return &pauseUploader{
		w:     w,
		queue: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[resources.ActorRef]()),
	}
}

// Start launches the queue workers and the resync; ctx ends them.
func (u *pauseUploader) Start(ctx context.Context) {
	go func() {
		defer u.queue.ShutDown()
		for range pauseUploadWorkerCount {
			go wait.UntilWithContext(ctx, u.runWorker, time.Second)
		}
		wait.UntilWithContext(ctx, u.resync, pauseUploadResyncInterval)
	}()
}

// Enqueue schedules the upload of an actor's pause snapshot. A nil uploader
// (a workflow built without one) accepts and drops it.
func (u *pauseUploader) Enqueue(actorRef resources.ActorRef) {
	if u == nil {
		return
	}
	u.queue.Add(actorRef)
}

// resync lists the stored actors and enqueues the paused ones without a
// durable copy, leaving a fresh pause to the upload PauseActor queued.
func (u *pauseUploader) resync(ctx context.Context) {
	pageToken := ""
	for {
		page, err := u.w.store.ListActors(ctx, "", store.ListOptions{PageSize: pauseUploadListPageSize, PageToken: pageToken})
		if err != nil {
			slog.ErrorContext(ctx, "Failed to list actors for pause snapshot uploads", slog.Any("err", err))
			return
		}
		for _, actor := range page.Items {
			if pauseAwaitsResync(actor, time.Now()) {
				u.queue.Add(resources.ActorRefFromActor(actor))
			}
		}
		if page.NextPageToken == "" {
			return
		}
		pageToken = page.NextPageToken
	}
}

// pauseAwaitsResync reports whether the resync should upload the actor's
// pause snapshot: it lacks a durable copy and is older than the grace the
// upload queued at pause time gets. A PAUSED actor's last write is its pause
// finalize, so its update time is when it was paused.
func pauseAwaitsResync(actor *ateapipb.Actor, now time.Time) bool {
	return needsDurablePauseCopy(actor) &&
		now.Sub(actor.GetMetadata().GetUpdateTime().AsTime()) >= pauseUploadResyncGrace
}

func (u *pauseUploader) runWorker(ctx context.Context) {
	for u.processNextWorkItem(ctx) {
	}
}

func (u *pauseUploader) processNextWorkItem(ctx context.Context) bool {
	actorRef, quit := u.queue.Get()
	if quit {
		return false
	}
	defer u.queue.Done(actorRef)

	err := u.w.uploadPauseSnapshot(ctx, actorRef)
	if err == nil {
		u.queue.Forget(actorRef)
		return true
	}
	attrs := append(ateattr.ActorRefLogAttrs(actorRef), slog.Any("err", err))
	if u.queue.NumRequeues(actorRef) < pauseUploadMaxRetries {
		slog.LogAttrs(ctx, slog.LevelWarn, "Pause snapshot upload failed; retrying", attrs...)
		u.queue.AddRateLimited(actorRef)
		return true
	}
	slog.LogAttrs(ctx, slog.LevelError, "Pause snapshot upload failed; giving up until the next resync", attrs...)
	u.queue.Forget(actorRef)
	return true
}

// uploadPauseSnapshot makes one paused actor's snapshot durable: it asks the
// atelet on the snapshot's node to upload the local checkpoint, keeping the
// local copy, then records the upload as the actor's external snapshot. The
// actor is not leased and stays PAUSED throughout — a resume that arrives
// meanwhile is not held up — so the record is committed under a version
// precondition and an upload the actor has outrun is discarded. The
// destination is named after the local snapshot: a retry re-sends the same
// request and overwrites the same objects, with the remote manifest as the
// commit marker.
func (w *ActorWorkflow) uploadPauseSnapshot(ctx context.Context, actorRef resources.ActorRef) (err error) {
	ctx, cancel := context.WithTimeout(ctx, w.workflowDeadline)
	defer cancel()
	ctx, done := stepSpan(ctx, "UploadPauseSnapshot")
	defer func() { err = done(err) }()

	actor, err := w.store.GetActor(ctx, actorRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			markSkipped(ctx, "the actor is gone")
			return nil
		}
		return err
	}
	if !needsDurablePauseCopy(actor) {
		markSkipped(ctx, "the actor is not paused on a local snapshot without a durable copy")
		return nil
	}
	if w.dialer == nil {
		return errors.New("no atelet dialer configured")
	}
	actorTemplate, err := resolveActorTemplate(ctx, w.store, actor)
	if err != nil {
		return err
	}
	local := actor.GetStatus().GetLocalSnapshotInfo()
	uri, err := inProgressSnapshotURI(actorTemplate, actor, local.GetSnapshotName())
	if err != nil {
		return err
	}
	// The scope the pause captured, not the template's commit scope: the copy
	// must restore exactly as the local snapshot would, memory included.
	scope := pausedContentScope(local, actorTemplate)

	node := local.GetNodeVmsWithLocalSnapshots()[0]
	ateletConn, err := w.dialer.DialForAteletOnNode(node)
	if err != nil {
		if errors.Is(err, ErrNoAteletOnNode) {
			// The atelet is restarting, or the node is gone with the snapshot;
			// the resync looks again, and a resume tells the two apart.
			slog.LogAttrs(ctx, slog.LevelInfo, "No atelet on the node holding the pause snapshot; not uploaded",
				append(ateattr.ActorRefLogAttrs(actorRef), slog.String("node", node))...)
			return nil
		}
		return fmt.Errorf("while getting atelet conn for node %q: %w", node, err)
	}
	req := &ateletpb.UploadPausedCheckpointRequest{
		Atespace:               actor.GetMetadata().GetAtespace(),
		ActorName:              actor.GetMetadata().GetName(),
		ActorUid:               actor.GetMetadata().GetUid(),
		ActorTemplateAtespace:  actor.GetActorTemplate().GetAtespace(),
		ActorTemplateName:      actor.GetActorTemplate().GetName(),
		LocalSnapshotName:      local.GetSnapshotName(),
		DestinationSnapshotUri: uri.String(),
		DesiredScope:           actorSnapshotContentScopeToAtelet(scope),
		KeepLocal:              true,
	}
	if _, err := ateletpb.NewAteomHerderClient(ateletConn).UploadPausedCheckpoint(ctx, req); err != nil {
		if ateerrors.ActorCrashRequested(err) {
			// The local snapshot is gone from its node: nothing to upload. The
			// actor is left as it is; its next resume fails with the cause.
			slog.LogAttrs(ctx, slog.LevelWarn, "Pause snapshot cannot be uploaded",
				append(ateattr.ActorRefLogAttrs(actorRef), slog.String("node", node), slog.Any("err", err))...)
			return nil
		}
		return fmt.Errorf("while uploading the pause snapshot from node %q: %w", node, err)
	}
	return w.recordDurablePauseCopy(ctx, actorRef, &ateapipb.ExternalSnapshot{
		SnapshotUri:             uri.String(),
		ContentScope:            scope,
		SourceLocalSnapshotName: local.GetSnapshotName(),
	})
}

// recordDurablePauseCopy commits an uploaded pause snapshot as the actor's
// external snapshot, if the actor is still paused on the local snapshot it
// was uploaded from, and then releases the external snapshot it replaces —
// committed first, so a release that fails leaves an orphan the actor's
// deletion collects rather than an actor with no snapshot. The commit runs
// under the actor's lease, held for the two store round trips only: the
// lifecycle workflows read the record when they take the lease and update it
// under a version precondition, so a write that slipped in between would fail
// their update with a conflict the caller sees as Aborted. A lease another
// operation holds is a retry, not an error to act on. An upload the actor has
// outrun (resumed, suspended, deleted meanwhile) is deleted again: nothing
// references it.
func (w *ActorWorkflow) recordDurablePauseCopy(ctx context.Context, actorRef resources.ActorRef, uploaded *ateapipb.ExternalSnapshot) error {
	replaced, err := w.commitDurablePauseCopy(ctx, actorRef, uploaded)
	if err != nil || replaced == nil {
		return err
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "Pause snapshot is durable",
		append(ateattr.ActorRefLogAttrs(actorRef),
			slog.String("local_snapshot", uploaded.GetSourceLocalSnapshotName()),
			slog.String("snapshot_uri", uploaded.GetSnapshotUri()))...)
	if err := w.releaseReplacedSnapshot(ctx, replaced, uploaded); err != nil {
		slog.LogAttrs(ctx, slog.LevelWarn, "Failed to release the external snapshot the pause copy replaces; the actor's deletion collects it",
			append(ateattr.ActorRefLogAttrs(actorRef), slog.Any("err", err))...)
	}
	return nil
}

// commitDurablePauseCopy is the leased part of recordDurablePauseCopy. It
// returns the record as it was before the commit — whose external snapshot
// the copy replaces — or nil when nothing was committed because the actor
// moved on (the upload is then discarded) or another replica recorded the
// same copy.
func (w *ActorWorkflow) commitDurablePauseCopy(ctx context.Context, actorRef resources.ActorRef, uploaded *ateapipb.ExternalSnapshot) (replaced *ateapipb.Actor, err error) {
	leaseCtx, lease, err := acquireLease(ctx, w.store, actorLeaseKey(actorRef), "actor")
	if err != nil {
		return nil, err
	}
	defer lease.Close()

	actor, err := w.store.GetActor(leaseCtx, actorRef)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, w.discardStaleUpload(ctx, actorRef, uploaded)
		}
		return nil, err
	}
	if !needsDurablePauseCopy(actor) || actor.GetStatus().GetLocalSnapshotInfo().GetSnapshotName() != uploaded.GetSourceLocalSnapshotName() {
		if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() == uploaded.GetSnapshotUri() {
			// Another replica recorded the same upload.
			return nil, nil
		}
		return nil, w.discardStaleUpload(ctx, actorRef, uploaded)
	}
	if _, err := w.store.UpdateActor(leaseCtx, actorRef, store.PreconditionFrom(actor), func(toUpdate *ateapipb.Actor) error {
		toUpdate.Status.ExternalSnapshot = proto.CloneOf(uploaded)
		return nil
	}); err != nil {
		return nil, err
	}
	return actor, nil
}

// discardStaleUpload deletes an uploaded pause snapshot the actor moved on
// from before it was recorded.
func (w *ActorWorkflow) discardStaleUpload(ctx context.Context, actorRef resources.ActorRef, uploaded *ateapipb.ExternalSnapshot) error {
	if w.objectStore == nil {
		return nil
	}
	uri, err := resources.ParseSnapshotURI(uploaded.GetSnapshotUri())
	if err != nil {
		return fmt.Errorf("while parsing the uploaded pause snapshot %q: %w", uploaded.GetSnapshotUri(), err)
	}
	slog.LogAttrs(ctx, slog.LevelInfo, "Discarding a pause snapshot upload the actor moved on from",
		append(ateattr.ActorRefLogAttrs(actorRef), slog.String("snapshot_uri", uploaded.GetSnapshotUri()))...)
	return objectstore.DeletePrefix(ctx, w.objectStore, uri.Prefix())
}

// releaseLocalCheckpoints asks the atelet on each node holding the actor's
// local pause snapshot to delete it. Best-effort: nothing a caller commits
// depends on it, and no atelet on the node means the node is restarting or
// gone — the very case a durable copy exists for.
func (w *ActorWorkflow) releaseLocalCheckpoints(ctx context.Context, actor *ateapipb.Actor) {
	if w.dialer == nil {
		return
	}
	actorRef := resources.ActorRefFromActor(actor)
	for _, node := range actor.GetStatus().GetLocalSnapshotInfo().GetNodeVmsWithLocalSnapshots() {
		ateletConn, err := w.dialer.DialForAteletOnNode(node)
		if err != nil {
			slog.LogAttrs(ctx, slog.LevelInfo, "Leaving the local pause snapshot on its node: no atelet to prune it",
				append(ateattr.ActorRefLogAttrs(actorRef), slog.String("node", node), slog.Any("err", err))...)
			continue
		}
		pruneCtx, cancel := context.WithTimeout(ctx, localCheckpointPruneTimeout)
		_, err = ateletpb.NewAteomHerderClient(ateletConn).PruneLocalCheckpoints(pruneCtx, &ateletpb.PruneLocalCheckpointsRequest{
			Atespace:  actor.GetMetadata().GetAtespace(),
			ActorName: actor.GetMetadata().GetName(),
			ActorUid:  actor.GetMetadata().GetUid(),
		})
		cancel()
		if err != nil {
			slog.LogAttrs(ctx, slog.LevelWarn, "Failed to prune the local pause snapshot",
				append(ateattr.ActorRefLogAttrs(actorRef), slog.String("node", node), slog.Any("err", err))...)
		}
	}
}
