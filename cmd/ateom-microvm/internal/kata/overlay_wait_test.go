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

package kata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWaitForSocketReturnsPromptly pins the reason this helper exists: it sits on
// the restore path, so what it costs is decided by how often it looks, not by how
// long virtiofsd takes to bind. A socket appearing after 5ms should not cost a
// 50ms tick. The bound is on the poll interval, which the helper controls; a
// wall-clock bound on the whole wait would measure the runner's scheduling
// latency instead.
func TestWaitForSocketReturnsPromptly(t *testing.T) {
	if socketPollInterval > 5*time.Millisecond {
		t.Errorf("socketPollInterval = %s; a socket appearing after 5ms would wait out the tick", socketPollInterval)
	}
}

// TestWaitForSocketAppearsLater covers a socket that does not exist yet when the
// wait starts: it returns once the socket appears, without error.
func TestWaitForSocketAppearsLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vfsd.sock")
	created := make(chan error, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		f, err := os.Create(path)
		if err == nil {
			err = f.Close()
		}
		created <- err
	}()

	if err := waitForSocket(context.Background(), path, time.Minute); err != nil {
		t.Fatalf("waitForSocket: %v", err)
	}
	if err := <-created; err != nil {
		t.Fatalf("creating the socket: %v", err)
	}
}

func TestWaitForSocketAlreadyPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vfsd.sock")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := waitForSocket(context.Background(), path, time.Second); err != nil {
		t.Errorf("waitForSocket on an existing path: %v", err)
	}
}

func TestWaitForSocketTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.sock")
	err := waitForSocket(context.Background(), path, 20*time.Millisecond)
	if err == nil {
		t.Fatal("waitForSocket on a socket that never appears = nil, want error")
	}
	if !errors.Is(err, context.Canceled) && err.Error() == "" {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestWaitForSocketHonoursContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "never.sock")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	if err := waitForSocket(ctx, path, 5*time.Second); !errors.Is(err, context.Canceled) {
		t.Errorf("waitForSocket after cancel = %v, want context.Canceled", err)
	}
}
