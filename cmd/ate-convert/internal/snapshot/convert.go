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

// Package snapshot converts the objects of a 1.x snapshot in place into the
// fs/ bundle and manifest a 2.x VOLUMES restore reads.
package snapshot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"

	"go.ate.dev/substrate/internal/resources"
	"go.ate.dev/substrate/pkg/objectstorage"
)

// Container is one container of the template a snapshot is restored into,
// with its durable-dir mounts.
type Container struct {
	Name   string  `json:"name"`
	Mounts []Mount `json:"mounts"`
}

// Mount is a durable-dir volume mounted at a path.
type Mount struct {
	Volume string `json:"volume"`
	Path   string `json:"path"`
}

// Job is one snapshot to convert, and the containers it is restored into.
type Job struct {
	URI        string
	Containers []Container
}

// State is what a snapshot's objects hold.
type State string

const (
	// State1x is a 1.x snapshot, to convert.
	State1x State = "1.x"
	// StateConverted is a converted snapshot: a 2.x manifest and the 1.x
	// one kept beside it.
	StateConverted State = "converted"
)

// Converter converts snapshots through a worker binary with the fsconvert
// subcommand, which needs root and a gVisor-capable host.
type Converter struct {
	Store    objectstorage.ObjectStorage
	Uploader *objectstorage.Uploader
	// WorkerBinary is an ateworker-gvisor built with the ateconvert tag.
	WorkerBinary string
	// Runsc and RunscFlags are the runsc of the 2.x default SandboxConfig
	// version and the worker's internal_flags.json.
	Runsc      string
	RunscFlags string
	// WorkDir holds one job's files at a time.
	WorkDir string
	// Asset is the sandbox asset the converted manifest records.
	Asset Asset
	Log   io.Writer
}

func objectURI(uri resources.SnapshotURI, name string) string {
	u, _ := uri.ObjectURI(name)
	return u
}

// Inspect returns the state of a snapshot's objects and its manifest. A
// snapshot without a manifest, or with a 2.x one and no 1.x one beside it,
// is not one a conversion handles.
func (c *Converter) Inspect(ctx context.Context, job Job) (State, *manifest, error) {
	uri, err := resources.ParseSnapshotURI(job.URI)
	if err != nil {
		return "", nil, err
	}
	raw, err := objectstorage.FetchFromGCS(ctx, c.Store, objectURI(uri, manifestFile))
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", manifestFile, err)
	}
	m, err := parseManifest(raw)
	if err != nil {
		return "", nil, err
	}
	if !m.is2x() {
		return State1x, m, nil
	}
	forkRaw, err := objectstorage.FetchFromGCS(ctx, c.Store, objectURI(uri, forkManifestFile))
	if err != nil {
		return "", nil, fmt.Errorf("a 2.x %s without %s: %w", manifestFile, forkManifestFile, err)
	}
	fork, err := parseManifest(forkRaw)
	if err != nil {
		return "", nil, err
	}
	return StateConverted, fork, nil
}

// Convert converts one snapshot, unless it is converted already. It restores
// the bundle in a scratch sandbox and compares it with the tars before it
// uploads anything, uploads the fs/ files, keeps the 1.x manifest as
// manifest.fork.json, and writes the 2.x manifest last, which commits the
// conversion. It deletes nothing.
func (c *Converter) Convert(ctx context.Context, job Job) error {
	state, fork, err := c.Inspect(ctx, job)
	if err != nil {
		return err
	}
	if state == StateConverted {
		fmt.Fprintf(c.Log, "snapshot %s: converted already\n", job.URI)
		return nil
	}
	uri, _ := resources.ParseSnapshotURI(job.URI)
	dir, err := c.jobDir(job)
	if err != nil {
		return err
	}
	defer c.removeJobDir(dir)
	tars, err := c.fetchTars(ctx, uri, fork, job, dir)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "out")
	var files []string
	if len(tars) > 0 {
		var res convertResult
		if err := c.runWorker(ctx, "convert", spec{Runsc: c.Runsc, Flags: c.RunscFlags, Root: filepath.Join(dir, "convert"), Out: out, Containers: job.Containers, Tars: tars}, &res); err != nil {
			return err
		}
		files = res.Files
		if err := c.compare(ctx, dir, out, job, tars); err != nil {
			return err
		}
	}
	for _, f := range files {
		local, err := os.Open(filepath.Join(out, f))
		if err != nil {
			return err
		}
		err = c.Uploader.SendFile(ctx, objectURI(uri, f+".zstd"), local)
		local.Close()
		if err != nil {
			return fmt.Errorf("uploading %s: %w", f, err)
		}
	}
	forkRaw, err := objectstorage.FetchFromGCS(ctx, c.Store, objectURI(uri, manifestFile))
	if err != nil {
		return err
	}
	if err := c.Uploader.Send(ctx, objectURI(uri, forkManifestFile), bytes.NewReader(forkRaw)); err != nil {
		return fmt.Errorf("keeping the 1.x manifest: %w", err)
	}
	converted, err := json.Marshal(convertedManifest(fork, files, c.Asset))
	if err != nil {
		return err
	}
	if err := c.Uploader.Send(ctx, objectURI(uri, manifestFile), bytes.NewReader(converted)); err != nil {
		return fmt.Errorf("writing the 2.x manifest: %w", err)
	}
	fmt.Fprintf(c.Log, "snapshot %s: converted, %d volumes, %d files\n", job.URI, len(tars), len(files))
	return nil
}

// Verify restores a converted snapshot in a scratch sandbox and compares it
// with the 1.x tars it was converted from.
func (c *Converter) Verify(ctx context.Context, job Job) error {
	state, fork, err := c.Inspect(ctx, job)
	if err != nil {
		return err
	}
	if state != StateConverted {
		return fmt.Errorf("snapshot %s is not converted", job.URI)
	}
	uri, _ := resources.ParseSnapshotURI(job.URI)
	raw, err := objectstorage.FetchFromGCS(ctx, c.Store, objectURI(uri, manifestFile))
	if err != nil {
		return err
	}
	converted, err := parseManifest(raw)
	if err != nil {
		return err
	}
	dir, err := c.jobDir(job)
	if err != nil {
		return err
	}
	defer c.removeJobDir(dir)
	tars, err := c.fetchTars(ctx, uri, fork, job, dir)
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "out")
	for _, f := range converted.SnapshotFiles {
		local := filepath.Join(out, f)
		if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
			return err
		}
		if err := objectstorage.FetchLocalFileFromGCSWithZstd(ctx, c.Store, objectURI(uri, f+".zstd"), local); err != nil {
			return fmt.Errorf("fetching %s: %w", f, err)
		}
	}
	if len(tars) == 0 {
		if len(converted.SnapshotFiles) > 0 {
			return fmt.Errorf("snapshot %s: files without 1.x tars", job.URI)
		}
		fmt.Fprintf(c.Log, "snapshot %s: no durable volumes ok\n", job.URI)
		return nil
	}
	return c.compare(ctx, dir, out, job, tars)
}

// compare restores out and fails unless every volume matches its tar.
func (c *Converter) compare(ctx context.Context, dir, out string, job Job, tars map[string]string) error {
	var results []volumeResult
	if err := c.runWorker(ctx, "restore", spec{Runsc: c.Runsc, Flags: c.RunscFlags, Root: filepath.Join(dir, "restore"), Out: out, Containers: job.Containers, Tars: tars}, &results); err != nil {
		return err
	}
	var errs []error
	for _, r := range results {
		status := "ok"
		if r.Mismatches > 0 {
			status = fmt.Sprintf("MISMATCH %d %v", r.Mismatches, r.Fields)
			errs = append(errs, fmt.Errorf("volume %s: %d entries differ in %v", r.Volume, r.Mismatches, r.Fields))
		}
		fmt.Fprintf(c.Log, "snapshot %s volume %s: %d entries %s %s\n", job.URI, r.Volume, r.Entries, r.Hash, status)
	}
	if len(results) != len(tars) {
		errs = append(errs, fmt.Errorf("restored %d volumes of %d", len(results), len(tars)))
	}
	return errors.Join(errs...)
}

func (c *Converter) removeJobDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(c.Log, "removing %s: %v\n", dir, err)
	}
}

func (c *Converter) jobDir(job Job) (string, error) {
	sum := sha256.Sum256([]byte(job.URI))
	dir := filepath.Join(c.WorkDir, hex.EncodeToString(sum[:8]))
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	return dir, os.MkdirAll(dir, 0o700)
}

// fetchTars downloads and decodes the 1.x volume tars into dir, split per
// volume, and returns them by volume. It refuses a tar of a volume no
// container of the template mounts: its content would be lost.
func (c *Converter) fetchTars(ctx context.Context, uri resources.SnapshotURI, fork *manifest, job Job, dir string) (map[string]string, error) {
	perVolume, legacy, err := fork.durableTars()
	if err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", job.URI, err)
	}
	mounted := map[string]bool{}
	for _, ctr := range job.Containers {
		for _, m := range ctr.Mounts {
			mounted[m.Volume] = true
		}
	}
	tarDir := filepath.Join(dir, "tars")
	if err := os.MkdirAll(tarDir, 0o700); err != nil {
		return nil, err
	}
	fetch := func(name string) (string, error) {
		local := filepath.Join(tarDir, name)
		if err := objectstorage.FetchLocalFileFromGCSWithZstd(ctx, c.Store, objectURI(uri, name+".zstd"), local); err != nil {
			return "", fmt.Errorf("fetching %s: %w", name, err)
		}
		return local, nil
	}
	tars := map[string]string{}
	if legacy {
		all, err := fetch(legacyDurableTar)
		if err != nil {
			return nil, err
		}
		dsts := map[string]string{}
		for vol := range mounted {
			dsts[vol] = filepath.Join(tarDir, durableTarPrefix+vol+durableTarSuffix)
		}
		if err := splitLegacyTar(all, dsts); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", job.URI, err)
		}
		for vol, p := range dsts {
			if _, err := os.Stat(p); err == nil {
				tars[vol] = p
			}
		}
		return tars, os.Remove(all)
	}
	vols := make([]string, 0, len(perVolume))
	for vol := range perVolume {
		vols = append(vols, vol)
	}
	sort.Strings(vols)
	for _, vol := range vols {
		if !mounted[vol] {
			return nil, fmt.Errorf("snapshot %s: volume %q has data but no container of the template mounts it", job.URI, vol)
		}
		if tars[vol], err = fetch(perVolume[vol]); err != nil {
			return nil, err
		}
	}
	return tars, nil
}

// spec is the worker's fsconvert input.
type spec struct {
	Runsc      string            `json:"runsc"`
	Flags      string            `json:"flags"`
	Root       string            `json:"root"`
	Out        string            `json:"out"`
	Containers []Container       `json:"containers"`
	Tars       map[string]string `json:"tars"`
}

type convertResult struct {
	Files []string `json:"files"`
}

type volumeResult struct {
	Volume     string   `json:"volume"`
	Entries    int      `json:"entries"`
	Hash       string   `json:"hash"`
	Mismatches int      `json:"mismatches"`
	Fields     []string `json:"fields"`
}

// runWorker runs `<worker> fsconvert <mode> <spec>` and decodes its result.
func (c *Converter) runWorker(ctx context.Context, mode string, s spec, result any) error {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	specPath := s.Root + ".json"
	if err := os.WriteFile(specPath, raw, 0o600); err != nil {
		return err
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, c.WorkerBinary, "fsconvert", mode, specPath)
	cmd.Stdout = &stdout
	cmd.Stderr = c.Log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fsconvert %s: %w", mode, err)
	}
	if err := json.Unmarshal(stdout.Bytes(), result); err != nil {
		return fmt.Errorf("fsconvert %s: reading its result: %w", mode, err)
	}
	return nil
}

// Plan reports, for each job, its state and what converting it fetches,
// and writes nothing.
func (c *Converter) Plan(ctx context.Context, job Job) error {
	state, fork, err := c.Inspect(ctx, job)
	if err != nil {
		return err
	}
	if state == StateConverted {
		fmt.Fprintf(c.Log, "snapshot %s: converted already\n", job.URI)
		return nil
	}
	perVolume, legacy, err := fork.durableTars()
	if err != nil {
		return err
	}
	mounted := map[string]bool{}
	for _, ctr := range job.Containers {
		for _, m := range ctr.Mounts {
			mounted[m.Volume] = true
		}
	}
	var volumes []string
	for vol := range perVolume {
		if !mounted[vol] {
			return fmt.Errorf("snapshot %s: volume %q has data but no container of the template mounts it", job.URI, vol)
		}
		volumes = append(volumes, vol)
	}
	slices.Sort(volumes)
	fmt.Fprintf(c.Log, "snapshot %s: 1.x scope %q, would convert volumes %v (legacy tar %t) into %d containers\n", job.URI, fork.Scope, volumes, legacy, len(job.Containers))
	return nil
}
