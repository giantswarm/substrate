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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"fmt"
	"time"
)

// AddCA generates a CA with the given ID and adds it to the pool without
// making it active for signing. The pool's trust anchors include its root from
// now on.
//
// The new CA's key is also certified by the CA active for signing
// (CA.CrossCertificate), so once Activate switches to it, the certificates it
// signs still verify for a relying party that trusts only the current root —
// one that loaded its anchors before this call and never reloads them. The
// cross-certificate is left out when the active CA has expired: nothing
// verifies against an expired root, so there is no trust left to carry over.
func (p *ConcretePool) AddCA(id string, keyType KeyType, validity time.Duration) (*CA, error) {
	if id == "" {
		return nil, fmt.Errorf("a new CA needs an ID")
	}
	if p.CA(id) != nil {
		return nil, fmt.Errorf("the pool already has a CA %q", id)
	}
	signer, err := p.SigningCA()
	if err != nil {
		return nil, err
	}

	ca, err := GenerateCA(id, keyType, validity)
	if err != nil {
		return nil, err
	}
	if time.Now().Before(signer.RootCertificate.NotAfter) {
		ca.CrossCertificate, err = crossCertify(ca, signer)
		if err != nil {
			return nil, fmt.Errorf("while cross-certifying CA %q under CA %q: %w", id, signer.ID, err)
		}
	}

	p.CAs = append(p.CAs, ca)
	return ca, nil
}

// crossCertify certifies ca's key under issuer's root: a CA certificate with
// ca's subject, key and subject key ID, issued by issuer and valid no longer
// than either root.
func crossCertify(ca, issuer *CA) (*x509.Certificate, error) {
	root := ca.RootCertificate
	notAfter := root.NotAfter
	if issuer.RootCertificate.NotAfter.Before(notAfter) {
		notAfter = issuer.RootCertificate.NotAfter
	}
	template := &x509.Certificate{
		// The subject bytes, not the parsed name: a chain builder matches a
		// leaf's issuer to its parent byte for byte, and the leaves ca signs
		// name its root's subject as it was encoded there.
		RawSubject:            root.RawSubject,
		SubjectKeyId:          root.SubjectKeyId,
		NotBefore:             root.NotBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              root.KeyUsage,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer.RootCertificate, root.PublicKey, issuer.SigningKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

// Activate makes the CA with the given ID the one CreateCertificate signs
// with, recording now as the time it became active. It reports whether that
// changed anything: activating the CA that signs already is a no-op.
//
// It refuses a CA whose cross-certificate was not issued by the CA signing now
// (see AddCA): without it, every relying party that has not reloaded its
// anchors since the CA was added would reject the certificates it signs. The
// one exception is a signing CA that has expired, which nothing verifies any
// more.
func (p *ConcretePool) Activate(id string, now time.Time) (bool, error) {
	ca := p.CA(id)
	if ca == nil {
		return false, fmt.Errorf("the pool has no CA %q", id)
	}
	signer, err := p.SigningCA()
	if err != nil {
		return false, err
	}
	if signer == ca {
		return false, nil
	}

	if now.Before(signer.RootCertificate.NotAfter) {
		if ca.CrossCertificate == nil || ca.CrossCertificate.CheckSignatureFrom(signer.RootCertificate) != nil {
			return false, fmt.Errorf("CA %q is not cross-certified by CA %q, which signs now: "+
				"relying parties that trust only the root of %q would reject what %q signs; "+
				"add a new CA while %q is active and activate that one instead",
				id, signer.ID, signer.ID, id, signer.ID)
		}
	}

	p.ActiveForSigning = id
	p.ActivatedAt = now
	return true, nil
}

// Retire removes the CA with the given ID from the pool, and with it its root
// from the pool's trust anchors.
//
// It refuses the CA active for signing, and it refuses while that CA has been
// active for less than minSigningAge: until then, a server that loads the pool
// from a file may still sign with the CA being retired, and certificates it
// signed earlier may still be in use, while relying parties that load the
// anchors afterwards no longer trust it. A pool that does not record when its
// signing CA became active has signed with it since before any activation was
// recorded, which satisfies any minimum.
func (p *ConcretePool) Retire(id string, now time.Time, minSigningAge time.Duration) error {
	ca := p.CA(id)
	if ca == nil {
		return fmt.Errorf("the pool has no CA %q", id)
	}
	signer, err := p.SigningCA()
	if err != nil {
		return err
	}
	if signer == ca {
		return fmt.Errorf("CA %q is active for signing; activate another CA before retiring it", id)
	}
	if !p.ActivatedAt.IsZero() {
		if age := now.Sub(p.ActivatedAt); age < minSigningAge {
			return fmt.Errorf("CA %q has been active for signing for %s, less than the %s before CA %q may be retired "+
				"(retry after %s)", signer.ID, age.Round(time.Second), minSigningAge, id,
				p.ActivatedAt.Add(minSigningAge).UTC().Format(time.RFC3339))
		}
	}

	var kept []*CA
	for _, c := range p.CAs {
		if c != ca {
			kept = append(kept, c)
		}
	}
	p.CAs = kept
	return nil
}

// KeyType returns the type of the CA's signing key.
func (ca *CA) KeyType() (KeyType, error) {
	switch key := ca.SigningKey.(type) {
	case ed25519.PrivateKey:
		return KeyTypeED25519, nil
	case *ecdsa.PrivateKey:
		if key.Curve == elliptic.P256() {
			return KeyTypeECDSAP256, nil
		}
		return 0, fmt.Errorf("unsupported ECDSA curve %s", key.Curve.Params().Name)
	default:
		return 0, fmt.Errorf("unsupported signing key type %T", ca.SigningKey)
	}
}
