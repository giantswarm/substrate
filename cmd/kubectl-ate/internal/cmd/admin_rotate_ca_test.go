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

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/agent-substrate/substrate/internal/localca"
	"github.com/agent-substrate/substrate/internal/localca/poolsecret"
)

func testCAPool(t *testing.T, keyType localca.KeyType) caPool {
	t.Helper()
	ca, err := localca.GenerateCA("1", keyType, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	data, err := poolsecret.Data(&localca.ConcretePool{CAs: []*localca.CA{ca}, ActiveForSigning: "1"})
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ate-system", Name: "egress-mitm-ca-pool"},
		Type:       corev1.SecretTypeTLS,
		Data:       data,
	})
	return caPool{secrets: client.CoreV1().Secrets("ate-system"), namespace: "ate-system", name: "egress-mitm-ca-pool"}
}

func TestCARotationCommands(t *testing.T) {
	ctx := context.Background()
	target := testCAPool(t, localca.KeyTypeECDSAP256)
	var out bytes.Buffer

	if err := runAddCA(ctx, &out, target, "2", nil, 365*24*time.Hour); err != nil {
		t.Fatalf("add-ca: %v", err)
	}
	pool, err := poolsecret.Get(ctx, target.secrets, target.name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if kt, _ := pool.CA("2").KeyType(); kt != localca.KeyTypeECDSAP256 {
		t.Errorf("add-ca without --key-type chose %v, want the signing CA's ECDSAP256", kt)
	}
	for _, want := range []string{`Added CA "2" to ate-system/egress-mitm-ca-pool`, "activate-ca --ca-id=2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("add-ca output lacks %q:\n%s", want, out.String())
		}
	}

	activated := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	out.Reset()
	if err := runActivateCA(ctx, &out, target, "2", activated); err != nil {
		t.Fatalf("activate-ca: %v", err)
	}
	if want := "Retire the previous CA with retire-ca after 2026-09-24T10:20:00Z"; !strings.Contains(out.String(), want) {
		t.Errorf("activate-ca output lacks %q:\n%s", want, out.String())
	}
	if !strings.Contains(out.String(), "yes, for 0s") {
		t.Errorf("activate-ca does not show CA 2 signing:\n%s", out.String())
	}

	out.Reset()
	if err := runActivateCA(ctx, &out, target, "2", activated.Add(time.Minute)); err != nil {
		t.Fatalf("activate-ca again: %v", err)
	}
	if !strings.Contains(out.String(), "nothing changed") {
		t.Errorf("a repeated activate-ca does not say it changed nothing:\n%s", out.String())
	}

	if err := runRetireCA(ctx, &out, target, "1", activated.Add(5*time.Minute), defaultMinSigningAge); err == nil {
		t.Fatalf("retire-ca five minutes after the activation succeeded")
	}
	out.Reset()
	if err := runRetireCA(ctx, &out, target, "1", activated.Add(defaultMinSigningAge), defaultMinSigningAge); err != nil {
		t.Fatalf("retire-ca: %v", err)
	}
	if !strings.Contains(out.String(), "a retired CA") {
		t.Errorf("retire-ca does not show CA 2's cross-certificate issuer as retired:\n%s", out.String())
	}
	pool, err = poolsecret.Get(ctx, target.secrets, target.name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(pool.CAs) != 1 || pool.CAs[0].ID != "2" {
		t.Errorf("after the rotation the pool holds %d CAs, want only 2", len(pool.CAs))
	}
}

func TestParseKeyType(t *testing.T) {
	for in, want := range map[string]localca.KeyType{"ED25519": localca.KeyTypeED25519, "ECDSAP256": localca.KeyTypeECDSAP256} {
		if got, err := parseKeyType(in); err != nil || got != want {
			t.Errorf("parseKeyType(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := parseKeyType("RSA"); err == nil {
		t.Errorf("parseKeyType(RSA) succeeded")
	}
}
