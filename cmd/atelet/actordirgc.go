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

package main

// The actor-directory GC loop.
//
// Terminate is the only path that removes an actor's directory, and an actor
// deleted while it has no node (suspended, its snapshot in the store) is
// deleted without a terminate anywhere: the directory it left on the node it
// last ran on stays. Each pass lists the node's actor directories, asks the
// control plane which of those actors are gone, and removes the directories
// of those alone, the way Terminate does. An actor in any state is not gone,
// and an answer the control plane could not give removes nothing.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/agent-substrate/substrate/cmd/atelet/internal/ateletpath"
	"github.com/agent-substrate/substrate/internal/nodepath"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

var (
	actorDirGCPeriod = pflag.Duration("actor-dir-gc-period", 10*time.Minute, "How often atelet removes the directories of actors the control plane no longer has, such as one deleted while it was suspended. The first pass runs at start. 0 disables the pass.")
	actorDirGCDryRun = pflag.Bool("actor-dir-gc-dry-run", false, "Log the actor directories the pass would remove without removing them.")
)

// actorDirGCBatch is how many actor UIDs one ListGoneActors call asks about;
// the request's own limit.
const actorDirGCBatch = 256

// actorDirGCCallTimeout bounds one ListGoneActors call.
const actorDirGCCallTimeout = 30 * time.Second

// goneActorLister is the slice of WorkerService the pass asks.
type goneActorLister interface {
	ListGoneActors(ctx context.Context, in *ateapipb.ListGoneActorsRequest, opts ...grpc.CallOption) (*ateapipb.ListGoneActorsResponse, error)
}

type actorDirGC struct {
	workers goneActorLister
	period  time.Duration
	dryRun  bool
	// remove reclaims one actor's directory; removeActorDirs outside tests.
	remove func(ctx context.Context, actorUID string) error
}

func newActorDirGC(workers goneActorLister) *actorDirGC {
	return &actorDirGC{
		workers: workers,
		period:  *actorDirGCPeriod,
		dryRun:  *actorDirGCDryRun,
		remove:  removeActorDirs,
	}
}

func validateActorDirGCFlags() error {
	if *actorDirGCPeriod < 0 {
		return fmt.Errorf("--actor-dir-gc-period %v must be >= 0", *actorDirGCPeriod)
	}
	return nil
}

// Run passes at start and then every period until ctx is done.
func (gc *actorDirGC) Run(ctx context.Context) {
	ticker := time.NewTicker(gc.period)
	defer ticker.Stop()
	for {
		gc.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pass removes the directories of the node's actors the control plane names
// gone, and returns how many it removed (or, in a dry run, would remove).
func (gc *actorDirGC) pass(ctx context.Context) int {
	candidates, err := actorDirCandidates(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Actor dir GC: failed to list the actors dir; nothing removed", slog.String("actors_dir", nodepath.ActorsDir), slog.Any("err", err))
		return 0
	}
	removed, failed := 0, 0
	for start := 0; start < len(candidates); start += actorDirGCBatch {
		batch := candidates[start:min(start+actorDirGCBatch, len(candidates))]
		gone, err := gc.listGone(ctx, batch)
		if err != nil {
			// Not knowing is never gone: keep every directory of this pass.
			slog.WarnContext(ctx, "Actor dir GC: the control plane did not answer which actors are gone; nothing more removed this pass", slog.Int("actor_dirs", len(candidates)), slog.Int("removed", removed), slog.Any("err", err))
			return removed
		}
		for _, actorUID := range gone {
			dir := ateletpath.ActorPath(actorUID)
			if gc.dryRun {
				slog.InfoContext(ctx, "Actor dir GC (dry run): would remove the directory of an actor gone from the control plane", slog.String("actor_uid", actorUID), slog.String("dir", dir))
				removed++
				continue
			}
			slog.InfoContext(ctx, "Actor dir GC: removing the directory of an actor gone from the control plane", slog.String("actor_uid", actorUID), slog.String("dir", dir))
			if err := gc.remove(ctx, actorUID); err != nil {
				slog.WarnContext(ctx, "Actor dir GC: failed to remove the directory of a gone actor; what is left stays in place", slog.String("actor_uid", actorUID), slog.String("dir", dir), slog.Any("err", err))
				failed++
				continue
			}
			removed++
		}
	}
	if removed > 0 || failed > 0 {
		slog.InfoContext(ctx, "Actor dir GC pass done", slog.Int("actor_dirs", len(candidates)), slog.Int("removed", removed), slog.Int("failed", failed), slog.Bool("dry_run", gc.dryRun))
	}
	return removed
}

// listGone asks the control plane which of uids are gone, and returns only
// those of uids it names.
func (gc *actorDirGC) listGone(ctx context.Context, uids []string) ([]string, error) {
	callCtx, cancel := context.WithTimeout(ctx, actorDirGCCallTimeout)
	defer cancel()
	resp, err := gc.workers.ListGoneActors(callCtx, &ateapipb.ListGoneActorsRequest{ActorUids: uids})
	if err != nil {
		return nil, err
	}
	asked := make(map[string]bool, len(uids))
	for _, uid := range uids {
		asked[uid] = true
	}
	var gone []string
	for _, uid := range resp.GetGoneActorUids() {
		if asked[uid] {
			gone = append(gone, uid)
			delete(asked, uid)
		}
	}
	return gone, nil
}

// actorDirCandidates lists the actor UIDs the actors dir holds a directory
// for. An entry not named by a UID is not an actor's and is never touched,
// nor is an actor's directory a sweep of leftover volumes still owns: the
// sweep removes its record once the volumes let go, and a later pass the rest.
func actorDirCandidates(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(nodepath.ActorsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var uids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if parsed, err := uuid.Parse(name); err != nil || parsed.String() != name {
			continue
		}
		if _, err := os.Lstat(leftoverVolumesPath(name)); err == nil {
			slog.DebugContext(ctx, "Actor dir GC: skipping an actor whose leftover volumes are still swept", slog.String("actor_uid", name), slog.String("dir", filepath.Join(nodepath.ActorsDir, name)))
			continue
		}
		uids = append(uids, name)
	}
	return uids, nil
}
