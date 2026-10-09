// Copyright 2026 Google LLC
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
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/agent-substrate/substrate/internal/ocispec"
	"github.com/agent-substrate/substrate/internal/proto/ateompb"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

func TestOpenVolumeDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"sessions/a", "mirrors"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Links another actor could plant on the shared volume.
	if err := os.Symlink(outside, filepath.Join(root, "sessions", "b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../mirrors", filepath.Join(root, "sessions", "c")); err != nil {
		t.Fatal(err)
	}

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
				if err != nil {
					t.Fatalf("openVolumeDir(%q): %v", tc.subPath, err)
				}
				unix.Close(fd)
				return
			}
			if err == nil {
				unix.Close(fd)
				t.Fatalf("openVolumeDir(%q) succeeded, want %q", tc.subPath, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("openVolumeDir(%q) = %v, want %q", tc.subPath, err, tc.wantErr)
			}
		})
	}
}

// A read-write mount's directory is created when its last component is
// missing, owned by the container's user and 0770 whatever the umask;
// everything else fails or is left alone the way the bind judges it.
func TestCreateVolumeDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"sessions/a", "unwritable"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sessions", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sessions", filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "unwritable"), 0o555); err != nil {
		t.Fatal(err)
	}
	unix.Umask(0o022)
	// The test's own identity: the one user an unprivileged test may give a
	// directory to.
	user := specs.User{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}

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
			err := createVolumeDir(root, tc.subPath, user)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("createVolumeDir(%q) = %v, want %q", tc.subPath, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("createVolumeDir(%q): %v", tc.subPath, err)
			}
			fd, err := openVolumeDir(root, tc.subPath)
			if tc.wantBind != "" {
				if err == nil {
					unix.Close(fd)
					t.Fatalf("openVolumeDir(%q) succeeded after the create, want %q", tc.subPath, tc.wantBind)
				}
				if !strings.Contains(err.Error(), tc.wantBind) {
					t.Errorf("openVolumeDir(%q) = %v, want %q", tc.subPath, err, tc.wantBind)
				}
				return
			}
			if err != nil {
				t.Fatalf("openVolumeDir(%q) after the create: %v", tc.subPath, err)
			}
			unix.Close(fd)
			if tc.wantMode == 0 {
				return
			}
			info, err := os.Lstat(filepath.Join(root, tc.subPath))
			if err != nil {
				t.Fatal(err)
			}
			if !info.IsDir() || info.Mode().Perm() != tc.wantMode {
				t.Errorf("%q is %v, want a directory of mode %o", tc.subPath, info.Mode(), tc.wantMode)
			}
			if st := info.Sys().(*syscall.Stat_t); st.Uid != user.UID || st.Gid != user.GID {
				t.Errorf("%q is owned by %d:%d, want %d:%d", tc.subPath, st.Uid, st.Gid, user.UID, user.GID)
			}
		})
	}
	if _, err := os.Lstat(filepath.Join(root, "sessions", "missing")); !os.IsNotExist(err) {
		t.Errorf("the missing parent was created (%v), want only the last component created", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Errorf("a directory was created outside the volume (%v)", err)
	}
}

// The user a created directory is given is the one the container's OCI spec
// runs it as; root without one.
func TestProcessUser(t *testing.T) {
	bundles := t.TempDir()
	for name, spec := range map[string]*specs.Spec{
		"app":   {Process: &specs.Process{User: specs.User{UID: 1000, GID: 1001}}},
		"pause": {},
	} {
		if err := os.Mkdir(filepath.Join(bundles, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ocispec.Save(filepath.Join(bundles, name), spec); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		container string
		want      specs.User
		wantErr   string
	}{
		{container: "app", want: specs.User{UID: 1000, GID: 1001}},
		{container: "pause"},
		{container: "missing", wantErr: "reading its OCI spec"},
	} {
		got, err := processUser(filepath.Join(bundles, tc.container))
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("processUser(%q) = %v, want %q", tc.container, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("processUser(%q): %v", tc.container, err)
		} else if got.UID != tc.want.UID || got.GID != tc.want.GID {
			t.Errorf("processUser(%q) = %d:%d, want %d:%d", tc.container, got.UID, got.GID, tc.want.UID, tc.want.GID)
		}
	}
}

func TestBoundMounts(t *testing.T) {
	containers := []*ateompb.Container{
		{Name: "main", CsiVolumeMounts: []*ateompb.VolumeMount{
			{VolumeName: "ws", MountPath: "/root"},
			{VolumeName: "ws", MountPath: "/workspace", SubPath: "sessions/a"},
			{VolumeName: "ws", MountPath: "/mirrors", SubPath: "mirrors", ReadOnly: true},
		}},
		{Name: "sidecar", CsiVolumeMounts: []*ateompb.VolumeMount{
			{VolumeName: "ws", MountPath: "/also", SubPath: "sessions/a"},
		}},
	}
	var got []string
	for _, bm := range boundMounts(containers) {
		got = append(got, bm.container+":"+bm.mount.GetMountPath())
	}
	if want := []string{"main:/workspace", "main:/mirrors"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("boundMounts = %v, want %v (one per directory, none for the root mount)", got, want)
	}
}
