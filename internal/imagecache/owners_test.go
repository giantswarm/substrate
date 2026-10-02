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

package imagecache

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"
)

// ownedLayerEntries is a layer that gives a home directory, its files and a
// setuid tool to uid 1000, and replaces some owned entries with root ones.
func ownedLayerEntries() []tarEntry {
	return []tarEntry{
		{name: "etc/", typeflag: tar.TypeDir},
		{name: "etc/passwd", typeflag: tar.TypeReg, body: "agent:x:1000:1000::/home/agent:/bin/sh\n"},
		{name: "home/agent/", typeflag: tar.TypeDir, mode: 0o700, uid: 1000, gid: 1000},
		{name: "home/agent/.bashrc", typeflag: tar.TypeReg, body: "export A=1\n", uid: 1000, gid: 1000},
		{name: "home/agent/.profile", typeflag: tar.TypeSymlink, linkname: ".bashrc", uid: 1000, gid: 1000},
		{name: "home/agent/tool", typeflag: tar.TypeReg, mode: 0o4755, body: "#!/bin/sh\n", uid: 1000, gid: 100},
		{name: "home/agent/tool-link", typeflag: tar.TypeLink, linkname: "home/agent/tool", mode: 0o4755, uid: 1000, gid: 100},
		// A directory replaced by a root file takes its subtree's owners with it.
		{name: "cache/", typeflag: tar.TypeDir, uid: 1000, gid: 1000},
		{name: "cache/entry", typeflag: tar.TypeReg, body: "x", uid: 1000, gid: 1000},
		{name: "cache", typeflag: tar.TypeReg, body: "now a file"},
		// A later root entry for the same path wins.
		{name: "data", typeflag: tar.TypeReg, body: "first", uid: 1000, gid: 1000},
		{name: "data", typeflag: tar.TypeReg, body: "second"},
		// Whiteouts carry no owner of their own.
		{name: "home/.wh.old", typeflag: tar.TypeReg, uid: 1000, gid: 1000},
	}
}

func wantOwnedLayerOwners() []layerOwner {
	return []layerOwner{
		{Path: "home/agent", UID: 1000, GID: 1000},
		{Path: "home/agent/.bashrc", UID: 1000, GID: 1000},
		{Path: "home/agent/.profile", UID: 1000, GID: 1000},
		{Path: "home/agent/tool", UID: 1000, GID: 100, Mode: os.ModeSetuid | 0o755},
		{Path: "home/agent/tool-link", UID: 1000, GID: 100, Mode: os.ModeSetuid | 0o755},
	}
}

func TestUnpackLayer_RecordsNonRootOwners(t *testing.T) {
	_, wh, err := runUnpack(t, ownedLayerEntries())
	if err != nil {
		t.Fatalf("unpackLayer: %v", err)
	}
	if wh.Version != layerMetadataOwnersVersion {
		t.Errorf("metadata version = %d, want %d", wh.Version, layerMetadataOwnersVersion)
	}
	if want := wantOwnedLayerOwners(); !reflect.DeepEqual(wh.Owners, want) {
		t.Errorf("owners =\n%+v\nwant\n%+v", wh.Owners, want)
	}
}

func TestScanLayerOwners_MatchesUnpack(t *testing.T) {
	_, wh, err := runUnpack(t, ownedLayerEntries())
	if err != nil {
		t.Fatalf("unpackLayer: %v", err)
	}
	scanned, err := scanLayerOwners(bytes.NewReader(buildTar(t, ownedLayerEntries())))
	if err != nil {
		t.Fatalf("scanLayerOwners: %v", err)
	}
	if !reflect.DeepEqual(scanned, wh.Owners) {
		t.Errorf("scanned owners =\n%+v\nunpacked\n%+v", scanned, wh.Owners)
	}
}

// A layer pooled before owners were recorded gets them from its tar headers
// the next time a pull reuses it; its whiteout state is kept.
func TestEnsureLayer_BackfillsOwnersOfAnOlderLayer(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	layer := layerFromEntries(t, ownedLayerEntries())
	diffID, err := layer.DiffID()
	if err != nil {
		t.Fatalf("DiffID: %v", err)
	}
	dir := s.layerDir(diffID)
	writeLayer(t, dir, map[string]string{"etc/passwd": "root:x:0:0::/root:/bin/sh\n"}, &whiteoutSet{Whiteouts: []string{"home/old"}})

	if _, err := s.ensureLayer(context.Background(), diffID, layer); err != nil {
		t.Fatalf("ensureLayer: %v", err)
	}
	wh, err := readWhiteouts(dir)
	if err != nil {
		t.Fatalf("readWhiteouts: %v", err)
	}
	if wh.Version != layerMetadataOwnersVersion {
		t.Errorf("metadata version = %d, want %d", wh.Version, layerMetadataOwnersVersion)
	}
	if want := wantOwnedLayerOwners(); !reflect.DeepEqual(wh.Owners, want) {
		t.Errorf("owners =\n%+v\nwant\n%+v", wh.Owners, want)
	}
	if want := []string{"home/old"}; !reflect.DeepEqual(wh.Whiteouts, want) {
		t.Errorf("whiteouts = %v, want %v", wh.Whiteouts, want)
	}
}
