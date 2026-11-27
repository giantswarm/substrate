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

package snapshot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	// manifestFile is the snapshot's manifest, uploaded last.
	manifestFile = "manifest.json"
	// forkManifestFile keeps the 1.x manifest of a converted snapshot.
	// Copying it back over manifestFile rolls the snapshot back.
	forkManifestFile = "manifest.fork.json"

	legacyDurableTar   = "durable-dir.tar"
	durableTarPrefix   = "durable-dir-"
	durableTarSuffix   = ".tar"
	fidelityVolumes    = "volumes"
	sandboxClassGVisor = "gvisor"
	gvisorAssetName    = "gvisor"
)

// Asset is a sandbox asset a manifest records.
type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// manifest is a snapshot manifest as either release writes it: 1.x records
// scope and pauseImage, 2.x fidelity.
type manifest struct {
	SandboxClass          string           `json:"sandboxClass"`
	Assets                map[string]Asset `json:"assets"`
	PauseImage            string           `json:"pauseImage,omitempty"`
	Atespace              string           `json:"atespace,omitempty"`
	ActorName             string           `json:"actorName,omitempty"`
	ActorUID              string           `json:"actorUid,omitempty"`
	ActorTemplateAtespace string           `json:"actorTemplateAtespace,omitempty"`
	ActorTemplateName     string           `json:"actorTemplateName,omitempty"`
	SnapshotFiles         []string         `json:"snapshotFiles,omitempty"`
	DataSnapshotFiles     []string         `json:"dataSnapshotFiles,omitempty"`
	Scope                 string           `json:"scope,omitempty"`
	Fidelity              string           `json:"fidelity,omitempty"`
	BootID                string           `json:"bootId,omitempty"`
}

func parseManifest(b []byte) (*manifest, error) {
	m := &manifest{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("parsing the manifest: %w", err)
	}
	return m, nil
}

// is2x reports whether a 2.x release, or a conversion, wrote m.
func (m *manifest) is2x() bool { return m.Fidelity != "" }

// durableTars returns the volume tars of a 1.x snapshot: per volume, or the
// one legacy tar of every volume.
func (m *manifest) durableTars() (perVolume map[string]string, legacy bool, err error) {
	if m.SandboxClass != "" && m.SandboxClass != sandboxClassGVisor {
		return nil, false, fmt.Errorf("sandbox class %q: only gVisor snapshots convert", m.SandboxClass)
	}
	files := m.DataSnapshotFiles
	if len(files) == 0 {
		files = m.SnapshotFiles
	}
	perVolume = map[string]string{}
	for _, f := range files {
		switch {
		case f == legacyDurableTar:
			legacy = true
		case strings.HasPrefix(f, durableTarPrefix) && strings.HasSuffix(f, durableTarSuffix):
			vol := strings.TrimSuffix(strings.TrimPrefix(f, durableTarPrefix), durableTarSuffix)
			if vol == "" || strings.Contains(vol, "/") {
				return nil, false, fmt.Errorf("data file %q names no volume", f)
			}
			perVolume[vol] = f
		}
	}
	if legacy && len(perVolume) > 0 {
		return nil, false, fmt.Errorf("both %s and per-volume tars", legacyDurableTar)
	}
	return perVolume, legacy, nil
}

// convertedManifest is the 2.x manifest of a converted 1.x snapshot: a
// VOLUMES capture made of files, at the asset given.
func convertedManifest(fork *manifest, files []string, asset Asset) *manifest {
	files = slices.Clone(files)
	return &manifest{
		SandboxClass:          sandboxClassGVisor,
		Assets:                map[string]Asset{gvisorAssetName: asset},
		Atespace:              fork.Atespace,
		ActorName:             fork.ActorName,
		ActorUID:              fork.ActorUID,
		ActorTemplateAtespace: fork.ActorTemplateAtespace,
		ActorTemplateName:     fork.ActorTemplateName,
		SnapshotFiles:         files,
		DataSnapshotFiles:     files,
		Fidelity:              fidelityVolumes,
	}
}
