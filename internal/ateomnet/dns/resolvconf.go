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

// Package dns answers an actor's DNS from its sandbox's gateway namespace and
// writes the resolv.conf that points the actor at it and the hosts file that
// names the actor itself.
package dns

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// resolvConfNameservers reads nameservers from resolv.conf as "host:53".
func resolvConfNameservers(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dns: reading resolv.conf: %w", err)
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.IndexAny(line, "#;"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		rest, ok := strings.CutPrefix(line, "nameserver")
		if !ok {
			continue
		}
		address := strings.TrimSpace(rest)
		if address == "" || net.ParseIP(address) == nil {
			continue
		}
		out = append(out, net.JoinHostPort(address, "53"))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("dns: reading resolv.conf: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("dns: %s names no usable nameserver", path)
	}
	return out, nil
}

// SandboxResolvConf replaces the pod's nameservers with nameserver, the
// address the sandbox's DNS is served on, while preserving the pod's options
// and the search domains of Kubernetes DNS.
//
// The search domains kubelet appends from the node's own resolv.conf are
// dropped. Under ndots:5 every cluster name is first tried under each of them,
// a query cluster DNS can only forward to the node's resolvers. Stub resolvers
// such as c-ares and musl end the whole lookup when one search query times out
// or fails, so a hiccup of the node's resolvers failed the sandbox's lookup of
// a cluster Service name; and the node's network is not the sandbox's.
func SandboxResolvConf(nameserver string, podResolvConf []byte) []byte {
	var out strings.Builder
	out.WriteString("nameserver " + nameserver + "\n")
	for line := range strings.SplitSeq(string(podResolvConf), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "nameserver" {
			continue
		}
		if fields[0] == "search" {
			line = strings.Join(append([]string{"search"}, clusterSearchDomains(fields[1:])...), " ")
		}
		out.WriteString(line + "\n")
	}
	return []byte(out.String())
}

// clusterSearchDomains keeps the search domains at or under the cluster
// domain, which kubelet lists together with its "svc." subdomain. A list
// without that pair was not written by kubelet's ClusterFirst policy and is
// kept as it is.
func clusterSearchDomains(domains []string) []string {
	var cluster string
	for _, d := range domains {
		if rest, ok := strings.CutPrefix(strings.ToLower(d), "svc."); ok && slices.ContainsFunc(domains, func(o string) bool { return strings.EqualFold(o, rest) }) {
			cluster = rest
			break
		}
	}
	if cluster == "" {
		return domains
	}
	var kept []string
	for _, d := range domains {
		if lower := strings.ToLower(d); lower == cluster || strings.HasSuffix(lower, "."+cluster) {
			kept = append(kept, d)
		}
	}
	return kept
}

// WriteRootfsResolvConf installs content at /etc/resolv.conf inside rootfs.
func WriteRootfsResolvConf(rootfs string, content []byte) error {
	if len(content) == 0 {
		return fmt.Errorf("dns: refusing to write an empty resolv.conf")
	}
	return writeRootfsEtcFile(rootfs, "resolv.conf", content)
}

// writeRootfsEtcFile installs content at /etc/<name> inside rootfs.
//
// os.Root confines path traversal; unlinking prevents writes through existing links.
func writeRootfsEtcFile(rootfs, name string, content []byte) error {
	root, err := os.OpenRoot(rootfs)
	if err != nil {
		return fmt.Errorf("opening rootfs %q: %w", rootfs, err)
	}
	defer root.Close()
	if err := root.Mkdir("etc", 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("creating %q: %w", filepath.Join(rootfs, "etc"), err)
	}
	path := "etc/" + name
	if err := root.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing existing %s: %w", name, err)
	}
	f, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	_, err = f.Write(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}
