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

// Package volumebind binds directories of an actor's mounted external
// volumes beside their mount points, read-only where asked, for the mounts
// that name a sub-path or are read-only (ocispec.MountDir). The worker runs
// it: it sees the volumes the CSI node plugin published on the host and may
// mount, which atelet may not, and the sandbox runtime it starts resolves the
// binds in its mount namespace.
package volumebind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.ate.dev/substrate/internal/ocispec"
	"go.ate.dev/substrate/internal/proto/ateworkerpb"
	"golang.org/x/sys/unix"
)

// boundMounts returns the containers' external volume mounts that bind a
// directory of their own, once per directory.
func boundMounts(containers []*ateworkerpb.Container) []*ateworkerpb.VolumeMount {
	var out []*ateworkerpb.VolumeMount
	seen := map[string]bool{}
	for _, ctr := range containers {
		for _, vm := range ctr.GetExternalVolumeMounts() {
			dir := ocispec.MountDir(vm.GetName(), vm.GetSubPath(), vm.GetReadOnly())
			if dir == vm.GetName() || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, vm)
		}
	}
	return out
}

// Prepare binds every directory the containers' external volume mounts bind
// from (ocispec.MountDir) under volumesDir, after releasing every bind an
// earlier activation left there. It runs before the sandbox starts. A
// directory that cannot be bound fails it, so the actor never starts without
// a mount it names.
func Prepare(volumesDir string, containers []*ateworkerpb.Container) error {
	if err := Release(volumesDir); err != nil {
		return err
	}
	for _, vm := range boundMounts(containers) {
		root := filepath.Join(volumesDir, vm.GetName())
		target := filepath.Join(volumesDir, ocispec.MountDir(vm.GetName(), vm.GetSubPath(), vm.GetReadOnly()))
		if err := bindVolumeDir(root, vm.GetSubPath(), target, vm.GetReadOnly()); err != nil {
			return fmt.Errorf("volume %q: %w", vm.GetName(), err)
		}
	}
	return nil
}

// Release unmounts every bind Prepare made under volumesDir and removes its
// mount point. It runs once the sandbox has stopped, before the volumes
// themselves are unmounted. It finds the binds by name rather than from a
// workload spec, so a bind of a mount that a later spec no longer names is
// released too: a bind's directory carries the suffix of ocispec.MountDir,
// which a volume's own mount point, named by a DNS label, never has.
func Release(volumesDir string) error {
	entries, err := os.ReadDir(volumesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the volumes directory %q: %w", volumesDir, err)
	}
	var errs []error
	for _, entry := range entries {
		if !strings.Contains(entry.Name(), ".") {
			continue
		}
		if err := unbindVolumeDir(filepath.Join(volumesDir, entry.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// volumeDirResolve resolves a path beneath the volume's root without
// following any symbolic link or crossing a mount.
const volumeDirResolve = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV

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
		Resolve: volumeDirResolve,
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
