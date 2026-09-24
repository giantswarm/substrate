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

package localca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

// relyingParty verifies server certificates against a fixed set of roots,
// like a process that loaded its trust anchors once.
type relyingParty struct {
	name  string
	roots *x509.CertPool
}

func trusting(name string, cas ...*CA) relyingParty {
	roots := x509.NewCertPool()
	for _, ca := range cas {
		roots.AddCert(ca.RootCertificate)
	}
	return relyingParty{name: name, roots: roots}
}

// serve signs a server leaf from pool and returns the chain a TLS server
// presents with it.
func serve(t *testing.T, pool *ConcretePool) []*x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating leaf key: %v", err)
	}
	ders, err := pool.CreateCertificate(&x509.Certificate{
		DNSNames:    []string{"example.com"},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, key.Public())
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	var chain []*x509.Certificate
	for _, der := range ders {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatalf("parsing chain: %v", err)
		}
		chain = append(chain, cert)
	}
	return chain
}

func (rp relyingParty) verify(chain []*x509.Certificate) error {
	intermediates := x509.NewCertPool()
	for _, cert := range chain[1:] {
		intermediates.AddCert(cert)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{
		DNSName:       "example.com",
		Roots:         rp.roots,
		Intermediates: intermediates,
	})
	return err
}

func newPool(t *testing.T, id string, keyType KeyType) *ConcretePool {
	t.Helper()
	ca, err := GenerateCA(id, keyType, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	return &ConcretePool{CAs: []*CA{ca}, ActiveForSigning: id}
}

// TestRotationKeepsRelyingPartiesVerifying walks a pool from CA "a" to CA "b"
// and checks, after every step, which relying parties verify what the pool
// signs: one that loaded the anchors before the rotation began (only "a"), one
// that loaded them while both roots were published, and one that loaded them
// after "a" was retired (only "b").
func TestRotationKeepsRelyingPartiesVerifying(t *testing.T) {
	for _, tc := range []struct {
		name     string
		old, new KeyType
	}{
		{"ED25519 to ED25519", KeyTypeED25519, KeyTypeED25519},
		{"ECDSAP256 to ECDSAP256", KeyTypeECDSAP256, KeyTypeECDSAP256},
		{"ED25519 to ECDSAP256", KeyTypeED25519, KeyTypeECDSAP256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newPool(t, "a", tc.old)
			a := pool.CAs[0]
			b, err := pool.AddCA("b", tc.new, 365*24*time.Hour)
			if err != nil {
				t.Fatalf("AddCA: %v", err)
			}
			onlyA, both, onlyB := trusting("only a", a), trusting("a and b", a, b), trusting("only b", b)

			check := func(step string, want map[string]bool) {
				t.Helper()
				chain := serve(t, pool)
				for _, rp := range []relyingParty{onlyA, both, onlyB} {
					err := rp.verify(chain)
					if got := err == nil; got != want[rp.name] {
						t.Errorf("%s: relying party trusting %s verified = %t (err %v), want %t", step, rp.name, got, err, want[rp.name])
					}
				}
			}

			check("b added", map[string]bool{"only a": true, "a and b": true, "only b": false})
			if got := anchorIDs(t, pool); got != "a,b" {
				t.Errorf("after AddCA, trust anchors = %s, want a,b", got)
			}

			changed, err := pool.Activate("b", time.Now())
			if err != nil || !changed {
				t.Fatalf("Activate(b) = %t, %v; want true, nil", changed, err)
			}
			check("b active", map[string]bool{"only a": true, "a and b": true, "only b": true})

			if err := pool.Retire("a", time.Now().Add(time.Hour), 30*time.Minute); err != nil {
				t.Fatalf("Retire(a): %v", err)
			}
			check("a retired", map[string]bool{"only a": true, "a and b": true, "only b": true})
			if got := anchorIDs(t, pool); got != "b" {
				t.Errorf("after Retire, trust anchors = %s, want b", got)
			}
		})
	}
}

// TestActivateRefusesACAWithoutCrossCertificate shows why Activate insists on
// the cross-certificate: a CA added without one signs leaves that a relying
// party still trusting only the old root rejects.
func TestActivateRefusesACAWithoutCrossCertificate(t *testing.T) {
	pool := newPool(t, "a", KeyTypeECDSAP256)
	a := pool.CAs[0]
	b, err := GenerateCA("b", KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	pool.CAs = append(pool.CAs, b)

	_, err = pool.Activate("b", time.Now())
	if err == nil || !strings.Contains(err.Error(), `CA "b" is not cross-certified by CA "a"`) {
		t.Fatalf("Activate(b) error = %v, want a refusal naming the missing cross-certificate", err)
	}
	if pool.ActiveForSigning != "a" {
		t.Errorf("ActiveForSigning = %q after a refused activation, want a", pool.ActiveForSigning)
	}

	// What the refusal prevents.
	unsafe := &ConcretePool{CAs: pool.CAs, ActiveForSigning: "b"}
	err = trusting("only a", a).verify(serve(t, unsafe))
	if _, ok := err.(x509.UnknownAuthorityError); !ok {
		t.Errorf("a leaf of b without cross-certificate, verified against a: err = %v, want x509.UnknownAuthorityError", err)
	}
}

func TestActivateRefusesACrossCertificateFromAnotherCA(t *testing.T) {
	pool := newPool(t, "a", KeyTypeECDSAP256)
	for _, id := range []string{"b", "c"} {
		if _, err := pool.AddCA(id, KeyTypeECDSAP256, 365*24*time.Hour); err != nil {
			t.Fatalf("AddCA(%s): %v", id, err)
		}
	}
	if _, err := pool.Activate("b", time.Now()); err != nil {
		t.Fatalf("Activate(b): %v", err)
	}
	// c was cross-certified by a, but b signs now.
	if _, err := pool.Activate("c", time.Now()); err == nil || !strings.Contains(err.Error(), `not cross-certified by CA "b"`) {
		t.Fatalf("Activate(c) error = %v, want a refusal naming b", err)
	}
}

func TestActivateTheSigningCAIsANoOp(t *testing.T) {
	for name, pool := range map[string]*ConcretePool{
		"named":   newPool(t, "a", KeyTypeED25519),
		"implied": {CAs: newPool(t, "a", KeyTypeED25519).CAs},
	} {
		t.Run(name, func(t *testing.T) {
			before := pool.ActiveForSigning
			changed, err := pool.Activate("a", time.Now())
			if err != nil || changed {
				t.Fatalf("Activate(a) = %t, %v; want false, nil", changed, err)
			}
			if pool.ActiveForSigning != before || !pool.ActivatedAt.IsZero() {
				t.Errorf("a no-op activation changed the pool: ActiveForSigning %q, ActivatedAt %v", pool.ActiveForSigning, pool.ActivatedAt)
			}
		})
	}
}

func TestActivateUnknownCA(t *testing.T) {
	pool := newPool(t, "a", KeyTypeED25519)
	if _, err := pool.Activate("nope", time.Now()); err == nil || !strings.Contains(err.Error(), `no CA "nope"`) {
		t.Fatalf("Activate(nope) error = %v, want no such CA", err)
	}
}

// TestRotationAfterTheSigningCAExpired covers the recovery from a CA nobody
// rotated in time: there is no trust in the expired root left to preserve, so
// the new CA is added without cross-certificate and may be activated anyway.
func TestRotationAfterTheSigningCAExpired(t *testing.T) {
	// A validity of a nanosecond encodes as a NotAfter at or before the
	// current second: the root has expired by the time AddCA looks at it.
	expired, err := GenerateCA("a", KeyTypeECDSAP256, time.Nanosecond)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	pool := &ConcretePool{CAs: []*CA{expired}, ActiveForSigning: "a"}
	b, err := pool.AddCA("b", KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("AddCA: %v", err)
	}
	if b.CrossCertificate != nil {
		t.Errorf("AddCA cross-certified b under an expired root")
	}
	if _, err := pool.Activate("b", time.Now()); err != nil {
		t.Fatalf("Activate(b) with an expired signing CA: %v", err)
	}
}

func TestAddCARefusals(t *testing.T) {
	pool := newPool(t, "a", KeyTypeED25519)
	if _, err := pool.AddCA("a", KeyTypeED25519, time.Hour); err == nil || !strings.Contains(err.Error(), `already has a CA "a"`) {
		t.Errorf("AddCA(a) error = %v, want a duplicate ID refusal", err)
	}
	if _, err := pool.AddCA("", KeyTypeED25519, time.Hour); err == nil {
		t.Errorf("AddCA with an empty ID succeeded")
	}
	if len(pool.CAs) != 1 {
		t.Errorf("refused additions left %d CAs in the pool, want 1", len(pool.CAs))
	}
}

func TestCrossCertificate(t *testing.T) {
	pool := newPool(t, "a", KeyTypeED25519)
	a := pool.CAs[0]
	b, err := pool.AddCA("b", KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("AddCA: %v", err)
	}
	cross, root := b.CrossCertificate, b.RootCertificate
	if cross == nil {
		t.Fatal("AddCA left the new CA without cross-certificate")
	}
	if err := cross.CheckSignatureFrom(a.RootCertificate); err != nil {
		t.Errorf("cross-certificate is not signed by a: %v", err)
	}
	if !bytes.Equal(cross.RawSubject, root.RawSubject) || !bytes.Equal(cross.RawIssuer, a.RootCertificate.RawSubject) {
		t.Errorf("cross-certificate names subject %q issuer %q, want %q issued by %q", cross.Subject, cross.Issuer, root.Subject, a.RootCertificate.Subject)
	}
	if !bytes.Equal(cross.SubjectKeyId, root.SubjectKeyId) || !bytes.Equal(cross.AuthorityKeyId, a.RootCertificate.SubjectKeyId) {
		t.Errorf("cross-certificate key IDs: subject %x authority %x, want %x and %x", cross.SubjectKeyId, cross.AuthorityKeyId, root.SubjectKeyId, a.RootCertificate.SubjectKeyId)
	}
	if !cross.IsCA || cross.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("cross-certificate IsCA %t, KeyUsage %v: cannot certify leaves", cross.IsCA, cross.KeyUsage)
	}
	if cross.NotAfter.After(a.RootCertificate.NotAfter) || cross.NotAfter.After(root.NotAfter) {
		t.Errorf("cross-certificate NotAfter %v outlives a root (a %v, b %v)", cross.NotAfter, a.RootCertificate.NotAfter, root.NotAfter)
	}

	// What a TLS server that mints from b's key (tls.crt) presents.
	chainPEM, err := b.TLSCertificateChainPEM()
	if err != nil {
		t.Fatalf("TLSCertificateChainPEM: %v", err)
	}
	if block, _ := pem.Decode(chainPEM); block == nil || !bytes.Equal(block.Bytes, cross.Raw) {
		t.Errorf("TLSCertificateChainPEM does not present the cross-certificate")
	}
}

func TestRetire(t *testing.T) {
	activated := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	rotated := func(t *testing.T) *ConcretePool {
		pool := newPool(t, "a", KeyTypeED25519)
		if _, err := pool.AddCA("b", KeyTypeED25519, 365*24*time.Hour); err != nil {
			t.Fatalf("AddCA: %v", err)
		}
		if _, err := pool.Activate("b", activated); err != nil {
			t.Fatalf("Activate: %v", err)
		}
		return pool
	}

	t.Run("the signing CA", func(t *testing.T) {
		err := rotated(t).Retire("b", activated.Add(time.Hour), 0)
		if err == nil || !strings.Contains(err.Error(), "active for signing") {
			t.Errorf("Retire(b) error = %v, want a refusal of the signing CA", err)
		}
	})
	t.Run("too soon after the activation", func(t *testing.T) {
		pool := rotated(t)
		err := pool.Retire("a", activated.Add(10*time.Minute), 20*time.Minute)
		if err == nil || !strings.Contains(err.Error(), "retry after 2026-09-24T10:20:00Z") {
			t.Errorf("Retire(a) error = %v, want a refusal with the time it becomes possible", err)
		}
		if len(pool.CAs) != 2 {
			t.Errorf("a refused retirement left %d CAs, want 2", len(pool.CAs))
		}
	})
	t.Run("after the activation", func(t *testing.T) {
		pool := rotated(t)
		if err := pool.Retire("a", activated.Add(20*time.Minute), 20*time.Minute); err != nil {
			t.Fatalf("Retire(a): %v", err)
		}
		if got := anchorIDs(t, pool); got != "b" {
			t.Errorf("trust anchors = %s, want b", got)
		}
	})
	t.Run("a pool without activation time", func(t *testing.T) {
		pool := newPool(t, "a", KeyTypeED25519)
		if _, err := pool.AddCA("b", KeyTypeED25519, time.Hour); err != nil {
			t.Fatalf("AddCA: %v", err)
		}
		if err := pool.Retire("b", time.Now(), 20*time.Minute); err != nil {
			t.Errorf("Retire(b) of a CA that never signed, in a pool without activation time: %v", err)
		}
	})
	t.Run("an unknown CA", func(t *testing.T) {
		if err := rotated(t).Retire("nope", activated.Add(time.Hour), 0); err == nil {
			t.Errorf("Retire(nope) succeeded")
		}
	})
}

func TestMarshalKeepsRotationState(t *testing.T) {
	pool := newPool(t, "a", KeyTypeECDSAP256)
	if _, err := pool.AddCA("b", KeyTypeED25519, 365*24*time.Hour); err != nil {
		t.Fatalf("AddCA: %v", err)
	}
	activated := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	if _, err := pool.Activate("b", activated); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	wire, err := Marshal(pool)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ActiveForSigning != "b" || !got.ActivatedAt.Equal(activated) {
		t.Errorf("round trip: ActiveForSigning %q ActivatedAt %v, want b and %v", got.ActiveForSigning, got.ActivatedAt, activated)
	}
	if got.CA("a").CrossCertificate != nil {
		t.Errorf("round trip gave a a cross-certificate")
	}
	if cross := got.CA("b").CrossCertificate; cross == nil || !bytes.Equal(cross.Raw, pool.CA("b").CrossCertificate.Raw) {
		t.Errorf("round trip lost b's cross-certificate")
	}
}

// TestUnmarshalPoolWithoutRotationState reads a pool as written before the
// cross-certificate and the activation time existed, and checks that such a
// pool is written back in the same shape.
func TestUnmarshalPoolWithoutRotationState(t *testing.T) {
	pool := newPool(t, "a", KeyTypeED25519)
	wire, err := Marshal(pool)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if _, ok := fields["ActivatedAt"]; ok {
		t.Errorf("a pool without activation time marshals an ActivatedAt field: %s", wire)
	}
	if strings.Contains(string(wire), "CrossCertificateDER") {
		t.Errorf("a CA without cross-certificate marshals a CrossCertificateDER field: %s", wire)
	}

	got, err := Unmarshal(wire)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.ActivatedAt.IsZero() || got.CAs[0].CrossCertificate != nil {
		t.Errorf("Unmarshal invented rotation state: ActivatedAt %v, cross-certificate %v", got.ActivatedAt, got.CAs[0].CrossCertificate)
	}
}

func TestCAKeyType(t *testing.T) {
	for _, want := range []KeyType{KeyTypeED25519, KeyTypeECDSAP256} {
		ca, err := GenerateCA("a", want, time.Hour)
		if err != nil {
			t.Fatalf("GenerateCA: %v", err)
		}
		if got, err := ca.KeyType(); err != nil || got != want {
			t.Errorf("KeyType() = %v, %v; want %v", got, err, want)
		}
	}
}

// anchorIDs names the CAs whose roots are the pool's trust anchors.
func anchorIDs(t *testing.T, pool *ConcretePool) string {
	t.Helper()
	anchors, err := pool.TrustAnchors()
	if err != nil {
		t.Fatalf("TrustAnchors: %v", err)
	}
	var ids []string
	for _, anchor := range anchors {
		for _, ca := range pool.CAs {
			if ca.RootCertificate.Equal(anchor) {
				ids = append(ids, ca.ID)
			}
		}
	}
	return strings.Join(ids, ",")
}
