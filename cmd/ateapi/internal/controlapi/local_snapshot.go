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

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/scheduling"
	"github.com/agent-substrate/substrate/internal/ateattr"
	"github.com/agent-substrate/substrate/internal/ateerrors"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// localSnapshotNodes returns the nodes a PAUSED actor's local snapshot is
// recorded on, without empty entries. A record written before pause
// finalization refused an unknown node can carry [""]; no worker has an
// empty node name, so left in place that entry would exclude every worker
// and make the actor unschedulable for good.
func localSnapshotNodes(actor *ateapipb.Actor) []string {
	recorded := actor.GetStatus().GetLocalSnapshotInfo().GetNodeVmsWithLocalSnapshots()
	nodes := make([]string, 0, len(recorded))
	for _, n := range recorded {
		if n != "" {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// localSnapshotNodesGone reports whether every node holding the actor's
// local snapshot has left the cluster. It is answered from the Node informer,
// the one signal a worker's absence cannot fake: a pool rolling, an ateom
// restarting or the atelet DaemonSet updating leave a node without an ACTIVE
// worker for a while, with its disk -- and the snapshot on it -- intact.
// Without a lister nothing is known, and nothing is declared lost.
func (w *ActorWorkflow) localSnapshotNodesGone(nodes []string) (bool, error) {
	if w.nodeLister == nil || len(nodes) == 0 {
		return false, nil
	}
	for _, name := range nodes {
		_, err := w.nodeLister.Get(name)
		if err == nil {
			return false, nil
		}
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("while looking up node %q: %w", name, err)
		}
	}
	return true, nil
}

// localSnapshotLost is the terminal answer for a PAUSED actor whose local
// snapshot went with its node. With no durable snapshot to fall back to the
// actor is crashed: a paused actor holds no worker, so CRASHED releases
// nothing here, but it makes the actor deletable and tells the consumer,
// through the crash directive, that this session's runtime state is gone.
// With a durable snapshot the actor is left as it is and the call fails at
// once with the cause; parking would wait on a node that will not return.
//
// The status is DataLoss either way: a dataplane parks a request on
// ResourceExhausted, FailedPrecondition and Unavailable, and this is not a
// condition another operation moves the actor out of.
func (w *ActorWorkflow) localSnapshotLost(ctx context.Context, actorRef resources.ActorRef, actor *ateapipb.Actor, opName string, nodes []string) error {
	snapshot := actor.GetStatus().GetLocalSnapshotInfo().GetSnapshotName()
	attrs := ateattr.ActorRefLogAttrs(actorRef)
	attrs = append(attrs, ateattr.FailureLogAttrs(ateattr.ReasonLocalSnapshotGone)...)
	attrs = append(attrs, slog.String("snapshot", snapshot), slog.Any("nodes", nodes))

	if actor.GetStatus().GetExternalSnapshot().GetSnapshotUri() != "" {
		slog.LogAttrs(ctx, slog.LevelError, "Paused actor's local snapshot is lost with its node; a durable snapshot remains", attrs...)
		return ateerrors.NewGRPCError(ctx, codes.DataLoss, ateerrors.ReasonLocalSnapshotGone, nil,
			fmt.Errorf("actor %s: its local snapshot %q is on node(s) %v, which no longer exist in the cluster; the paused state is lost", actorRef, snapshot, nodes))
	}

	slog.LogAttrs(ctx, slog.LevelError, "Setting Actor to crashed: its local snapshot is lost with its node and it has no durable snapshot", attrs...)
	if cerr := crashActor(ctx, w.store, actorRef, opName, ateattr.ReasonLocalSnapshotGone); cerr != nil {
		return cerr
	}
	return ateerrors.NewGRPCError(ctx, codes.DataLoss, ateerrors.ReasonLocalSnapshotGone, ateerrors.ActorCrashedMetadata(),
		fmt.Errorf("actor %s crashed: its local snapshot %q is on node(s) %v, which no longer exist in the cluster, and it has no durable snapshot", actorRef, snapshot, nodes))
}

// noFreeWorkerError turns the scheduler's refusal into the caller's status.
// Without a node restriction the pool is full: ResourceExhausted, which the
// router parks on until a worker frees up. Under one -- a PAUSED actor's
// local snapshot -- the scan says which of three states the actor is in:
//
//   - eligible workers on the snapshot's node(s), all full: as above, with
//     the node named, so the message does not point at cluster capacity;
//   - the node(s) still in the cluster but no eligible worker on them (a
//     pool rolling, a pool moved off the node): ResourceExhausted with the
//     node named, still worth parking on -- a worker may come back;
//   - the node(s) gone from the cluster: the snapshot is lost
//     (localSnapshotLost).
//
// A node is gone only when no ACTIVE worker of any pool reports it and the
// Node object is absent; either signal alone keeps the actor waiting.
func (w *ActorWorkflow) noFreeWorkerError(ctx context.Context, actorRef resources.ActorRef, actor *ateapipb.Actor, err error) error {
	var restricted *scheduling.NoCapacityError
	if !errors.As(err, &restricted) {
		return status.Errorf(codes.ResourceExhausted, "no free workers available")
	}
	nodes := restricted.RequiredNodes
	switch {
	case restricted.EligibleOnRequiredNodes > 0:
		return status.Errorf(codes.ResourceExhausted, "actor's local snapshot is on node(s) %v but all eligible workers on those nodes are busy", nodes)
	case restricted.WorkersOnRequiredNodes > 0:
		return status.Errorf(codes.ResourceExhausted, "actor's local snapshot is on node(s) %v but no eligible workers exist on those nodes", nodes)
	}
	gone, err := w.localSnapshotNodesGone(nodes)
	if err != nil {
		return err
	}
	if !gone {
		return status.Errorf(codes.ResourceExhausted, "actor's local snapshot is on node(s) %v but no workers exist on those nodes", nodes)
	}
	return w.localSnapshotLost(ctx, actorRef, actor, ateattr.OperationResume, nodes)
}
