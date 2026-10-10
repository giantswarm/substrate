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

import "fmt"

// SandboxHosts is the actor's /etc/hosts: loopback plus its own hostname on
// 127.0.1.1, the way a pod's /etc/hosts names the pod. Without the hostname
// line every lookup of it (hostname -f, a runtime resolving itself at start)
// leaves the sandbox and fails upstream. 127.0.1.1 is a loopback address that
// stays the same across snapshot and restore, unlike the sandbox's interface
// address.
func SandboxHosts(hostname string) []byte {
	return fmt.Appendf(nil, "127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\n127.0.1.1\t%s\n", hostname)
}

// WriteRootfsHosts installs SandboxHosts(hostname) at /etc/hosts inside
// rootfs, replacing the image's own file.
func WriteRootfsHosts(rootfs, hostname string) error {
	return writeRootfsEtcFile(rootfs, "hosts", SandboxHosts(hostname))
}
