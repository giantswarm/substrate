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
	"testing"

	"github.com/agent-substrate/substrate/internal/proto/ateompb"
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

func TestBoundMounts(t *testing.T) {
	containers := []*ateompb.Container{
		{CsiVolumeMounts: []*ateompb.VolumeMount{
			{VolumeName: "ws", MountPath: "/root"},
			{VolumeName: "ws", MountPath: "/workspace", SubPath: "sessions/a"},
			{VolumeName: "ws", MountPath: "/mirrors", SubPath: "mirrors", ReadOnly: true},
		}},
		{CsiVolumeMounts: []*ateompb.VolumeMount{
			{VolumeName: "ws", MountPath: "/also", SubPath: "sessions/a"},
		}},
	}
	var got []string
	for _, vm := range boundMounts(containers) {
		got = append(got, vm.GetMountPath())
	}
	if want := []string{"/workspace", "/mirrors"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("boundMounts = %v, want %v (one per directory, none for the root mount)", got, want)
	}
}

// A mount of a sub-path or a read-only mount binds a directory of its own,
// one per sub-path and read-only flag, which never names a volume.
func TestMountDir(t *testing.T) {
	plain := MountDir("ws", "", false)
	sub := MountDir("ws", "sessions/a", false)
	subRO := MountDir("ws", "sessions/a", true)
	other := MountDir("ws", "sessions/b", false)
	if plain != "ws" {
		t.Errorf("plain mount dir = %q, want the volume's own", plain)
	}
	seen := map[string]bool{}
	for _, d := range []string{plain, sub, subRO, other} {
		if seen[d] {
			t.Errorf("mount dir %q is not distinct", d)
		}
		seen[d] = true
	}
	if !strings.HasPrefix(sub, "ws.") {
		t.Errorf("sub-path mount dir = %q, want ws.<hash>", sub)
	}
}
