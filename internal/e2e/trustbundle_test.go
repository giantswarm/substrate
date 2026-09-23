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
	"encoding/pem"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/localca"
)

func TestSameCertificates(t *testing.T) {
	root := func(id string) string {
		t.Helper()
		ca, err := localca.GenerateCA(id, localca.KeyTypeECDSAP256, time.Hour)
		if err != nil {
			t.Fatalf("generating CA %q: %v", id, err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.RootCertificate.Raw}))
	}
	a, b, c := root("a"), root("b"), root("c")

	for _, tc := range []struct {
		name       string
		got, want  string
		wantResult bool
	}{
		{"same order", a + b, a + b, true},
		{"shuffled", b + a, a + b, true},
		{"a root missing", a, a + b, false},
		{"an extra root", a + b + c, a + b, false},
		{"another root", a + c, a + b, false},
		{"nothing projected", "", a, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameCertificates(tc.got, tc.want); got != tc.wantResult {
				t.Errorf("SameCertificates = %v, want %v", got, tc.wantResult)
			}
		})
	}
}
