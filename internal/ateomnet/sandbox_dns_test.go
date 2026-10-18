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

package ateomnet

import "testing"

func TestDNSQueriesWithoutARelay(t *testing.T) {
	var none *SandboxSession
	if n := none.DNSQueries(); n != 0 {
		t.Errorf("nil session counted %d queries, want 0", n)
	}
	if n := (&SandboxSession{}).DNSQueries(); n != 0 {
		t.Errorf("session without DNS counted %d queries, want 0", n)
	}
}
