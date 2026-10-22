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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/nodepath"
	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/google/go-cmp/cmp"
	"github.com/klauspost/compress/zstd"
)

func TestExtractTarArchive(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		compress  func(io.Writer) io.WriteCloser
		wantError bool
		errMsg    string
	}{
		{
			name: ".tar.gz format",
			url:  "https://example.com/gvisor.tar.gz",
			compress: func(w io.Writer) io.WriteCloser {
				return gzip.NewWriter(w)
			},
			wantError: false,
		},
		{
			name: ".tgz format",
			url:  "https://example.com/gvisor.tgz",
			compress: func(w io.Writer) io.WriteCloser {
				return gzip.NewWriter(w)
			},
			wantError: false,
		},
		{
			name:      ".tar.bz2 format",
			url:       "https://example.com/gvisor.tar.bz2",
			compress:  nil,
			wantError: false,
		},
		{
			name: ".tar.zst format",
			url:  "https://example.com/gvisor.tar.zst",
			compress: func(w io.Writer) io.WriteCloser {
				zw, _ := zstd.NewWriter(w) // no options, cannot fail
				return zw
			},
			wantError: false,
		},
		{
			name: ".tar.zstd format",
			url:  "https://example.com/gvisor.tar.zstd",
			compress: func(w io.Writer) io.WriteCloser {
				zw, _ := zstd.NewWriter(w) // no options, cannot fail
				return zw
			},
			wantError: false,
		},
		{
			name: ".tar format (uncompressed)",
			url:  "https://example.com/gvisor.tar",
			compress: func(w io.Writer) io.WriteCloser {
				return nopWriteCloser{w}
			},
			wantError: false,
		},
		{
			name:      "unsupported format",
			url:       "https://example.com/gvisor.zip",
			compress:  nil,
			wantError: true,
			errMsg:    "unsupported archive format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			archivePath := filepath.Join(tempDir, "archive")
			destDir := filepath.Join(tempDir, "dest")

			if tt.compress != nil {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatalf("failed to create archive: %v", err)
				}

				cw := tt.compress(f)
				tw := tar.NewWriter(cw)

				hdr := &tar.Header{
					Name: "test.txt",
					Mode: 0644,
					Size: int64(len("hello world")),
				}
				if err := tw.WriteHeader(hdr); err != nil {
					t.Fatalf("failed to write tar header: %v", err)
				}
				if _, err := tw.Write([]byte("hello world")); err != nil {
					t.Fatalf("failed to write tar content: %v", err)
				}

				tw.Close()
				cw.Close()
				f.Close()
			} else if !tt.wantError && strings.Contains(tt.url, ".tar.bz2") {
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatalf("failed to create archive: %v", err)
				}
				f.Write([]byte("invalid bzip2 data"))
				f.Close()

				err = extractTarArchive(context.Background(), archivePath, tt.url, destDir)
				if err == nil {
					t.Fatal("expected error for invalid bzip2 data, got nil")
				}
				if strings.Contains(err.Error(), "unsupported archive format") {
					t.Errorf("expected bzip2 read error, got: %v", err)
				}
				return
			}

			err := extractTarArchive(context.Background(), archivePath, tt.url, destDir)
			if (err != nil) != tt.wantError {
				t.Fatalf("extractTarArchive() error = %v, wantError = %v", err, tt.wantError)
			}

			if tt.wantError {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
				return
			}

			content, err := os.ReadFile(filepath.Join(destDir, "test.txt"))
			if err != nil {
				t.Fatalf("failed to read extracted file: %v", err)
			}
			if string(content) != "hello world" {
				t.Errorf("extracted content = %q, want %q", content, "hello world")
			}
		})
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error {
	return nil
}

func TestRecordFromRequest(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	valid := func() *ateletpb.SandboxAssets {
		return &ateletpb.SandboxAssets{
			SandboxClass: "gvisor",
			PauseImage:   testPauseImage,
			Assets: map[string]*ateletpb.ArchAssets{
				runtime.GOARCH: {Files: map[string]*ateletpb.AssetFile{
					gvisorAssetName: {Url: "gs://bucket/gvisor.tar.zst", Sha256: sha},
				}},
			},
		}
	}

	t.Run("valid request projects onto this architecture", func(t *testing.T) {
		got, err := recordFromRequest(valid())
		if err != nil {
			t.Fatalf("recordFromRequest: %v", err)
		}
		want := &sandboxAssetsRecord{
			SandboxClass: "gvisor",
			PauseImage:   testPauseImage,
			Assets:       map[string]assetEntry{gvisorAssetName: {URL: "gs://bucket/gvisor.tar.zst", SHA256: sha}},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("recordFromRequest mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("microvm needs no pause image", func(t *testing.T) {
		sa := valid()
		sa.SandboxClass = "microvm"
		sa.PauseImage = ""
		got, err := recordFromRequest(sa)
		if err != nil {
			t.Fatalf("recordFromRequest: %v", err)
		}
		if got.PauseImage != "" {
			t.Errorf("PauseImage = %q, want empty", got.PauseImage)
		}
	})

	for _, tc := range []struct {
		name   string
		mutate func(*ateletpb.SandboxAssets) *ateletpb.SandboxAssets
	}{
		{"missing", func(*ateletpb.SandboxAssets) *ateletpb.SandboxAssets { return nil }},
		{"no assets for this architecture", func(sa *ateletpb.SandboxAssets) *ateletpb.SandboxAssets {
			sa.Assets = map[string]*ateletpb.ArchAssets{"not-" + runtime.GOARCH: sa.Assets[runtime.GOARCH]}
			return sa
		}},
		{"gvisor without pause image", func(sa *ateletpb.SandboxAssets) *ateletpb.SandboxAssets {
			sa.PauseImage = ""
			return sa
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := recordFromRequest(tc.mutate(valid())); err == nil {
				t.Error("recordFromRequest succeeded, want an error")
			}
		})
	}
}

// TestRestoreRuntime proves a restore that loads a checkpoint image runs the
// gVisor its snapshot was captured with when that build is trusted, and every
// other restore the request's: runsc refuses a checkpoint image another build
// wrote, and the snapshot store never picks a binary the host runs.
func TestRestoreRuntime(t *testing.T) {
	newer := map[string]assetEntry{gvisorAssetName: {URL: "gs://gvisor/releases/nightly/2026-09-09/x86_64/gvisor.tar.zstd", SHA256: "newer"}}
	older := map[string]assetEntry{gvisorAssetName: retainedSandboxAssets[0]}
	forged := map[string]assetEntry{gvisorAssetName: {URL: retainedSandboxAssets[0].URL, SHA256: "forged"}}
	elsewhere := map[string]assetEntry{gvisorAssetName: {URL: "https://example.com/gvisor.tar.zstd", SHA256: retainedSandboxAssets[0].SHA256}}
	requested := &sandboxAssetsRecord{SandboxClass: "gvisor", PauseImage: "pause@sha256:requested", Assets: newer}

	for _, tc := range []struct {
		name     string
		snapshot *sandboxAssetsRecord
		scope    ateletpb.SnapshotScope
		want     map[string]assetEntry
	}{
		{
			name:     "full scope runs the snapshot's gVisor",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: older},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     older,
		},
		{
			name:     "unspecified scope is a full restore",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: older},
			want:     older,
		},
		{
			name:     "data scope boots fresh on the request's gVisor",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: older},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_DATA,
			want:     newer,
		},
		{
			name:     "a snapshot without recorded assets runs the request's",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor"},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     newer,
		},
		{
			name:     "the request's own build is trusted",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: newer},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     newer,
		},
		{
			name:     "a recorded build with another sha256 runs the request's",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: forged},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     newer,
		},
		{
			name:     "a recorded build at another URL runs the request's",
			snapshot: &sandboxAssetsRecord{SandboxClass: "gvisor", Assets: elsewhere},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     newer,
		},
		{
			name:     "a snapshot of another sandbox class runs the request's",
			snapshot: &sandboxAssetsRecord{SandboxClass: "microvm", Assets: older},
			scope:    ateletpb.SnapshotScope_SNAPSHOT_SCOPE_FULL,
			want:     newer,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := restoreRuntime(requested, tc.snapshot, tc.scope)
			if diff := cmp.Diff(tc.want, got.Assets); diff != "" {
				t.Errorf("restoreRuntime() assets (-want +got):\n%s", diff)
			}
			if got.PauseImage != requested.PauseImage || got.SandboxClass != requested.SandboxClass {
				t.Errorf("restoreRuntime() = %+v, want the request's pause image and class", got)
			}
			if requested.Assets[gvisorAssetName].SHA256 != "newer" {
				t.Errorf("restoreRuntime() changed the request's record")
			}
		})
	}
}

// gatedObjectStorage serves data, counting opens; a read blocks until release
// is closed, so callers that miss the cache together overlap in the fetch.
type gatedObjectStorage struct {
	data    []byte
	opens   *atomic.Int32
	release chan struct{}
}

func (g gatedObjectStorage) GetObject(_ context.Context, _, _ string) (io.ReadCloser, error) {
	g.opens.Add(1)
	<-g.release
	return io.NopCloser(bytes.NewReader(g.data)), nil
}

func (gatedObjectStorage) PutObject(_ context.Context, _, _ string, _ io.Reader) error { return nil }

// TestEnsureSandboxAssetsSharesGVisorFetch covers two callers that miss the
// cache for one gVisor release at once: they share one download and extract.
func TestEnsureSandboxAssetsSharesGVisorFetch(t *testing.T) {
	origDir := nodepath.StaticFilesDir
	nodepath.StaticFilesDir = t.TempDir()
	t.Cleanup(func() { nodepath.StaticFilesDir = origDir })

	var tarball bytes.Buffer
	zw, _ := zstd.NewWriter(&tarball) // no options, cannot fail
	tw := tar.NewWriter(zw)
	runsc := []byte("runsc binary")
	if err := tw.WriteHeader(&tar.Header{Name: "runsc", Mode: 0o755, Size: int64(len(runsc)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(runsc); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd Close: %v", err)
	}

	store := gatedObjectStorage{data: tarball.Bytes(), opens: new(atomic.Int32), release: make(chan struct{})}
	s := &AteomHerder{anonGCSClient: store}
	rec := &sandboxAssetsRecord{Assets: map[string]assetEntry{gvisorAssetName: {
		URL:    "gs://gvisor/releases/nightly/2026-09-09/x86_64/gvisor.tar.zstd",
		SHA256: fmt.Sprintf("%x", sha256.Sum256(tarball.Bytes())),
	}}}

	const callers = 2
	var wg sync.WaitGroup
	paths := make([]map[string]string, callers)
	errs := make([]error, callers)
	for i := range callers {
		wg.Go(func() { paths[i], errs[i] = s.ensureSandboxAssets(context.Background(), rec) })
	}
	// Both callers are past the cache check and the first is in the download
	// before either finishes; without the single flight each opens the object.
	time.Sleep(200 * time.Millisecond)
	close(store.release)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: ensureSandboxAssets: %v", i, errs[i])
		}
		if got, err := os.ReadFile(paths[i][gvisorAssetName]); err != nil || !bytes.Equal(got, runsc) {
			t.Errorf("caller %d: runsc at %q = %q, %v; want %q", i, paths[i][gvisorAssetName], got, err, runsc)
		}
	}
	if got := store.opens.Load(); got != 1 {
		t.Errorf("object opened %d times, want 1", got)
	}
	entries, err := os.ReadDir(nodepath.StaticFilesDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("static-files dir holds %d entries, want the one release dir: %v", len(entries), entries)
	}
}
