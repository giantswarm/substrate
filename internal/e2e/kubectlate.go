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

package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// KubectlAte runs this tree's kubectl-ate (`go run ./cmd/kubectl-ate`, so the
// client under test is built from the same source as the control plane it
// talks to) against the suite's cluster, with stdin as the command's standard
// input, and returns its standard output. A non-zero exit is returned as an
// error carrying the command's standard error, so a test can expect a refusal.
func KubectlAte(t *testing.T, stdin io.Reader, args ...string) ([]byte, error) {
	t.Helper()
	root, err := FindRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("kubectl-ate: %w", err)
	}
	cmdArgs := []string{"run", "./cmd/kubectl-ate"}
	if KubeConfig != "" {
		cmdArgs = append(cmdArgs, "--kubeconfig="+KubeConfig)
	}
	if KubeContext != "" {
		cmdArgs = append(cmdArgs, "--context="+KubeContext)
	}
	cmdArgs = append(cmdArgs, args...)
	t.Logf("Running command: kubectl-ate %s", strings.Join(args, " "))
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = root
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("kubectl-ate %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
