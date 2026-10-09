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
// that name a sub-path or are read-only, and creates the directory of a
// read-write mount whose last component is missing. ateom runs it: it sees
// the volumes the CSI node plugin published on the host and may mount, which
// atelet may not.
package volumebind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agent-substrate/substrate/internal/ocispec"
	"github.com/agent-substrate/substrate/internal/proto/ateompb"
	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

// boundMount is a container's CSI volume mount that binds a directory of its
// own (ocispec.MountDir).
type boundMount struct {
	container string
	mount     *ateompb.VolumeMount
}

// boundMounts returns the containers' CSI volume mounts that bind a
// directory of their own, once per directory.
func boundMounts(containers []*ateompb.Container) []boundMount {
	var out []boundMount
	seen := map[string]bool{}
	for _, ctr := range containers {
		for _, vm := range ctr.GetCsiVolumeMounts() {
			dir := ocispec.MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly())
			if dir == vm.GetVolumeName() || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, boundMount{container: ctr.GetName(), mount: vm})
		}
	}
	return out
}

// Prepare binds every directory the containers' mounts bind from
// (ocispec.MountDir) under volumesDir, replacing a bind left by an earlier
// attempt. The directory of a read-write mount is created first when its
// last component is missing (createVolumeDir), owned by the user the
// mounting container runs as, read from its OCI bundle under bundlesDir: the
// caller that creates the actor may name a directory of its own that nothing
// has made yet. A read-only mount's directory must exist. It runs before the
// sandbox starts.
func Prepare(volumesDir, bundlesDir string, containers []*ateompb.Container) error {
	mounts := boundMounts(containers)
	// Every creation before any bind, so that a read-only mount of a directory
	// a read-write mount creates finds it whatever the order of the mounts.
	users := map[string]specs.User{}
	for _, bm := range mounts {
		if bm.mount.GetReadOnly() {
			continue
		}
		user, ok := users[bm.container]
		if !ok {
			var err error
			if user, err = processUser(filepath.Join(bundlesDir, bm.container)); err != nil {
				return fmt.Errorf("container %q: %w", bm.container, err)
			}
			users[bm.container] = user
		}
		if err := createVolumeDir(filepath.Join(volumesDir, bm.mount.GetVolumeName()), bm.mount.GetSubPath(), user); err != nil {
			return fmt.Errorf("volume %q: %w", bm.mount.GetVolumeName(), err)
		}
	}
	for _, bm := range mounts {
		vm := bm.mount
		root := filepath.Join(volumesDir, vm.GetVolumeName())
		target := filepath.Join(volumesDir, ocispec.MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly()))
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
	for _, bm := range boundMounts(containers) {
		vm := bm.mount
		if err := unbindVolumeDir(filepath.Join(volumesDir, ocispec.MountDir(vm.GetVolumeName(), vm.GetSubPath(), vm.GetReadOnly()))); err != nil {
			errs = append(errs, fmt.Errorf("volume %q: %w", vm.GetVolumeName(), err))
		}
	}
	return errors.Join(errs...)
}

// processUser returns the user the container of the OCI bundle at bundlePath
// runs as.
func processUser(bundlePath string) (specs.User, error) {
	spec, err := ocispec.Load(bundlePath)
	if err != nil {
		return specs.User{}, fmt.Errorf("reading its OCI spec: %w", err)
	}
	if spec.Process == nil {
		return specs.User{}, nil
	}
	return spec.Process.User, nil
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
// root when its last component is missing, owned by user and mode 0770
// whatever the umask, beneath its parent resolved like openVolumeDir resolves
// a sub-path. The parent must exist: an actor is given a directory of its
// own, not a tree, so a missing parent fails as it does at the bind.
// Whatever exists under the name is left as it is, and the bind that follows
// judges it: a directory binds, a symbolic link or a file fails. The volume's
// own refusal (a read-only file system, a permission) is reported with its
// reason.
func createVolumeDir(root, subPath string, user specs.User) error {
	name := filepath.Base(subPath)
	if name == "." || name == ".." {
		return nil
	}
	parentFd, err := openVolumeDir(root, filepath.Dir(subPath))
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
	if err := unix.Fchown(fd, int(user.UID), int(user.GID)); err != nil {
		return fmt.Errorf("giving the created sub_path %q to uid %d, gid %d: %w", subPath, user.UID, user.GID, err)
	}
	if err := unix.Fchmod(fd, 0o770); err != nil {
		return fmt.Errorf("setting the mode of the created sub_path %q: %w", subPath, err)
	}
	return nil
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
