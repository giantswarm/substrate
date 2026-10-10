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

package dns

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestWriteRootfsHosts(t *testing.T) {
	rootfs := t.TempDir()
	if err := os.Mkdir(filepath.Join(rootfs, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The image's own file is replaced, not appended to.
	if err := os.WriteFile(filepath.Join(rootfs, "etc", "hosts"), []byte("10.0.0.1 image-build-host\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteRootfsHosts(rootfs, "actor"); err != nil {
		t.Fatalf("WriteRootfsHosts: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(rootfs, "etc", "hosts"))
	if err != nil {
		t.Fatalf("reading what was written: %v", err)
	}
	want := "127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\n127.0.1.1\tactor\n"
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("actor /etc/hosts mismatch (-want +got):\n%s", diff)
	}
}

// Like resolv.conf, the hosts file must not be written through a link the
// untrusted image planted.
func TestWriteRootfsHostsDoesNotFollowAPlantedSymlink(t *testing.T) {
	rootfs := t.TempDir()
	outside := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(outside, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(rootfs, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rootfs, "etc", "hosts")); err != nil {
		t.Fatal(err)
	}

	if err := WriteRootfsHosts(rootfs, "actor"); err != nil {
		t.Fatalf("WriteRootfsHosts: %v", err)
	}
	if victim, err := os.ReadFile(outside); err != nil || string(victim) != "original" {
		t.Errorf("the symlink was followed: the file outside the rootfs now holds %q (err %v)", victim, err)
	}
}
