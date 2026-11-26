// Copyright 2026 The Agent Substrate Authors
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

package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestRangedReaderCloseStopsRequests(t *testing.T) {
	head, body := io.Pipe()
	defer head.Close()
	defer body.Close()
	fetchContext := make(chan context.Context, 1)
	r := newRangedReader(t.Context(), downloadChunkSize+1, head, func(ctx context.Context, _ int, _, _ int64, _ []byte) error {
		fetchContext <- ctx
		<-ctx.Done()
		return ctx.Err()
	})
	defer r.Close()

	// Leave the first range blocked waiting for the rest of its body.
	if _, err := io.WriteString(body, "partial"); err != nil {
		t.Fatal(err)
	}
	ctx := <-fetchContext
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("Close did not cancel the later range request")
	}

	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(body, "more")
		written <- err
	}()
	select {
	case err := <-written:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("first range write = %v, want closed pipe", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close left the first range reader open")
	}
}

// A read cut short by cancellation must fail, not end in a clean EOF that a
// decoder could take for the end of a frame.
func TestRangedReaderCancelIsNotEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	fetched := make(chan struct{}, downloadConcurrency)
	head := io.NopCloser(bytes.NewReader(make([]byte, downloadChunkSize)))
	r := newRangedReader(ctx, (downloadConcurrency+1)*downloadChunkSize, head, func(context.Context, int, int64, int64, []byte) error {
		fetched <- struct{}{}
		return nil
	})
	defer r.Close()

	// Every buffer holds a fetched range, so the last range waits for one.
	for range downloadConcurrency - 1 {
		<-fetched
	}
	cancel()
	n, err := io.Copy(io.Discard, r)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("read %d of %d bytes, err %v, want context.Canceled", n, r.ObjectSize(), err)
	}
}

// TestRangedReadersShareDownloadSlots checks that no more than
// maxRangedDownloads readers hold their buffers at once, that a waiting reader
// starts once one closes, and that a reader closed while waiting never starts.
func TestRangedReadersShareDownloadSlots(t *testing.T) {
	started := make(chan int, maxRangedDownloads+2)
	open := func(id int) *rangedReader {
		// No head: the first range is fetched, so a fetch marks a start.
		return newRangedReader(t.Context(), 2*downloadChunkSize, nil, func(ctx context.Context, i int, _, _ int64, _ []byte) error {
			if i == 0 {
				started <- id
			}
			<-ctx.Done()
			return ctx.Err()
		})
	}
	readers := make([]*rangedReader, 0, maxRangedDownloads)
	for id := range maxRangedDownloads {
		readers = append(readers, open(id))
	}
	for range maxRangedDownloads {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("a reader within the bound did not start")
		}
	}
	waiting := open(100)
	closedWhileWaiting := open(200)
	select {
	case id := <-started:
		t.Fatalf("reader %d started beyond the bound", id)
	case <-time.After(100 * time.Millisecond):
	}
	if err := closedWhileWaiting.Close(); err != nil {
		t.Fatal(err)
	}
	if err := readers[0].Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-started:
		if id != 100 {
			t.Fatalf("reader %d started, want the waiting one", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting reader did not start once a slot was free")
	}
	for _, r := range append(readers[1:], waiting) {
		_ = r.Close()
	}
}
