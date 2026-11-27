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

//go:build linux && ateconvert

package main

import (
	"archive/tar"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// writeTree writes a small tree owned by the test's own user, with the
// kinds of entries a durable volume holds.
func writeTree(t *testing.T, root string) (xattrs bool) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dir/sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dir/a.txt"), []byte("hello"), 0o640))
	require.NoError(t, os.Chmod(filepath.Join(root, "dir/a.txt"), 0o2640))
	sparse, err := os.Create(filepath.Join(root, "sparse.bin"))
	require.NoError(t, err)
	_, err = sparse.WriteAt([]byte("tail"), 1<<20)
	require.NoError(t, err)
	require.NoError(t, sparse.Close())
	require.NoError(t, os.Link(filepath.Join(root, "dir/a.txt"), filepath.Join(root, "dir/sub/b.txt")))
	require.NoError(t, os.Symlink("../a.txt", filepath.Join(root, "dir/sub/link")))
	require.NoError(t, os.Symlink("/nowhere", filepath.Join(root, "dangling")))
	require.NoError(t, unix.Mkfifo(filepath.Join(root, "pipe"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gvisor.state"), []byte("x"), 0o600))
	require.NoError(t, os.Chmod(filepath.Join(root, "dir"), 0o1777))
	err = unix.Lsetxattr(filepath.Join(root, "dir/a.txt"), "user.note", []byte{0, 0xff}, 0)
	if errors.Is(err, unix.ENOTSUP) {
		xattrs = false
	} else {
		require.NoError(t, err)
		xattrs = true
	}
	old := time.Date(2001, 2, 3, 4, 5, 6, 7000, time.UTC)
	for _, p := range []string{"dir/a.txt", "sparse.bin", "pipe", "dir/sub", "dir", "."} {
		require.NoError(t, os.Chtimes(filepath.Join(root, p), old, old))
	}
	return xattrs
}

// tarTree archives root the way the 1.x worker did: a "./" root entry,
// PAX records, user.* xattrs and hard links.
func tarTree(t *testing.T, root, tarPath string) {
	t.Helper()
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	defer f.Close()
	tw := tar.NewWriter(f)
	seen := map[uint64]string{}
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, _ := filepath.Rel(root, p)
		info, err := os.Lstat(p)
		require.NoError(t, err)
		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			link, err = os.Readlink(p)
			require.NoError(t, err)
		}
		hdr, err := tar.FileInfoHeader(info, link)
		require.NoError(t, err)
		hdr.Format = tar.FormatPAX
		hdr.Name = rel
		if rel == "." {
			hdr.Name = "./"
		} else if d.IsDir() {
			hdr.Name = rel + "/"
		}
		st := info.Sys().(*syscall.Stat_t)
		if x, err := userXattrs(p); err == nil {
			for k, v := range x {
				raw, err := hex.DecodeString(v)
				require.NoError(t, err)
				if hdr.PAXRecords == nil {
					hdr.PAXRecords = map[string]string{}
				}
				hdr.PAXRecords["SCHILY.xattr."+k] = string(raw)
			}
		}
		if info.Mode().IsRegular() && st.Nlink > 1 {
			if first, ok := seen[st.Ino]; ok {
				hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeLink, first, 0
				require.NoError(t, tw.WriteHeader(hdr))
				return nil
			}
			seen[st.Ino] = rel
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			_, err = tw.Write(data)
			require.NoError(t, err)
		}
		return nil
	}))
	require.NoError(t, tw.Close())
}

func TestFSConvertExtractMatchesTarManifest(t *testing.T) {
	src := t.TempDir()
	xattrs := writeTree(t, src)
	tarPath := filepath.Join(t.TempDir(), "vol.tar")
	tarTree(t, src, tarPath)

	want, err := tarManifest(tarPath)
	require.NoError(t, err)
	srcManifest, err := dirManifest(src)
	require.NoError(t, err)
	require.Equal(t, fsconvertVolumeResult{Volume: "data", Entries: len(want), Hash: compareManifests("data", want, nil).Hash}, compareManifests("data", want, srcManifest),
		"the tar holds the tree")

	dst := t.TempDir()
	require.NoError(t, fsconvertExtract(tarPath, dst))
	got, err := dirManifest(dst)
	require.NoError(t, err)
	res := compareManifests("data", want, got)
	require.Zero(t, res.Mismatches, res.Fields)
	require.Len(t, want, 9, "the .gvisor file is skipped")

	var st unix.Stat_t
	require.NoError(t, unix.Stat(filepath.Join(dst, "sparse.bin"), &st))
	require.Less(t, st.Blocks*512, int64(1<<20), "runs of zeros stay holes")
	if xattrs {
		x, err := userXattrs(filepath.Join(dst, "dir/a.txt"))
		require.NoError(t, err)
		require.Equal(t, map[string]string{"user.note": "00ff"}, x)
	}
}

func TestCompareManifestsNamesFieldsOnly(t *testing.T) {
	want := []treeEntry{
		{Path: ".", Type: "dir", Mode: 0o755},
		{Path: "secret-name.txt", Type: "file", Mode: 0o600, Size: 3, SHA256: "aa", MTime: 1},
		{Path: "gone", Type: "file"},
	}
	got := []treeEntry{
		{Path: ".", Type: "dir", Mode: 0o755},
		{Path: "secret-name.txt", Type: "file", Mode: 0o644, Size: 3, SHA256: "bb", MTime: 1},
		{Path: "new", Type: "file"},
	}
	res := compareManifests("data", want, got)
	require.Equal(t, 3, res.Mismatches)
	require.Equal(t, []string{"content", "extra", "missing", "mode"}, res.Fields)
	out, err := json.Marshal(res)
	require.NoError(t, err)
	require.NotContains(t, string(out), "secret-name")
}

func TestVolumeEntryName(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    string
		skip    bool
		wantErr bool
	}{
		{in: "./", want: "."},
		{in: "/", want: "."},
		{in: "a/b", want: "a/b"},
		{in: "./a/../b", want: "b"},
		{in: "a/.gvisor.filestore", want: "a/.gvisor.filestore", skip: true},
		{in: "../etc/passwd", wantErr: true},
		{in: "/etc/passwd", wantErr: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, skip, err := volumeEntryName(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.skip, skip)
		})
	}
}

// TestFSConvertRoundTrip converts a tar into an fs/ bundle with runsc and
// restores it. It needs root, a gVisor release's runsc in ATE_CONVERT_RUNSC
// and its internal_flags.json in ATE_CONVERT_RUNSC_FLAGS, and a test binary
// built with CGO_ENABLED=0, which runs as the helper inside the sandbox.
func TestFSConvertRoundTrip(t *testing.T) {
	runscPath, flags := os.Getenv("ATE_CONVERT_RUNSC"), os.Getenv("ATE_CONVERT_RUNSC_FLAGS")
	if runscPath == "" || flags == "" || os.Geteuid() != 0 {
		t.Skip("needs root, ATE_CONVERT_RUNSC and ATE_CONVERT_RUNSC_FLAGS")
	}
	src := t.TempDir()
	writeTree(t, src)
	require.NoError(t, os.Lchown(filepath.Join(src, "dir/a.txt"), 1000, 3000))
	require.NoError(t, os.Chmod(filepath.Join(src, "dir/a.txt"), 0o4750))
	tarPath := filepath.Join(t.TempDir(), "data.tar")
	tarTree(t, src, tarPath)

	work := t.TempDir()
	spec := &fsconvertSpec{
		Runsc: runscPath, Flags: flags, Root: filepath.Join(work, "convert"), Out: filepath.Join(work, "out"),
		Containers: []fsconvertContainer{{Name: "kagent", Mounts: []fsconvertMount{{Volume: "data", Path: "/data"}}}},
		Tars:       map[string]string{"data": tarPath},
	}
	conv, err := fsconvertConvert(t.Context(), spec)
	require.NoError(t, err)
	require.Contains(t, conv.Files, "fs/fscheckpoint.pb")

	spec.Root = filepath.Join(work, "restore")
	res, err := fsconvertRestore(t.Context(), spec)
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Zero(t, res[0].Mismatches, res[0].Fields)

	// A container name the bundle does not have restores nothing.
	spec.Root = filepath.Join(work, "renamed")
	spec.Containers[0].Name = "other"
	_, err = fsconvertRestore(t.Context(), spec)
	require.ErrorContains(t, err, "restoring the volumes of other")
}
