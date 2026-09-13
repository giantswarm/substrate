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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agent-substrate/substrate/internal/ateompath"
	"github.com/agent-substrate/substrate/internal/ocispec"
	"github.com/agent-substrate/substrate/internal/proto/ateompb"
)

func TestListArgs(t *testing.T) {
	r := &runsc{
		path:     "/usr/bin/runsc",
		actorUID: "test-actor-123",
	}

	got := r.listArgs()
	want := []string{
		"-log-format", "json",
		"--alsologtostderr",
		"-root", ateompath.RunSCStateDir("test-actor-123"),
		"list",
		"-quiet",
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("listArgs() = %v, want %v", got, want)
	}
}

// fakeRunscBehaviour configures the runsc stand-in fakeRunsc writes.
type fakeRunscBehaviour struct {
	// known are the containers runsc knows at the start.
	known []string
	// listFails makes `list` fail with exit status 128.
	listFails bool
	// deleteFailsAfterRemoving are the containers whose `delete` removes them
	// and then fails with exit status 128, the way `runsc delete -force`
	// does when the filestore file it cleans up last is already gone.
	deleteFailsAfterRemoving []string
	// deleteFailsKeeping are the containers whose `delete` fails with exit
	// status 128 and leaves them in place.
	deleteFailsKeeping []string
}

// fakeRunsc writes a runsc stand-in: `list -quiet` prints the ids of the
// containers it currently knows, `state` and `delete` of one of them succeed
// (`delete` forgets it), and of any other fail with exit status 128 the way
// runsc's fatal errors do. Every invocation is appended to the returned log
// file as one line: the subcommand and its arguments.
func fakeRunsc(t *testing.T, b fakeRunscBehaviour) (path, log string) {
	t.Helper()
	dir := t.TempDir()
	path = filepath.Join(dir, "runsc")
	log = filepath.Join(dir, "invocations")
	known := filepath.Join(dir, "known")
	if err := os.WriteFile(known, []byte(strings.Join(b.known, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	listExit := "0"
	if b.listFails {
		listExit = "128"
	}
	script := fmt.Sprintf(`#!/bin/sh
known=%q
while [ $# -gt 0 ]; do
  case "$1" in
    -root|-log-format) shift 2 ;;
    --alsologtostderr) shift ;;
    *) break ;;
  esac
done
echo "$*" >> %q
sub=$1; shift
for arg in "$@"; do id=$arg; done
case "$sub" in
  list)
    grep -v '^$' "$known"
    exit %s ;;
  state)
    grep -qx "$id" "$known" && exit 0
    echo "loading container: file does not exist" >&2; exit 128 ;;
  delete)
    grep -qx "$id" "$known" || { echo "loading container: file does not exist" >&2; exit 128; }
    case " %s " in *" $id "*) exit 128 ;; esac
    grep -vx "$id" "$known" > "$known.new"; mv "$known.new" "$known"
    case " %s " in *" $id "*) echo "destroying container: failed to delete filestore file" >&2; exit 128 ;; esac
    exit 0 ;;
  *) echo "unexpected subcommand $sub" >&2; exit 128 ;;
esac
`, known, log, listExit, strings.Join(b.deleteFailsKeeping, " "), strings.Join(b.deleteFailsAfterRemoving, " "))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func invocations(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestCleanupContainers(t *testing.T) {
	const pause = ocispec.PauseContainer
	containers := []*ateompb.Container{{Name: "kagent"}}
	for _, tc := range []struct {
		name      string
		behaviour fakeRunscBehaviour
		want      []string
		wantErr   string
	}{
		{
			// A terminate retried after the containers are gone: nothing to
			// check or delete, and no error to fail the terminate on.
			name: "every container already gone",
			want: []string{"list -quiet"},
		},
		{
			// The application container went with a failed boot, the sandbox
			// is still there.
			name:      "application container gone, pause container present",
			behaviour: fakeRunscBehaviour{known: []string{pause}},
			want:      []string{"list -quiet", "state " + pause, "delete -force " + pause},
		},
		{
			name:      "every container present",
			behaviour: fakeRunscBehaviour{known: []string{pause, "kagent"}},
			want: []string{
				"list -quiet",
				"state kagent", "state " + pause,
				"delete -force kagent", "delete -force " + pause,
			},
		},
		{
			// The first terminate of a crashed boot: `runsc delete -force`
			// destroys the sandbox's state and then fails on the filestore
			// file of the detached rootfs overlay. The container is gone, so
			// the terminate succeeds in this pass.
			name:      "delete fails after removing the container",
			behaviour: fakeRunscBehaviour{known: []string{pause}, deleteFailsAfterRemoving: []string{pause}},
			want:      []string{"list -quiet", "state " + pause, "delete -force " + pause, "list -quiet"},
		},
		{
			name:      "delete fails with the container still there",
			behaviour: fakeRunscBehaviour{known: []string{pause}, deleteFailsKeeping: []string{pause}},
			want:      []string{"list -quiet", "state " + pause, "delete -force " + pause, "list -quiet"},
			wantErr:   "while deleting \"" + pause + "\" container: while running `runsc delete`",
		},
		{
			name:      "listing fails",
			behaviour: fakeRunscBehaviour{known: []string{pause, "kagent"}, listFails: true},
			want:      []string{"list -quiet"},
			wantErr:   "while listing containers: while running `runsc list`",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, log := fakeRunsc(t, tc.behaviour)
			r := &runsc{path: path, actorUID: "test-actor-123"}

			err := r.cleanupContainers(context.Background(), containers)

			if tc.wantErr == "" && err != nil {
				t.Fatalf("cleanupContainers() = %v, want nil", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("cleanupContainers() = %v, want an error containing %q", err, tc.wantErr)
			}
			if got := invocations(t, log); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("runsc invocations = %q, want %q", got, tc.want)
			}
		})
	}
}
