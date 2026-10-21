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

// Package snapshotlayout numbers the layouts an actor snapshot's files are
// written in. A restore reads a snapshot by its layout, so the control plane
// records the layout a golden snapshot was written in and takes a golden in
// an older layout again once the workers write a newer one.
package snapshotlayout

const (
	// Unrecorded marks a golden snapshot status that records no layout: one
	// written by a release before layouts were recorded, in whatever layout
	// that release wrote.
	Unrecorded int32 = 0

	// SingleDurableTar is the layout before one tar per durable-dir volume:
	// every durable-dir volume in one durable-dir.tar, its entries named
	// <volume>/... . No release recorded it.
	SingleDurableTar int32 = 1

	// PerVolume is the layout of one rootfs-upper tar per container and one
	// durable-dir-<volume>.tar per durable-dir volume, each written and read
	// relative to the snapshot directory.
	PerVolume int32 = 2

	// GVisorNightly20260909 is PerVolume with the checkpoint image written by
	// the gVisor nightly of 2026-09-09. runsc restores only its own build's
	// checkpoint images; a restore runs the build its snapshot recorded, so a
	// golden taken by an older build is taken again to start new actors on the
	// newer one.
	GVisorNightly20260909 int32 = 3

	// Current is the layout the workers write. A change to the files a
	// checkpoint writes, to how a restore reads them, or to the gVisor build the
	// SandboxConfig names adds the next layout above and makes it Current,
	// which has every golden snapshot in an older layout taken again.
	Current = GVisorNightly20260909
)
