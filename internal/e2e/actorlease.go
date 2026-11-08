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
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/resources"
)

// heldActorLeaseToken marks the lease rows HoldActorLease writes, so its
// release deletes only its own row and never an operation's.
const heldActorLeaseToken = "e2e-held-actor"

// HoldActorLease holds actorRef's lease, as a lifecycle operation of ate-api
// does, for ttl or until the test ends: a resume of the Actor meanwhile is
// refused Aborted ("another operation is in progress"), and the router keeps
// retrying it until its resume budget is spent. It writes the lease row into
// the bundled PostgreSQL of the kind install, which is the only way to keep an
// Actor held on demand for longer than that budget; no Control API call holds
// a lease past its own operation.
//
// The row's key is ate-api's actorLeaseKey (cmd/ateapi/internal/controlapi)
// and its columns those acquireLease writes (cmd/ateapi/internal/store/atepg).
func HoldActorLease(t *testing.T, actorRef resources.ActorRef, ttl time.Duration) {
	t.Helper()
	key := sqlLiteral("lease:actor:" + actorRef.Atespace + ":" + actorRef.Name)
	token := sqlLiteral(heldActorLeaseToken)
	runPostgresSQL(t, fmt.Sprintf(`INSERT INTO leases (key, token, holder, acquired_at, expires_at)
VALUES (%[1]s, %[2]s, 'e2e', clock_timestamp(), clock_timestamp() + make_interval(secs => %[3]d))
ON CONFLICT (key) DO UPDATE SET token = EXCLUDED.token, holder = EXCLUDED.holder,
  acquired_at = EXCLUDED.acquired_at, expires_at = EXCLUDED.expires_at`, key, token, int(ttl.Seconds())))
	t.Cleanup(func() {
		runPostgresSQL(t, fmt.Sprintf(`DELETE FROM leases WHERE key = %s AND token = %s`, key, token))
	})
}

// runPostgresSQL runs one statement against ate-api's database in the bundled
// PostgreSQL StatefulSet and fails the test if it fails.
func runPostgresSQL(t *testing.T, statement string) {
	t.Helper()
	args := []string{
		"exec", "-n", SystemNamespace(), "statefulset/" + ResourceName("postgres"), "-c", "postgres", "--",
		"psql", "-U", "postgres", "-d", "atepg", "-v", "ON_ERROR_STOP=1", "-Atc", statement,
	}
	if KubeContext != "" {
		args = append([]string{"--context=" + KubeContext}, args...)
	}
	if KubeConfig != "" {
		args = append([]string{"--kubeconfig=" + KubeConfig}, args...)
	}
	output, err := exec.Command("kubectl", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("running %q in PostgreSQL: %v: %s", statement, err, output)
	}
}

// sqlLiteral quotes s as an SQL string literal.
func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
