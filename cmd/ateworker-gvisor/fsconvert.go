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

// The fsconvert subcommand turns durable-dir volume tars into the fs/ bundle
// a VOLUMES restore reads, and restores such a bundle to compare it with the
// tars. It drives runsc with the worker's own spec and argument builders, so
// a bundle it writes is one this worker restores. ate-convert runs it; a
// worker built without the ateconvert tag does not have it.

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"go.ate.dev/substrate/internal/ateworker/actornet"
	"go.ate.dev/substrate/internal/imagecache"
	"go.ate.dev/substrate/internal/netns"
	"go.ate.dev/substrate/internal/nodepath"
	"go.ate.dev/substrate/internal/ocispec"
	"go.ate.dev/substrate/internal/proto/ateworkerpb"
	"golang.org/x/sys/unix"
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "fsconvert" {
		os.Exit(fsconvertMain(os.Args[2:]))
	}
}

// fsconvertSpec is the input of convert and restore, read from a JSON file.
type fsconvertSpec struct {
	// Runsc and Flags are the runsc binary and its internal_flags.json.
	Runsc string `json:"runsc"`
	Flags string `json:"flags"`
	// Root is an empty directory for the sandbox's actor directories.
	Root string `json:"root"`
	// Out is where convert writes the bundle, and restore reads it from.
	Out string `json:"out"`
	// Containers are the template's containers that mount durable-dir
	// volumes, with their names and mount paths.
	Containers []fsconvertContainer `json:"containers"`
	// Tars are the decoded volume tars by volume name.
	Tars map[string]string `json:"tars"`
}

type fsconvertContainer struct {
	Name   string           `json:"name"`
	Mounts []fsconvertMount `json:"mounts"`
}

type fsconvertMount struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

// fsconvertConvertResult lists the bundle's files, relative to Out.
type fsconvertConvertResult struct {
	Files []string `json:"files"`
}

// fsconvertVolumeResult compares one restored volume with its tar. It holds
// no path or content of the volume.
type fsconvertVolumeResult struct {
	Volume     string   `json:"volume"`
	Entries    int      `json:"entries"`
	Hash       string   `json:"hash"`
	Mismatches int      `json:"mismatches"`
	Fields     []string `json:"fields,omitempty"`
}

const (
	fsconvertStagingVolume = "fsconvert-staging"
	fsconvertStagingPath   = "/.fsconvert-staging"
	fsconvertBinary        = "/fsconvert"
)

func fsconvertMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: fsconvert convert|restore SPEC | extract TAR DIR | manifest DIR | hold")
		return 2
	}
	var err error
	switch {
	case args[0] == "hold":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
	case args[0] == "extract" && len(args) == 3:
		err = fsconvertExtract(args[1], args[2])
	case args[0] == "manifest" && len(args) == 2:
		err = writeTreeManifest(os.Stdout, args[1])
	case (args[0] == "convert" || args[0] == "restore") && len(args) == 2:
		err = fsconvertRun(context.Background(), args[0], args[1])
	default:
		err = fmt.Errorf("bad arguments %q", args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fsconvert:", err)
		return 1
	}
	return 0
}

func fsconvertRun(ctx context.Context, mode, specPath string) error {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var spec fsconvertSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("parsing %s: %w", specPath, err)
	}
	var result any
	if mode == "convert" {
		result, err = fsconvertConvert(ctx, &spec)
	} else {
		result, err = fsconvertRestore(ctx, &spec)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

// fsconvertSandbox is a sandbox of helper containers named and mounted as
// the template's, whose rootfs holds only this binary.
type fsconvertSandbox struct {
	r          *runsc
	containers []*ateworkerpb.Container
	created    []string
	netns      netns.Handle
}

func newFSConvertSandbox(spec *fsconvertSpec, staging bool) (*fsconvertSandbox, error) {
	if len(spec.Containers) == 0 {
		return nil, errors.New("no container mounts a durable-dir volume")
	}
	if err := os.MkdirAll(spec.Root, 0o700); err != nil {
		return nil, err
	}
	actorUID := uuid.NewString()
	actorDirs := &ateworkerpb.ActorDirs{
		RootDir:                   spec.Root,
		OciBundleDir:              filepath.Join(spec.Root, "bundles"),
		CheckpointDir:             filepath.Join(spec.Root, "checkpoint"),
		RestoreDir:                filepath.Join(spec.Root, "restore"),
		DurableDirVolumeMountsDir: filepath.Join(spec.Root, "durable-dir"),
		SystemInfoVolumeRootsDir:  filepath.Join(spec.Root, "system-info"),
		ExternalVolumeMountsDir:   filepath.Join(spec.Root, "external"),
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := requireStatic(self); err != nil {
		return nil, err
	}
	var containers []*ateworkerpb.Container
	for _, sc := range spec.Containers {
		c := &ateworkerpb.Container{
			Name: sc.Name,
			ContainerSpec: &ateworkerpb.ContainerSpec{
				Args:         []string{fsconvertBinary, "fsconvert", "hold"},
				Env:          []string{"PATH=/"},
				Capabilities: []string{"CAP_CHOWN", "CAP_FOWNER", "CAP_DAC_OVERRIDE", "CAP_FSETID", "CAP_MKNOD", "CAP_SYS_ADMIN"},
				User:         &ateworkerpb.ProcessUser{Uid: 0, Gid: 0},
			},
		}
		for _, m := range sc.Mounts {
			c.DurableDirVolumeMounts = append(c.DurableDirVolumeMounts, &ateworkerpb.VolumeMount{Name: m.Volume, MountPath: m.Path})
		}
		if staging {
			c.SystemInfoVolumeMounts = append(c.SystemInfoVolumeMounts, &ateworkerpb.VolumeMount{Name: fsconvertStagingVolume, MountPath: fsconvertStagingPath})
		}
		containers = append(containers, c)
		rootfs := filepath.Join(ociBundlePath(actorDirs, sc.Name), "rootfs")
		for _, d := range []string{filepath.Join(rootfs, "etc"), filepath.Join(rootfs, "tmp")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return nil, err
			}
		}
		if err := copyFile(self, filepath.Join(rootfs, fsconvertBinary), 0o755); err != nil {
			return nil, err
		}
		if err := actornet.WriteRootfsEtcFiles(rootfs, ocispec.Hostname); err != nil {
			return nil, err
		}
	}
	durable := durableVolumeNames(&ateworkerpb.WorkloadSpec{Containers: containers})
	for _, v := range durable {
		if err := os.MkdirAll(filepath.Join(actorDirs.GetDurableDirVolumeMountsDir(), v), 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Join(actorDirs.GetSystemInfoVolumeRootsDir(), fsconvertStagingVolume), 0o700); err != nil {
		return nil, err
	}
	if err := resetRunscStateAndPidFileDirs(actorDirs); err != nil {
		return nil, err
	}
	flags, err := newFlagStore(context.Background(), spec.Flags)
	if err != nil {
		return nil, err
	}
	ns, err := netns.CreateNamed(nodepath.ActorNetNSName(actorUID))
	if err != nil {
		return nil, err
	}
	return &fsconvertSandbox{
		r: &runsc{
			path:           spec.Runsc,
			actorUID:       actorUID,
			actorDirs:      actorDirs,
			durableVolumes: durable,
			containers:     containers,
			flags:          flags,
		},
		containers: containers,
		netns:      ns,
	}, nil
}

func (s *fsconvertSandbox) start(ctx context.Context, sandboxArgs []string) error {
	s.created = append(s.created, ocispec.SandboxID)
	if err := s.r.cmdCreate(ctx, os.Stderr, ocispec.SandboxID, sandboxArgs, nil); err != nil {
		return err
	}
	if err := s.r.cmdStart(ctx, os.Stderr, ocispec.SandboxID); err != nil {
		return err
	}
	for _, c := range s.containers {
		s.created = append(s.created, c.GetName())
		if err := s.r.cmdCreate(ctx, os.Stderr, c.GetName(), nil, nil); err != nil {
			return err
		}
		if err := s.r.cmdStart(ctx, os.Stderr, c.GetName()); err != nil {
			return err
		}
	}
	return nil
}

func (s *fsconvertSandbox) exec(ctx context.Context, stdout io.Writer, container string, argv ...string) error {
	cmd := exec.CommandContext(ctx, s.r.path, s.r.buildArgs("exec", "", append([]string{container}, argv...)...)...)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runsc exec %s in %s: %w", argv[0], container, err)
	}
	return nil
}

// mountOf returns the first container mounting volume and its mount path:
// the container a restore waits on for the volume.
func (s *fsconvertSandbox) mountOf(volume string) (string, string, bool) {
	for _, c := range s.containers {
		for _, m := range c.GetDurableDirVolumeMounts() {
			if m.GetName() == volume {
				return c.GetName(), m.GetMountPath(), true
			}
		}
	}
	return "", "", false
}

// close deletes the sandbox and removes what runsc leaves behind after a
// fscheckpoint ended it: the containers' state, their mounts and cgroup
// leaves, and the network namespace.
func (s *fsconvertSandbox) close() {
	ctx := context.Background()
	for i := len(s.created) - 1; i >= 0; i-- {
		_ = s.r.cmdDelete(ctx, s.created[i])
	}
	removeCgroupLeaves(s.r.actorUID, s.created)
	_ = netns.RemoveNamed(nodepath.ActorNetNSName(s.r.actorUID))
	_ = s.netns.Close()
	if err := imagecache.UnmountAllUnder(s.r.actorDirs.GetRootDir()); err != nil {
		fmt.Fprintln(os.Stderr, "fsconvert: unmounting:", err)
	}
}

// removeCgroupLeaves removes the empty cgroup leaves runsc created for the
// containers, which it does not remove when the sandbox ended before its
// delete. runsc creates them next to the cgroup it runs in.
func removeCgroupLeaves(actorUID string, containers []string) {
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return
	}
	self, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "0::")
	if !ok {
		return
	}
	for _, parent := range []string{filepath.Dir(self), self} {
		for _, c := range containers {
			leaf := filepath.Join("/sys/fs/cgroup", parent, ocispec.GVisorCgroupLeaf(actorUID, c))
			if err := unix.Rmdir(leaf); err != nil && !errors.Is(err, unix.ENOENT) {
				fmt.Fprintf(os.Stderr, "fsconvert: removing cgroup %s: %v\n", leaf, err)
			}
		}
	}
}

func fsconvertConvert(ctx context.Context, spec *fsconvertSpec) (*fsconvertConvertResult, error) {
	s, err := newFSConvertSandbox(spec, true)
	if err != nil {
		return nil, err
	}
	defer s.close()
	staging := filepath.Join(s.r.actorDirs.GetSystemInfoVolumeRootsDir(), fsconvertStagingVolume)
	volumes := make([]string, 0, len(spec.Tars))
	for vol, path := range spec.Tars {
		if _, _, ok := s.mountOf(vol); !ok {
			return nil, fmt.Errorf("volume %q has a tar but no container mounts it", vol)
		}
		if err := os.Link(path, filepath.Join(staging, vol+".tar")); err != nil {
			return nil, fmt.Errorf("staging volume %q: %w", vol, err)
		}
		volumes = append(volumes, vol)
	}
	sort.Strings(volumes)
	if err := s.start(ctx, nil); err != nil {
		return nil, err
	}
	for _, vol := range volumes {
		c, mp, _ := s.mountOf(vol)
		if err := s.exec(ctx, os.Stderr, c, fsconvertBinary, "fsconvert", "extract", fsconvertStagingPath+"/"+vol+".tar", mp); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(spec.Out, 0o700); err != nil {
		return nil, err
	}
	// fscheckpoint ends the sandbox, so it takes every volume at once.
	if err := s.r.cmdFsCheckpoint(ctx, ocispec.SandboxID, spec.Out, durableFSCheckpointPaths(s.containers)); err != nil {
		return nil, err
	}
	files, err := listSnapshotFiles(spec.Out)
	if err != nil {
		return nil, err
	}
	return &fsconvertConvertResult{Files: files}, nil
}

func fsconvertRestore(ctx context.Context, spec *fsconvertSpec) ([]fsconvertVolumeResult, error) {
	s, err := newFSConvertSandbox(spec, false)
	if err != nil {
		return nil, err
	}
	defer s.close()
	args, err := fsRestoreArgs(spec.Out, ateworkerpb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 && len(spec.Tars) > 0 {
		return nil, fmt.Errorf("%s holds no %s/%s", spec.Out, "fs", fsCheckpointManifest)
	}
	if err := s.start(ctx, args); err != nil {
		return nil, err
	}
	// A worker only logs a failed wait; here it fails the comparison.
	for _, c := range fsRestoreContainers(ateworkerpb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES, s.containers) {
		if err := s.r.cmdWaitFSRestore(ctx, c); err != nil {
			return nil, fmt.Errorf("restoring the volumes of %s: %w", c, err)
		}
	}
	volumes := slices.Sorted(func(yield func(string) bool) {
		for v := range spec.Tars {
			if !yield(v) {
				return
			}
		}
	})
	var results []fsconvertVolumeResult
	for _, vol := range volumes {
		want, err := tarManifest(spec.Tars[vol])
		if err != nil {
			return nil, fmt.Errorf("volume %q: %w", vol, err)
		}
		c, mp, ok := s.mountOf(vol)
		if !ok {
			return nil, fmt.Errorf("volume %q has a tar but no container mounts it", vol)
		}
		var out bytes.Buffer
		if err := s.exec(ctx, &out, c, fsconvertBinary, "fsconvert", "manifest", mp); err != nil {
			return nil, err
		}
		got, err := readTreeManifest(&out)
		if err != nil {
			return nil, fmt.Errorf("volume %q: %w", vol, err)
		}
		results = append(results, compareManifests(vol, want, got))
	}
	return results, nil
}

// requireStatic refuses a dynamically linked binary: it is the helpers'
// whole rootfs, which has no loader or libraries.
func requireStatic(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return fmt.Errorf("%s is dynamically linked; build it with CGO_ENABLED=0", path)
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fsconvertExtract extracts a durable-dir tar into dst, the volume's root,
// keeping owners, modes with their set-id and sticky bits, user.* xattrs,
// hard links, symlinks and times, writing runs of zeros as holes. It skips
// gVisor's .gvisor.* files and refuses an entry outside dst. It runs inside
// the sandbox, so the tmpfs of the volume receives the files.
func fsconvertExtract(tarPath, dst string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(bufio.NewReaderSize(f, 1<<20))
	type dirMeta struct {
		path string
		hdr  *tar.Header
	}
	var dirs []dirMeta
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name, skip, err := volumeEntryName(hdr.Name)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		p := filepath.Join(dst, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if name != "." {
				if err := os.Mkdir(p, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
					return err
				}
			}
			dirs = append(dirs, dirMeta{p, hdr})
			continue
		case tar.TypeReg:
			if err := writeSparse(p, tr); err != nil {
				return fmt.Errorf("writing an entry: %w", err)
			}
		case tar.TypeSymlink:
			if err := os.Symlink(hdr.Linkname, p); err != nil {
				return err
			}
		case tar.TypeLink:
			target, _, err := volumeEntryName(hdr.Linkname)
			if err != nil || target == "." {
				return errors.New("a hard link leaves the volume")
			}
			if err := os.Link(filepath.Join(dst, target), p); err != nil {
				return err
			}
			continue
		case tar.TypeFifo:
			if err := unix.Mkfifo(p, 0o600); err != nil {
				return err
			}
		default:
			return fmt.Errorf("entry type %q is not supported", hdr.Typeflag)
		}
		if err := applyMeta(p, hdr); err != nil {
			return err
		}
	}
	// Last, and children first, so creating an entry does not move the mtime
	// of its directory.
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := applyMeta(dirs[i].path, dirs[i].hdr); err != nil {
			return err
		}
	}
	return nil
}

// volumeEntryName returns the clean name of a tar entry below the volume
// root ("." for the root itself), and whether it is a gVisor file to skip.
func volumeEntryName(name string) (string, bool, error) {
	if name == "/" {
		return ".", false, nil
	}
	clean := filepath.Clean(name)
	if clean == "." {
		return ".", false, nil
	}
	if !filepath.IsLocal(clean) {
		return "", false, errors.New("an entry leaves the volume")
	}
	return clean, strings.HasPrefix(filepath.Base(clean), ".gvisor."), nil
}

// writeSparse writes r to a new file at p, leaving 64 KiB runs of zeros as
// holes.
func writeSparse(p string, r io.Reader) error {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	var off int64
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			if slices.ContainsFunc(buf[:n], func(c byte) bool { return c != 0 }) {
				if _, werr := f.WriteAt(buf[:n], off); werr != nil {
					return werr
				}
			}
			off += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return f.Truncate(off)
}

// applyMeta applies owner, then mode (chown clears set-id bits), user.*
// xattrs and times, never following a final symlink. The durable volume's
// tmpfs takes only user.* xattrs from a workload.
func applyMeta(p string, hdr *tar.Header) error {
	if err := os.Lchown(p, hdr.Uid, hdr.Gid); err != nil {
		return err
	}
	if hdr.Typeflag != tar.TypeSymlink {
		if err := unix.Chmod(p, uint32(hdr.Mode&0o7777)); err != nil {
			return err
		}
	}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range hdr.PAXRecords {
			if !yield(k) {
				return
			}
		}
	}) {
		attr, ok := strings.CutPrefix(k, "SCHILY.xattr.")
		if !ok || !strings.HasPrefix(attr, "user.") {
			continue
		}
		if err := unix.Lsetxattr(p, attr, []byte(hdr.PAXRecords[k]), 0); err != nil {
			return err
		}
	}
	mtime := unix.NsecToTimespec(hdr.ModTime.UnixNano())
	atime := mtime
	if !hdr.AccessTime.IsZero() {
		atime = unix.NsecToTimespec(hdr.AccessTime.UnixNano())
	}
	return unix.UtimesNanoAt(unix.AT_FDCWD, p, []unix.Timespec{atime, mtime}, unix.AT_SYMLINK_NOFOLLOW)
}

// treeEntry is what a restore must bring back of one file of a volume. A
// symlink's mode and mtime and a directory's size are not compared.
type treeEntry struct {
	Path   string            `json:"path"`
	Type   string            `json:"type"`
	Mode   uint32            `json:"mode,omitempty"`
	UID    int               `json:"uid"`
	GID    int               `json:"gid"`
	Size   int64             `json:"size,omitempty"`
	SHA256 string            `json:"sha256,omitempty"`
	Link   string            `json:"link,omitempty"`
	Peer   string            `json:"peer,omitempty"`
	MTime  int64             `json:"mtime,omitempty"`
	Xattrs map[string]string `json:"xattrs,omitempty"`
}

// tarManifest returns the entries a tar extracts to, sorted by path. Each
// file of a hard-link group names the group's first path as its peer.
func tarManifest(tarPath string) ([]treeEntry, error) {
	f, err := os.Open(tarPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tr := tar.NewReader(bufio.NewReaderSize(f, 1<<20))
	byPath := map[string]*treeEntry{}
	links := map[string]string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		name, skip, err := volumeEntryName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if skip {
			continue
		}
		e := &treeEntry{Path: name, UID: hdr.Uid, GID: hdr.Gid, Mode: uint32(hdr.Mode & 0o7777), MTime: hdr.ModTime.UnixNano()}
		for k, v := range hdr.PAXRecords {
			if attr, ok := strings.CutPrefix(k, "SCHILY.xattr."); ok && strings.HasPrefix(attr, "user.") {
				if e.Xattrs == nil {
					e.Xattrs = map[string]string{}
				}
				e.Xattrs[attr] = hex.EncodeToString([]byte(v))
			}
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			e.Type = "dir"
		case tar.TypeReg:
			e.Type = "file"
			h := sha256.New()
			n, err := io.Copy(h, tr)
			if err != nil {
				return nil, err
			}
			e.Size, e.SHA256 = n, hex.EncodeToString(h.Sum(nil))
		case tar.TypeSymlink:
			e.Type, e.Link, e.Mode, e.MTime = "symlink", hdr.Linkname, 0, 0
		case tar.TypeFifo:
			e.Type = "fifo"
		case tar.TypeLink:
			target, _, err := volumeEntryName(hdr.Linkname)
			if err != nil {
				return nil, err
			}
			links[name] = target
			continue
		default:
			e.Type = string(hdr.Typeflag)
		}
		byPath[name] = e
	}
	// A hard link is a second name of its target's file.
	groups := map[string][]string{}
	for name, target := range links {
		for {
			next, ok := links[target]
			if !ok {
				break
			}
			target = next
		}
		t, ok := byPath[target]
		if !ok {
			return nil, errors.New("a hard link names no file")
		}
		e := *t
		e.Path = name
		byPath[name] = &e
		groups[target] = append(groups[target], name)
	}
	for target, names := range groups {
		peer := slices.Min(append(names, target))
		for _, n := range append(names, target) {
			byPath[n].Peer = peer
		}
	}
	out := make([]treeEntry, 0, len(byPath))
	for _, e := range byPath {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// dirManifest returns the entries under root, sorted by path, as tarManifest
// does for a tar. It skips gVisor's .gvisor.* files.
func dirManifest(root string) ([]treeEntry, error) {
	var out []treeEntry
	inodes := map[uint64][]int{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".gvisor.") && rel != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		var st unix.Stat_t
		if err := unix.Lstat(p, &st); err != nil {
			return err
		}
		e := treeEntry{Path: rel, UID: int(st.Uid), GID: int(st.Gid), Mode: st.Mode & 0o7777, MTime: st.Mtim.Nano()}
		if xattrs, err := userXattrs(p); err != nil {
			return err
		} else if len(xattrs) > 0 {
			e.Xattrs = xattrs
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			e.Type = "dir"
		case unix.S_IFREG:
			e.Type = "file"
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			h := sha256.New()
			n, err := io.Copy(h, f)
			f.Close()
			if err != nil {
				return err
			}
			e.Size, e.SHA256 = n, hex.EncodeToString(h.Sum(nil))
			if st.Nlink > 1 {
				inodes[st.Ino] = append(inodes[st.Ino], len(out))
			}
		case unix.S_IFLNK:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			e.Type, e.Link, e.Mode, e.MTime = "symlink", target, 0, 0
		case unix.S_IFIFO:
			e.Type = "fifo"
		default:
			e.Type = fmt.Sprintf("mode-%o", st.Mode&unix.S_IFMT)
		}
		out = append(out, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, idx := range inodes {
		if len(idx) < 2 {
			continue
		}
		peer := out[idx[0]].Path
		for _, i := range idx {
			peer = min(peer, out[i].Path)
		}
		for _, i := range idx {
			out[i].Peer = peer
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func userXattrs(p string) (map[string]string, error) {
	size, err := unix.Llistxattr(p, nil)
	if err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			return nil, nil
		}
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	buf := make([]byte, size)
	if size, err = unix.Llistxattr(p, buf); err != nil {
		return nil, err
	}
	var out map[string]string
	for _, name := range strings.Split(strings.TrimRight(string(buf[:size]), "\x00"), "\x00") {
		if !strings.HasPrefix(name, "user.") {
			continue
		}
		vsize, err := unix.Lgetxattr(p, name, nil)
		if err != nil {
			return nil, err
		}
		val := make([]byte, vsize)
		if vsize, err = unix.Lgetxattr(p, name, val); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = hex.EncodeToString(val[:vsize])
	}
	return out, nil
}

func writeTreeManifest(w io.Writer, root string) error {
	entries, err := dirManifest(root)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func readTreeManifest(r io.Reader) ([]treeEntry, error) {
	var out []treeEntry
	dec := json.NewDecoder(r)
	for {
		var e treeEntry
		if err := dec.Decode(&e); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
}

// compareManifests counts the entries of got that differ from want and
// names the fields that differ, never a path or a value.
func compareManifests(volume string, want, got []treeEntry) fsconvertVolumeResult {
	res := fsconvertVolumeResult{Volume: volume, Entries: len(want)}
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, e := range want {
		_ = enc.Encode(e)
	}
	res.Hash = hex.EncodeToString(h.Sum(nil))[:16]
	fields := map[string]bool{}
	gotByPath := make(map[string]treeEntry, len(got))
	for _, e := range got {
		gotByPath[e.Path] = e
	}
	for _, w := range want {
		g, ok := gotByPath[w.Path]
		delete(gotByPath, w.Path)
		if !ok {
			res.Mismatches++
			fields["missing"] = true
			continue
		}
		diff := entryDiff(w, g)
		if len(diff) > 0 {
			res.Mismatches++
			for _, f := range diff {
				fields[f] = true
			}
		}
	}
	if len(gotByPath) > 0 {
		res.Mismatches += len(gotByPath)
		fields["extra"] = true
	}
	for f := range fields {
		res.Fields = append(res.Fields, f)
	}
	sort.Strings(res.Fields)
	return res
}

func entryDiff(want, got treeEntry) []string {
	var out []string
	add := func(name string, differ bool) {
		if differ {
			out = append(out, name)
		}
	}
	add("type", want.Type != got.Type)
	add("mode", want.Mode != got.Mode)
	add("owner", want.UID != got.UID || want.GID != got.GID)
	add("size", want.Type == "file" && want.Size != got.Size)
	add("content", want.SHA256 != got.SHA256)
	add("link", want.Link != got.Link)
	add("hardlink", want.Peer != got.Peer)
	add("mtime", want.MTime != got.MTime)
	add("xattrs", !mapsEqual(want.Xattrs, got.Xattrs))
	return out
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}
