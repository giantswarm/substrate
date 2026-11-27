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
// that name a sub-path or are read-only (ocispec.MountDir), and creates the
// directory of a read-write mount whose last component is missing. The
// worker runs it: it sees the volumes the CSI node plugin published on the
// host and may mount, which atelet may not, and the sandbox runtime it starts
// resolves the binds in its mount namespace.
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

// boundMount is a container's external volume mount that binds a directory
// of its own (ocispec.MountDir).
type boundMount struct {
	container *ateworkerpb.Container
	mount     *ateworkerpb.VolumeMount
}

// boundMounts returns the containers' external volume mounts that bind a
// directory of their own, once per directory.
func boundMounts(containers []*ateworkerpb.Container) []boundMount {
	var out []boundMount
	seen := map[string]bool{}
	for _, ctr := range containers {
		for _, vm := range ctr.GetExternalVolumeMounts() {
			dir := ocispec.MountDir(vm.GetName(), vm.GetSubPath(), vm.GetReadOnly())
			if dir == vm.GetName() || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, boundMount{container: ctr, mount: vm})
		}
	}
	return out
}

// Prepare binds every directory the containers' external volume mounts bind
// from (ocispec.MountDir) under volumesDir, after releasing every bind an
// earlier activation left there. The directory of a read-write mount is
// created first when its last component is missing (createVolumeDir), owned
// by the user the mounting container runs as: the caller that creates the
// actor may name a directory of its own that nothing has made yet. A
// read-only mount's directory must exist. It runs before the sandbox starts.
// A directory that cannot be bound fails it, so the actor never starts
// without a mount it names.
func Prepare(volumesDir string, containers []*ateworkerpb.Container) error {
	if err := Release(volumesDir); err != nil {
		return err
	}
	mounts := boundMounts(containers)
	// Every creation before any bind, so that a read-only mount of a directory
	// a read-write mount creates finds it whatever the order of the mounts.
	for _, bm := range mounts {
		if bm.mount.GetReadOnly() {
			continue
		}
		user := bm.container.GetContainerSpec().GetUser()
		if err := createVolumeDir(filepath.Join(volumesDir, bm.mount.GetName()), bm.mount.GetSubPath(), user.GetUid(), user.GetGid()); err != nil {
			return fmt.Errorf("volume %q: %w", bm.mount.GetName(), err)
		}
	}
	for _, bm := range mounts {
		vm := bm.mount
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

// createVolumeDir creates the directory subPath of the volume mounted at
// root when its last component is missing, owned by uid:gid and mode 0770
// whatever the umask, beneath its parent resolved like openVolumeDir resolves
// a sub-path. The parent must exist: an actor is given a directory of its
// own, not a tree, so a missing parent fails as it does at the bind.
// Whatever exists under the name is left as it is, and the bind that follows
// judges it: a directory binds, a symbolic link or a file fails. The volume's
// own refusal (a read-only file system, a permission) is reported with its
// reason.
func createVolumeDir(root, subPath string, uid, gid uint32) error {
	name := filepath.Base(subPath)
	if subPath == "" || name == "." || name == ".." {
		return nil
	}
	parentFd, err := openVolumeDir(root, parentDir(subPath))
	if err != nil {
		return fmt.Errorf("creating sub_path %q: its parent: %w", subPath, err)
	}
	defer unix.Close(parentFd)
	switch err := unix.Mkdirat(parentFd, name, 0o770); {
	case errors.Is(err, unix.EEXIST):
		return nil
	case err != nil:
		return fmt.Errorf("creating sub_path %q on the volume: %w", subPath, err)
	}
	// The directory is the container's own: its user owns it, and the mode
	// mkdirat left after the umask becomes 0770.
	fd, err := unix.Openat2(parentFd, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: volumeDirResolve,
	})
	if err != nil {
		return fmt.Errorf("opening the created sub_path %q: %w", subPath, err)
	}
	defer unix.Close(fd)
	if err := unix.Fchown(fd, int(uid), int(gid)); err != nil {
		return fmt.Errorf("giving the created sub_path %q to uid %d, gid %d: %w", subPath, uid, gid, err)
	}
	if err := unix.Fchmod(fd, 0o770); err != nil {
		return fmt.Errorf("setting the mode of the created sub_path %q: %w", subPath, err)
	}
	return nil
}

// parentDir is subPath's parent in the form of a sub-path: empty for the
// volume's root.
func parentDir(subPath string) string {
	if dir := filepath.Dir(subPath); dir != "." {
		return dir
	}
	return ""
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
