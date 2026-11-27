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

package snapshot

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/pkg/objectstorage"
)

// The test binary stands in for the worker's fsconvert when
// FAKE_FSCONVERT is set: convert writes a bundle holding the hash of each
// tar, and restore reports a mismatch for a volume named in
// FAKE_FSCONVERT_MISMATCH.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_FSCONVERT") == "1" && len(os.Args) == 4 && os.Args[1] == "fsconvert" {
		os.Exit(fakeFSConvert(os.Args[2], os.Args[3]))
	}
	os.Exit(m.Run())
}

func fakeFSConvert(mode, specPath string) int {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return 1
	}
	var s spec
	if err := json.Unmarshal(raw, &s); err != nil {
		return 1
	}
	digest := func() string {
		h := sha256.New()
		for _, vol := range []string{"cache", "data"} {
			if p, ok := s.Tars[vol]; ok {
				b, _ := os.ReadFile(p)
				fmt.Fprintf(h, "%s=%x\n", vol, sha256.Sum256(b))
			}
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	switch mode {
	case "convert":
		_ = os.MkdirAll(filepath.Join(s.Out, "fs"), 0o700)
		_ = os.WriteFile(filepath.Join(s.Out, "fs", "fscheckpoint.pb"), []byte(digest()), 0o600)
		_ = os.WriteFile(filepath.Join(s.Out, "fs", "pages.img"), make([]byte, 1<<16), 0o600)
		_ = json.NewEncoder(os.Stdout).Encode(convertResult{Files: []string{"fs/fscheckpoint.pb", "fs/pages.img"}})
	case "restore":
		pb, _ := os.ReadFile(filepath.Join(s.Out, "fs", "fscheckpoint.pb"))
		var out []volumeResult
		for vol := range s.Tars {
			r := volumeResult{Volume: vol, Entries: 1, Hash: "h"}
			if string(pb) != digest() || os.Getenv("FAKE_FSCONVERT_MISMATCH") == vol {
				r.Mismatches, r.Fields = 1, []string{"content"}
			}
			out = append(out, r)
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
	}
	return 0
}

// memStore is an ObjectStorage in memory that records the order of writes.
type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    []string
}

func (s *memStore) GetObject(_ context.Context, bucket, object string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[bucket+"/"+object]
	if !ok {
		return nil, fmt.Errorf("%w: %s", objectstorage.ErrObjectNotFound, object)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *memStore) PutObject(_ context.Context, bucket, object string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[bucket+"/"+object] = b
	s.puts = append(s.puts, object)
	return nil
}

const snapURI = "s3://ate-snapshots/kagent/atespaces/team/actors/11111111-1111-4111-8111-111111111111/snapshots/44444444-4444-4444-8444-444444444444/"

func tarBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755}))
	for name, body := range entries {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// seed1x writes a 1.x snapshot the way the 1.x atelet did: files through
// the sparse zstd upload, the manifest last and uncompressed.
func seed1x(t *testing.T, store *memStore, files map[string][]byte, m *manifest) {
	t.Helper()
	up, err := objectstorage.NewUploader(store, 1<<30)
	require.NoError(t, err)
	dir := t.TempDir()
	for name, b := range files {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, b, 0o600))
		f, err := os.Open(p)
		require.NoError(t, err)
		require.NoError(t, up.SendFile(t.Context(), snapURI+name+".zstd", f))
		f.Close()
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, up.Send(t.Context(), snapURI+manifestFile, bytes.NewReader(raw)))
	store.puts = nil
}

func newConverter(t *testing.T, store *memStore) (*Converter, *bytes.Buffer) {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("FAKE_FSCONVERT", "1")
	up, err := objectstorage.NewUploader(store, 1<<30)
	require.NoError(t, err)
	var log bytes.Buffer
	return &Converter{Store: store, Uploader: up, WorkerBinary: self, Runsc: "runsc", RunscFlags: "flags.json", WorkDir: t.TempDir(), Asset: Asset{URL: "gs://gvisor/x/gvisor.tar.zstd", SHA256: strings.Repeat("a", 64)}, Log: &log}, &log
}

var kagentJob = Job{URI: snapURI, Containers: []Container{{Name: "kagent", Mounts: []Mount{{Volume: "data", Path: "/data"}}}}}

func fork1x() *manifest {
	return &manifest{
		SandboxClass: "gvisor", Assets: map[string]Asset{"gvisor": {URL: "gs://old", SHA256: "b"}}, PauseImage: "pause@sha256:x",
		Atespace: "team", ActorName: "session", ActorUID: "11111111-1111-4111-8111-111111111111", ActorTemplateAtespace: "team", ActorTemplateName: "agent",
		SnapshotFiles: []string{"checkpoint.img", "durable-dir-data.tar"}, DataSnapshotFiles: []string{"durable-dir-data.tar"}, Scope: "full",
	}
}

func TestConvert(t *testing.T) {
	store := &memStore{objects: map[string][]byte{}}
	data := tarBytes(t, map[string]string{"a.txt": "hello"})
	seed1x(t, store, map[string][]byte{"durable-dir-data.tar": data, "checkpoint.img": []byte("memory")}, fork1x())
	forkRaw := store.objects["ate-snapshots/"+strings.TrimPrefix(snapURI, "s3://ate-snapshots/")+manifestFile]
	c, _ := newConverter(t, store)

	require.NoError(t, c.Convert(t.Context(), kagentJob))
	prefix := strings.TrimPrefix(snapURI, "s3://ate-snapshots/")
	require.Equal(t, []string{prefix + "fs/fscheckpoint.pb.zstd", prefix + "fs/pages.img.zstd", prefix + forkManifestFile, prefix + manifestFile}, store.puts,
		"the 2.x manifest is written last")
	require.Equal(t, forkRaw, store.objects["ate-snapshots/"+prefix+forkManifestFile])
	got, err := parseManifest(store.objects["ate-snapshots/"+prefix+manifestFile])
	require.NoError(t, err)
	require.Equal(t, &manifest{
		SandboxClass: "gvisor", Assets: map[string]Asset{"gvisor": c.Asset},
		Atespace: "team", ActorName: "session", ActorUID: "11111111-1111-4111-8111-111111111111", ActorTemplateAtespace: "team", ActorTemplateName: "agent",
		SnapshotFiles: []string{"fs/fscheckpoint.pb", "fs/pages.img"}, DataSnapshotFiles: []string{"fs/fscheckpoint.pb", "fs/pages.img"}, Fidelity: "volumes",
	}, got)
	require.Contains(t, store.objects, "ate-snapshots/"+prefix+"durable-dir-data.tar.zstd", "the 1.x objects stay")

	// Converting again changes nothing, and Verify restores what it wrote.
	store.puts = nil
	require.NoError(t, c.Convert(t.Context(), kagentJob))
	require.Empty(t, store.puts)
	require.NoError(t, c.Verify(t.Context(), kagentJob))
	state, _, err := c.Inspect(t.Context(), kagentJob)
	require.NoError(t, err)
	require.Equal(t, StateConverted, state)
}

func TestConvertRefuses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    map[string][]byte
		manifest func(*manifest)
		job      Job
		mismatch string
		wantErr  string
	}{{
		name:    "a volume no container mounts",
		job:     Job{URI: snapURI, Containers: []Container{{Name: "kagent", Mounts: []Mount{{Volume: "other", Path: "/data"}}}}},
		wantErr: `volume "data" has data but no container of the template mounts it`,
	}, {
		name:     "a restore that differs",
		mismatch: "data",
		wantErr:  "volume data: 1 entries differ in [content]",
	}, {
		name:     "a microVM snapshot",
		manifest: func(m *manifest) { m.SandboxClass = "microvm" },
		wantErr:  `sandbox class "microvm"`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &memStore{objects: map[string][]byte{}}
			m := fork1x()
			if tc.manifest != nil {
				tc.manifest(m)
			}
			seed1x(t, store, map[string][]byte{"durable-dir-data.tar": tarBytes(t, map[string]string{"a": "b"})}, m)
			c, _ := newConverter(t, store)
			t.Setenv("FAKE_FSCONVERT_MISMATCH", tc.mismatch)
			job := tc.job
			if job.URI == "" {
				job = kagentJob
			}
			require.ErrorContains(t, c.Convert(t.Context(), job), tc.wantErr)
			require.Empty(t, store.puts, "a refused conversion writes nothing")
		})
	}
}

func TestConvertLegacyTar(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range []*tar.Header{
		{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o700, Uid: 1000},
		{Name: "data/a.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1},
		{Name: "data/b.txt", Typeflag: tar.TypeLink, Linkname: "data/a.txt"},
		{Name: "cache/", Typeflag: tar.TypeDir, Mode: 0o755},
	} {
		require.NoError(t, tw.WriteHeader(h))
		if h.Size > 0 {
			_, err := tw.Write([]byte("x"))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())

	store := &memStore{objects: map[string][]byte{}}
	m := fork1x()
	m.DataSnapshotFiles, m.SnapshotFiles = []string{legacyDurableTar}, []string{legacyDurableTar}
	seed1x(t, store, map[string][]byte{legacyDurableTar: buf.Bytes()}, m)
	c, _ := newConverter(t, store)
	job := Job{URI: snapURI, Containers: []Container{{Name: "kagent", Mounts: []Mount{{Volume: "data", Path: "/data"}, {Volume: "cache", Path: "/cache"}}}}}
	require.NoError(t, c.Convert(t.Context(), job))
	require.NoError(t, c.Verify(t.Context(), job))
}

func TestSplitLegacyTar(t *testing.T) {
	write := func(t *testing.T, headers ...*tar.Header) string {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, h := range headers {
			require.NoError(t, tw.WriteHeader(h))
			if h.Size > 0 {
				_, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
				require.NoError(t, err)
			}
		}
		require.NoError(t, tw.Close())
		p := filepath.Join(t.TempDir(), legacyDurableTar)
		require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
		return p
	}
	t.Run("one tar per volume", func(t *testing.T) {
		p := write(t,
			&tar.Header{Name: "./", Typeflag: tar.TypeDir},
			&tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o2775, Uid: 7, PAXRecords: map[string]string{"SCHILY.xattr.user.k": "v", "path": "ignored"}},
			&tar.Header{Name: "data/f", Typeflag: tar.TypeReg, Size: 3},
			&tar.Header{Name: "data/g", Typeflag: tar.TypeLink, Linkname: "data/f"},
		)
		dir := t.TempDir()
		dsts := map[string]string{"data": filepath.Join(dir, "data.tar"), "cache": filepath.Join(dir, "cache.tar")}
		require.NoError(t, splitLegacyTar(p, dsts))
		_, err := os.Stat(dsts["cache"])
		require.True(t, os.IsNotExist(err), "a volume with no entries gets no tar")
		f, err := os.Open(dsts["data"])
		require.NoError(t, err)
		defer f.Close()
		tr := tar.NewReader(f)
		var names []string
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			names = append(names, h.Name+"|"+h.Linkname)
			if h.Name == "./" {
				require.EqualValues(t, 0o2775, h.Mode)
				require.Equal(t, 7, h.Uid)
				require.Equal(t, "v", h.PAXRecords["SCHILY.xattr.user.k"])
			}
		}
		require.Equal(t, []string{"./|", "f|", "g|f"}, names)
	})
	for _, tc := range []struct {
		name    string
		headers []*tar.Header
		wantErr string
	}{
		{"entry outside every volume", []*tar.Header{{Name: "other/x", Typeflag: tar.TypeReg}}, "outside every durable volume"},
		{"file at the top", []*tar.Header{{Name: "data", Typeflag: tar.TypeReg}}, "not a volume directory"},
		{"hard link across volumes", []*tar.Header{{Name: "cache/f", Typeflag: tar.TypeReg}, {Name: "data/g", Typeflag: tar.TypeLink, Linkname: "cache/f"}}, "across volumes"},
		{"entry escaping", []*tar.Header{{Name: "../x", Typeflag: tar.TypeReg}}, "not a local path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := write(t, tc.headers...)
			dir := t.TempDir()
			err := splitLegacyTar(p, map[string]string{"data": filepath.Join(dir, "d"), "cache": filepath.Join(dir, "c")})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestDurableTars(t *testing.T) {
	for _, tc := range []struct {
		name      string
		m         manifest
		perVolume map[string]string
		legacy    bool
		wantErr   string
	}{
		{"per volume", manifest{DataSnapshotFiles: []string{"durable-dir-data.tar", "durable-dir-cache.tar"}}, map[string]string{"data": "durable-dir-data.tar", "cache": "durable-dir-cache.tar"}, false, ""},
		{"older manifest without data files", manifest{SnapshotFiles: []string{"checkpoint.img", "durable-dir-data.tar"}}, map[string]string{"data": "durable-dir-data.tar"}, false, ""},
		{"legacy", manifest{DataSnapshotFiles: []string{"durable-dir.tar"}}, map[string]string{}, true, ""},
		{"no durable data", manifest{SnapshotFiles: []string{"checkpoint.img"}}, map[string]string{}, false, ""},
		{"both layouts", manifest{DataSnapshotFiles: []string{"durable-dir.tar", "durable-dir-data.tar"}}, nil, false, "both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pv, legacy, err := tc.m.durableTars()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.perVolume, pv)
			require.Equal(t, tc.legacy, legacy)
		})
	}
}
