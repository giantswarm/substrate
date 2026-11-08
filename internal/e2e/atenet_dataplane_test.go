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
	"net/http"
	"testing"

	"google.golang.org/grpc/codes"
)

func TestAtenetDataplaneEgressPolicyDenial(t *testing.T) {
	t.Run("envoy", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "")
		if !CurrentAtenetDataplane().IsEgressPolicyDenied(http.StatusBadGateway, "request failed") {
			t.Error("Envoy CONNECT refusal was not recognized as an egress-policy denial")
		}
	})
	t.Run("agentgateway", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "agentgateway")
		if !CurrentAtenetDataplane().IsEgressPolicyDenied(http.StatusForbidden, "actor egress policy denied destination") {
			t.Error("AgentGateway direct policy denial was not recognized")
		}
	})
}

func TestAtenetDataplaneHeldActorGRPCCode(t *testing.T) {
	t.Run("envoy", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "")
		if code, ok := CurrentAtenetDataplane().HeldActorGRPCCode(); !ok || code != codes.Aborted {
			t.Errorf("Envoy HeldActorGRPCCode() = %v, %t; want Aborted, true", code, ok)
		}
	})
	t.Run("agentgateway", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "agentgateway")
		if _, ok := CurrentAtenetDataplane().HeldActorGRPCCode(); ok {
			t.Error("AgentGateway HeldActorGRPCCode() is asserted, want it skipped until it carries the resume's status")
		}
	})
}

func TestAtenetDataplaneTCPRefusal(t *testing.T) {
	const envoy400 = "HTTP/1.1 400 Bad Request\r\ncontent-length: 11\r\ncontent-type: text/plain\r\nserver: envoy\r\nconnection: close\r\n\r\nBad Request"
	t.Run("envoy", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "")
		dataplane := CurrentAtenetDataplane()
		for _, received := range []string{"", envoy400} {
			if !dataplane.IsTCPRefusal(received) {
				t.Errorf("Envoy IsTCPRefusal(%q) = false, want true", received)
			}
		}
		if dataplane.IsTCPRefusal("echo: SSH-2.0-actor\r\n") {
			t.Error("Envoy IsTCPRefusal took the origin's echo for a refusal")
		}
	})
	t.Run("agentgateway", func(t *testing.T) {
		t.Setenv(AtenetDataplaneEnv, "agentgateway")
		dataplane := CurrentAtenetDataplane()
		if !dataplane.IsTCPRefusal("") || dataplane.IsTCPRefusal(envoy400) {
			t.Error("AgentGateway IsTCPRefusal accepts anything but a stream closed with nothing")
		}
	})
}
