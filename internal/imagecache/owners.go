// Copyright 2026 Google LLC
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

// Image-layer ownership.
//
// atelet unpacks layers with every capability dropped, so it cannot chown:
// every entry it writes belongs to root whatever the layer tar says. It
// records the tar's non-root owners in the layer metadata instead, and
// ateom, which can chown, applies them to the layer's tree once per layer
// node-wide (FinalizeLayer). Layers unpacked before owners were recorded are
// backfilled from their tar headers on reuse (backfillLayerOwners).

package imagecache

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// layerMetadataOwnersVersion is the whiteoutSet version from which Owners is
// recorded. Metadata below it says nothing about ownership.
const layerMetadataOwnersVersion = 2

// layerOwner is the non-root owner a layer tar gives one of its entries.
type layerOwner struct {
	Path string `json:"path"`
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
	// Mode is the entry's permission and setuid/setgid bits, recorded for a
	// file that carries setuid or setgid: chown clears both, so they are
	// restored after it.
	Mode os.FileMode `json:"mode,omitempty"`
}

// ownerTracker follows a layer tar's entries with the unpacker's "later
// entry wins" rule: an entry replaces what was at its path, and an entry
// other than a directory replaces a directory's whole subtree.
type ownerTracker struct {
	owners map[string]layerOwner
}

func newOwnerTracker() *ownerTracker {
	return &ownerTracker{owners: map[string]layerOwner{}}
}

// observe records the owner hdr gives the entry at name, a clean path
// relative to the layer root.
func (o *ownerTracker) observe(name string, hdr *tar.Header) {
	if hdr.Typeflag != tar.TypeDir {
		prefix := name + string(filepath.Separator)
		for p := range o.owners {
			if strings.HasPrefix(p, prefix) {
				delete(o.owners, p)
			}
		}
	}
	if hdr.Uid == 0 && hdr.Gid == 0 {
		delete(o.owners, name)
		return
	}
	owner := layerOwner{Path: name, UID: hdr.Uid, GID: hdr.Gid}
	if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeLink {
		if mode := hdr.FileInfo().Mode(); mode&(os.ModeSetuid|os.ModeSetgid) != 0 {
			owner.Mode = mode & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
		}
	}
	o.owners[name] = owner
}

// sorted returns the recorded owners, parents before children.
func (o *ownerTracker) sorted() []layerOwner {
	out := make([]layerOwner, 0, len(o.owners))
	for _, owner := range o.owners {
		out = append(out, owner)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// scanLayerOwners reads a layer tar's headers only and returns the owners
// unpackLayer would record for it.
func scanLayerOwners(tarData io.Reader) ([]layerOwner, error) {
	owners := newOwnerTracker()
	tarReader := tar.NewReader(tarData)
	for {
		hdr, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return owners.sorted(), nil
		} else if err != nil {
			return nil, fmt.Errorf("in tarReader.Next: %w", err)
		}
		name, skip, err := validateTarName(hdr.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid tar entry: %w", err)
		}
		if skip || strings.HasPrefix(filepath.Base(name), whiteoutPrefix) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink, tar.TypeLink:
			owners.observe(name, hdr)
		default:
			return nil, fmt.Errorf("unhandled tar entry typeflag %q", string([]byte{hdr.Typeflag}))
		}
	}
}

// backfillLayerOwners records the owners of a pooled layer whose metadata
// predates owner recording, reading them from the layer's tar headers. The
// metadata is replaced atomically; the layer tree is not touched.
func backfillLayerOwners(ctx context.Context, layerDir string, layer v1.Layer) error {
	meta, err := readWhiteouts(layerDir)
	if err != nil {
		return err
	}
	if meta.Version >= layerMetadataOwnersVersion {
		return nil
	}
	rc, err := layer.Uncompressed()
	if err != nil {
		return fmt.Errorf("while opening layer stream: %w", err)
	}
	defer rc.Close()
	owners, err := scanLayerOwners(rc)
	if err != nil {
		return fmt.Errorf("while reading layer owners: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	meta.Version = layerMetadataOwnersVersion
	meta.Owners = owners
	return writeWhiteouts(layerDir, meta)
}

// writeWhiteouts replaces a layer's metadata through a temporary file, so a
// reader sees the old or the new file, never a partial one.
func writeWhiteouts(layerDir string, meta *whiteoutSet) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("while encoding layer whiteouts: %w", err)
	}
	tmp, err := os.CreateTemp(layerDir, "."+layerWhiteoutsFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("while creating layer whiteouts temp file: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("while writing layer whiteouts: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("while closing layer whiteouts: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(layerDir, layerWhiteoutsFileName)); err != nil {
		return fmt.Errorf("while replacing layer whiteouts: %w", err)
	}
	return nil
}
