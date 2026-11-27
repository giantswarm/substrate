//go:build linux

// Copyright 2026 The Agent Substrate Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package volumebind

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"go.ate.dev/substrate/internal/ocispec"
	"go.ate.dev/substrate/internal/proto/ateworkerpb"
	"go.ate.dev/substrate/internal/roottest"
	"golang.org/x/sys/unix"
)

func TestOpenVolumeDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"sessions/a", "mirrors"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o644))
	// Links another actor could plant on the shared volume.
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "sessions", "b")))
	require.NoError(t, os.Symlink("../mirrors", filepath.Join(root, "sessions", "c")))

	for _, tc := range []struct {
		subPath string
		wantErr string
	}{
		{subPath: ""},
		{subPath: "sessions/a"},
		{subPath: "mirrors"},
		{subPath: "sessions/missing", wantErr: "does not exist"},
		{subPath: "sessions/b", wantErr: "crosses a symbolic link"},
		{subPath: "sessions/c", wantErr: "crosses a symbolic link"},
		{subPath: "file", wantErr: "is not a directory"},
		{subPath: "../" + filepath.Base(outside), wantErr: "opening sub_path"},
	} {
		t.Run(tc.subPath, func(t *testing.T) {
			fd, err := openVolumeDir(root, tc.subPath)
			if tc.wantErr == "" {
				require.NoError(t, err)
				require.NoError(t, unix.Close(fd))
				return
			}
			if err == nil {
				_ = unix.Close(fd)
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestBoundMounts(t *testing.T) {
	containers := []*ateworkerpb.Container{
		{Name: "main", ExternalVolumeMounts: []*ateworkerpb.VolumeMount{
			{Name: "ws", MountPath: "/root"},
			{Name: "ws", MountPath: "/workspace", SubPath: "sessions/a"},
			{Name: "ws", MountPath: "/mirrors", SubPath: "mirrors", ReadOnly: true},
		}},
		{Name: "sidecar", ExternalVolumeMounts: []*ateworkerpb.VolumeMount{
			{Name: "ws", MountPath: "/also", SubPath: "sessions/a"},
		}},
	}
	var got []string
	for _, bm := range boundMounts(containers) {
		got = append(got, bm.container.GetName()+":"+bm.mount.GetMountPath())
	}
	// One per directory, none for the root mount.
	require.Equal(t, []string{"main:/workspace", "main:/mirrors"}, got)
}

func TestCreateVolumeDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"sessions/a", "unwritable"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "sessions", "link")))
	require.NoError(t, os.Symlink("sessions", filepath.Join(root, "linked")))
	require.NoError(t, os.Chmod(filepath.Join(root, "unwritable"), 0o555))
	unix.Umask(0o022)
	// The test's own identity: the one user an unprivileged test may give a
	// directory to.
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())

	for _, tc := range []struct {
		subPath   string
		wantErr   string // from createVolumeDir
		wantBind  string // from the openVolumeDir of the bind that follows
		wantMode  os.FileMode
		unlessUID int // skipped for this uid: it is not refused
	}{
		{subPath: "sessions/new", wantMode: 0o770},
		{subPath: "top", wantMode: 0o770},
		{subPath: "sessions/a", wantMode: 0o755},
		{subPath: ""},
		{subPath: "sessions/missing/new", wantErr: `its parent: sub_path "sessions/missing" does not exist`},
		{subPath: "linked/new", wantErr: `its parent: sub_path "linked" crosses a symbolic link`},
		{subPath: "file/new", wantErr: `its parent: sub_path "file" is not a directory`},
		{subPath: "sessions/link", wantBind: "crosses a symbolic link"},
		{subPath: "file", wantBind: "is not a directory"},
		{subPath: "unwritable/new", wantErr: "permission denied", unlessUID: 0},
		{subPath: "../" + filepath.Base(outside) + "/new", wantErr: "its parent: opening sub_path"},
	} {
		t.Run(tc.subPath, func(t *testing.T) {
			if tc.wantErr != "" && tc.unlessUID == os.Geteuid() {
				t.Skipf("uid %d is not refused", tc.unlessUID)
			}
			err := createVolumeDir(root, tc.subPath, uid, gid)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			fd, err := openVolumeDir(root, tc.subPath)
			if tc.wantBind != "" {
				if err == nil {
					_ = unix.Close(fd)
				}
				require.ErrorContains(t, err, tc.wantBind)
				return
			}
			require.NoError(t, err)
			require.NoError(t, unix.Close(fd))
			if tc.wantMode == 0 {
				return
			}
			info, err := os.Lstat(filepath.Join(root, tc.subPath))
			require.NoError(t, err)
			require.True(t, info.IsDir())
			require.Equal(t, tc.wantMode, info.Mode().Perm())
			st := info.Sys().(*syscall.Stat_t)
			require.Equal(t, [2]uint32{uid, gid}, [2]uint32{st.Uid, st.Gid})
		})
	}
	_, err := os.Lstat(filepath.Join(root, "sessions", "missing"))
	require.ErrorIs(t, err, os.ErrNotExist, "only the last component is created")
	_, err = os.Lstat(filepath.Join(outside, "new"))
	require.ErrorIs(t, err, os.ErrNotExist, "nothing is created outside the volume")
}

// Release leaves the volumes' own mount points alone and returns cleanly
// when there is nothing to release.
func TestReleaseWithoutBinds(t *testing.T) {
	require.NoError(t, Release(filepath.Join(t.TempDir(), "missing")))

	volumesDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(volumesDir, "ws"), 0o750))
	require.NoError(t, Release(volumesDir))
	_, err := os.Stat(filepath.Join(volumesDir, "ws"))
	require.NoError(t, err)
}

func TestPrepareAndRelease(t *testing.T) {
	roottest.Require(t, "bind mounts")

	volumesDir := t.TempDir()
	root := filepath.Join(volumesDir, "ws")
	for _, dir := range []string{"sessions/a", "mirrors"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "mirrors", "m.txt"), []byte("mirror"), 0o644))
	// A bind a stopped activation left behind.
	stale := filepath.Join(volumesDir, ocispec.MountDir("ws", "gone", false))
	require.NoError(t, os.Mkdir(stale, 0o750))
	require.NoError(t, unix.Mount(filepath.Join(root, "sessions"), stale, "", unix.MS_BIND, ""))

	// A read-only mount of the directory a read-write mount creates, listed
	// first, finds it.
	containers := []*ateworkerpb.Container{{
		ContainerSpec: &ateworkerpb.ContainerSpec{User: &ateworkerpb.ProcessUser{Uid: 0, Gid: 0}},
		ExternalVolumeMounts: []*ateworkerpb.VolumeMount{
			{Name: "ws", MountPath: "/new-ro", SubPath: "sessions/new", ReadOnly: true},
			{Name: "ws", MountPath: "/workspace", SubPath: "sessions/a"},
			{Name: "ws", MountPath: "/mirrors", SubPath: "mirrors", ReadOnly: true},
			{Name: "ws", MountPath: "/new", SubPath: "sessions/new"},
		},
	}}
	require.NoError(t, Prepare(volumesDir, containers))
	info, err := os.Stat(filepath.Join(root, "sessions", "new"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o770), info.Mode().Perm())
	t.Cleanup(func() { _ = Release(volumesDir) })

	_, err = os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist, "Prepare releases a bind no mount names")

	workspace := filepath.Join(volumesDir, ocispec.MountDir("ws", "sessions/a", false))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "w.txt"), []byte("w"), 0o644))
	_, err = os.Stat(filepath.Join(root, "sessions", "a", "w.txt"))
	require.NoError(t, err, "a write through the bind lands in the sub-path")

	mirrors := filepath.Join(volumesDir, ocispec.MountDir("ws", "mirrors", true))
	got, err := os.ReadFile(filepath.Join(mirrors, "m.txt"))
	require.NoError(t, err)
	require.Equal(t, "mirror", string(got))
	err = os.WriteFile(filepath.Join(mirrors, "x"), nil, 0o644)
	require.True(t, errors.Is(err, unix.EROFS), "write to a read-only bind = %v, want EROFS", err)

	require.NoError(t, Release(volumesDir))
	for _, dir := range []string{workspace, mirrors} {
		_, err := os.Stat(dir)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	_, err = os.Stat(filepath.Join(root, "sessions", "a", "w.txt"))
	require.NoError(t, err, "Release leaves the volume's content")
}

// A read-only mount of a sub-path that does not exist, a read-write mount
// whose parent does not exist, or a sub-path that crosses a link another
// actor planted fails Prepare: the actor must not start without the mount.
func TestPrepareFailsWithoutTheDirectory(t *testing.T) {
	roottest.Require(t, "bind mounts")

	volumesDir := t.TempDir()
	root := filepath.Join(volumesDir, "ws")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sessions"), 0o755))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "sessions", "link")))

	for _, vm := range []*ateworkerpb.VolumeMount{
		{Name: "ws", MountPath: "/workspace", SubPath: "sessions/missing", ReadOnly: true},
		{Name: "ws", MountPath: "/workspace", SubPath: "sessions/link", ReadOnly: true},
		{Name: "ws", MountPath: "/workspace", SubPath: "sessions/link"},
		{Name: "ws", MountPath: "/workspace", SubPath: "missing/new"},
	} {
		err := Prepare(volumesDir, []*ateworkerpb.Container{{ExternalVolumeMounts: []*ateworkerpb.VolumeMount{vm}}})
		require.Error(t, err, vm.GetSubPath())
	}
}
