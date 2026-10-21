//go:build linux

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

import (
	"slices"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/agent-substrate/substrate/internal/proto/ateompb"
)

// checkpointedActors remembers each actor's last completed full checkpoint
// until the actor runs, restores or terminates here again. A full checkpoint
// stops the sandbox, so its repeat finds nothing to checkpoint; a caller that
// lost the answer (its atelet restarted while ateom saved the actor) asks
// again, and is answered the checkpoint already taken while its files are
// still in the checkpoint directory. The zero value is ready to use.
type checkpointedActors struct {
	mu sync.Mutex
	// Keyed by actor UID.
	byUID map[string]checkpointRecord
}

// checkpointRecord is a completed full checkpoint: where it was written and
// the answer it was given.
type checkpointRecord struct {
	dir  string
	resp *ateompb.CheckpointWorkloadResponse
}

// record remembers a completed full checkpoint of the actor.
func (c *checkpointedActors) record(actorUID, dir string, resp *ateompb.CheckpointWorkloadResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byUID == nil {
		c.byUID = map[string]checkpointRecord{}
	}
	c.byUID[actorUID] = checkpointRecord{dir: dir, resp: proto.CloneOf(resp)}
}

// forget drops the actor's record: it runs, restores or terminates again.
func (c *checkpointedActors) forget(actorUID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byUID, actorUID)
}

// repeat returns the answer of the actor's completed full checkpoint into dir
// when every file it reported is still there and no other, so the caller
// ships exactly what that checkpoint wrote; false otherwise.
func (c *checkpointedActors) repeat(actorUID, dir string) (*ateompb.CheckpointWorkloadResponse, bool) {
	c.mu.Lock()
	rec, ok := c.byUID[actorUID]
	c.mu.Unlock()
	if !ok || rec.dir != dir {
		return nil, false
	}
	files, err := listSnapshotFiles(dir)
	if err != nil || !slices.Equal(files, rec.resp.GetSnapshotFiles()) {
		return nil, false
	}
	return proto.CloneOf(rec.resp), true
}
