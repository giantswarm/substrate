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

// Package volumebind binds directories of an actor's mounted external
// volumes beside their mount points, read-only where asked, for the mounts
// that name a sub-path or are read-only. ateom runs it: it sees the volumes
// the CSI node plugin published on the host and may mount, which atelet may
// not.
package volumebind

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agent-substrate/substrate/internal/proto/ateompb"
	"golang.org/x/sys/unix"
)

// MountDir is the directory under an actor's volumes directory that a mount
// of the external volume name binds from: the volume's own mount point, or,
// for a mount of a sub-path or a read-only mount, a bind of its own beside
// it. A volume name is a DNS label, so the suffix never names a volume.
func MountDir(name, subPath string, readOnly bool) string {
	if subPath == "" && !readOnly {
		return name
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%t", subPath, readOnly))
	return name + "." + hex.EncodeToString(sum[:6])
}

// boundMounts returns the containers' CSI volume mounts that bind a
// directory of their own (MountDir), once per directory.
func boundMounts(containers []*ateompb.Container) []*ateompb.VolumeMount {
	var out []*ateompb.VolumeMount
	seen := map[string]bool{}
	for _, ctr := range containers {
		for _, vm := range ctr.GetCsiVolumeMounts() {
			dir := MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly())
			if dir == vm.GetVolumeName() || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, vm)
		}
	}
	return out
}

// Prepare binds every directory the containers' mounts bind from (MountDir)
// under volumesDir, replacing a bind left by an earlier attempt. It runs
// before the sandbox starts.
func Prepare(volumesDir string, containers []*ateompb.Container) error {
	for _, vm := range boundMounts(containers) {
		root := filepath.Join(volumesDir, vm.GetVolumeName())
		target := filepath.Join(volumesDir, MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly()))
		if err := bindVolumeDir(root, vm.GetSubPath(), target, vm.GetReadOnly()); err != nil {
			return fmt.Errorf("volume %q: %w", vm.GetVolumeName(), err)
		}
	}
	return nil
}

// Release unmounts the binds of Prepare. It runs once the sandbox has
// stopped, before the volumes themselves are unmounted.
func Release(volumesDir string, containers []*ateompb.Container) error {
	var errs []error
	for _, vm := range boundMounts(containers) {
		if err := unbindVolumeDir(filepath.Join(volumesDir, MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly()))); err != nil {
			errs = append(errs, fmt.Errorf("volume %q: %w", vm.GetVolumeName(), err))
		}
	}
	return errors.Join(errs...)
}

// openVolumeDir opens the directory subPath of the volume mounted at root
// (root itself when subPath is empty) as an O_PATH descriptor. The path is
// resolved beneath root without following any symbolic link or crossing a
// mount: other actors write the volume, so a link they planted must not turn
// a mount into one of a directory outside it.
func openVolumeDir(root, subPath string) (int, error) {
	if subPath == "" {
		subPath = "."
	}
	rootFd, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("opening volume root %q: %w", root, err)
	}
	defer unix.Close(rootFd)
	fd, err := unix.Openat2(rootFd, subPath, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	switch {
	case errors.Is(err, unix.ENOENT):
		return -1, fmt.Errorf("sub_path %q does not exist on the volume", subPath)
	case errors.Is(err, unix.ELOOP):
		return -1, fmt.Errorf("sub_path %q crosses a symbolic link on the volume", subPath)
	case errors.Is(err, unix.ENOTDIR):
		return -1, fmt.Errorf("sub_path %q is not a directory on the volume", subPath)
	case err != nil:
		return -1, fmt.Errorf("opening sub_path %q of the volume: %w", subPath, err)
	}
	return fd, nil
}

// bindVolumeDir bind-mounts the directory subPath of the volume mounted at
// root onto target, read-only when readOnly. A bind left by an earlier
// attempt is replaced.
func bindVolumeDir(root, subPath, target string, readOnly bool) error {
	fd, err := openVolumeDir(root, subPath)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unbindVolumeDir(target); err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		return fmt.Errorf("creating mount point %q: %w", target, err)
	}
	// Binding from the descriptor mounts exactly the directory resolved above.
	if err := unix.Mount(fmt.Sprintf("/proc/self/fd/%d", fd), target, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind-mounting sub_path %q at %q: %w", subPath, target, err)
	}
	if readOnly {
		if err := unix.Mount("", target, "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_RDONLY, ""); err != nil {
			_ = unix.Unmount(target, unix.MNT_DETACH)
			return fmt.Errorf("making %q read-only: %w", target, err)
		}
	}
	return nil
}

// unbindVolumeDir unmounts a bind of bindVolumeDir and removes its mount
// point; a target that is absent or not mounted is left as it is.
func unbindVolumeDir(target string) error {
	if err := unix.Unmount(target, 0); err != nil && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("unmounting %q: %w", target, err)
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing mount point %q: %w", target, err)
	}
	return nil
}
