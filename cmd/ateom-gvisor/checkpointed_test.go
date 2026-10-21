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
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/agent-substrate/substrate/internal/proto/ateompb"
)

// TestCheckpointedActorsRepeat proves a full checkpoint repeated after its
// answer was lost is answered the one taken while its files are in place, and
// taken anew otherwise.
func TestCheckpointedActorsRepeat(t *testing.T) {
	writeFiles := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, name := range names {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	taken := &ateompb.CheckpointWorkloadResponse{
		SnapshotFiles:     []string{"checkpoint.img", "durable-dir-data.tar"},
		DataSnapshotFiles: []string{"durable-dir-data.tar"},
	}

	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T, c *checkpointedActors, dir string)
		askDir func(dir string) string
		want   bool
	}{
		{
			name: "the files of the checkpoint taken answer its repeat",
			setup: func(t *testing.T, c *checkpointedActors, dir string) {
				writeFiles(t, dir, "checkpoint.img", "durable-dir-data.tar")
				c.record("uid", dir, taken)
			},
			want: true,
		},
		{
			name: "nothing recorded takes it anew",
			setup: func(t *testing.T, c *checkpointedActors, dir string) {
				writeFiles(t, dir, "checkpoint.img", "durable-dir-data.tar")
			},
		},
		{
			name: "a run since forgets it",
			setup: func(t *testing.T, c *checkpointedActors, dir string) {
				writeFiles(t, dir, "checkpoint.img", "durable-dir-data.tar")
				c.record("uid", dir, taken)
				c.forget("uid")
			},
		},
		{
			name: "a file gone takes it anew",
			setup: func(t *testing.T, c *checkpointedActors, dir string) {
				writeFiles(t, dir, "checkpoint.img")
				c.record("uid", dir, taken)
			},
		},
		{
			name: "another directory takes it anew",
			setup: func(t *testing.T, c *checkpointedActors, dir string) {
				writeFiles(t, dir, "checkpoint.img", "durable-dir-data.tar")
				c.record("uid", dir, taken)
			},
			askDir: func(dir string) string { return filepath.Join(dir, "other") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var c checkpointedActors
			dir := t.TempDir()
			tc.setup(t, &c, dir)
			ask := dir
			if tc.askDir != nil {
				ask = tc.askDir(dir)
			}
			got, ok := c.repeat("uid", ask)
			if ok != tc.want {
				t.Fatalf("repeat() ok = %v, want %v", ok, tc.want)
			}
			if ok && !proto.Equal(got, taken) {
				t.Errorf("repeat() = %v, want %v", got, taken)
			}
		})
	}
}
