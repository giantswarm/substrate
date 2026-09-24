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
	"context"
	"encoding/pem"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/localca"
	"github.com/agent-substrate/substrate/internal/localca/poolsecret"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Constants of atecontroller's EgressMITMTrustReconciler (#946): the CA pool
// Secret it watches (the key is what `kubectl-ate admin make-ca-pool` writes)
// and the ClusterTrustBundle it derives from that pool — the backing object
// of the allowlisted "egress-mitm.ate.dev" bundle the probe fixture projects.
//
// Suites provision the POOL and let the real reconciler publish the bundle,
// exercising the whole chain (pool -> reconciler -> bundle -> projection).
// Writing the bundle directly is not an option: the reconciler watches it
// and reverts or deletes hand-written contents.
const (
	// EgressTrustBundleObjectName is the reconciler-owned ClusterTrustBundle.
	EgressTrustBundleObjectName = "egress-mitm.ate.dev:mitm:primary-bundle"

	egressCAPoolNamespace  = "ate-system"
	egressCAPoolSecretName = "egress-mitm-ca-pool"
	egressCAPoolSecretKey  = poolsecret.PoolKey
)

// EnsureEgressTrustBundle makes sure the egress trust bundle exists, then
// waits until the reconciler-published bundle is non-empty. It provisions a
// pool only when there is none and never replaces one it finds: the pool is
// cluster-wide, and the sdsmint gateway mounts the one the install created.
// A suite that needs to OWN the bundle's contents (the identity suite's
// deterministic assertions and rotation) uses RotateEgressTrustPool.
func EnsureEgressTrustBundle(t *testing.T, ctx context.Context, clients *Clients) {
	t.Helper()
	createEgressTrustPool(t, ctx, clients, newEgressTrustPool(t))
	waitForEgressTrustBundle(t, ctx, clients, "")
}

// rotatedCAIDPrefix marks the CAs RotateEgressTrustPool adds, the only ones it
// ever removes again.
const rotatedCAIDPrefix = "e2e-rotation-"

// RotateEgressTrustPool changes the egress trust bundle without changing the
// CA the MITM gateway signs with: it adds a fresh CA (ID cn, which keeps
// successive bundles distinguishable in failure output) to the pool, dropping
// only the one an earlier call added, waits for the reconciler to publish the
// derived bundle, and returns its PEM — the anchors a trustBundle projection
// must then deliver, in any order (see SameCertificates). The pool the call
// found is put back when the test ends; one it had to create is deleted.
//
// A rotation never removes a CA it did not add because the two ends of the
// chain follow the pool at different speeds: atelet delivers the bundle to
// actors within seconds of the reconciler, while the gateway reads its CA from
// a Secret volume that kubelet refreshes on its own schedule, up to a minute
// or more later. A bundle without the gateway's CA would, for that long, leave
// every actor that loads its anchors unable to verify the leaves the gateway
// still mints — in whichever suite is running in parallel.
func RotateEgressTrustPool(t *testing.T, ctx context.Context, clients *Clients, cn string) string {
	t.Helper()
	secrets := clients.K8s.CoreV1().Secrets(egressCAPoolNamespace)
	created := createEgressTrustPool(t, ctx, clients, newEgressTrustPool(t))
	secret, err := secrets.Get(ctx, egressCAPoolSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading CA pool secret: %v", err)
	}
	if !created {
		original := maps.Clone(secret.Data)
		t.Cleanup(func() { restoreEgressTrustPool(t, clients, original) })
	}

	found, err := localca.Unmarshal(secret.Data[egressCAPoolSecretKey])
	if err != nil {
		t.Fatalf("parsing CA pool secret: %v", err)
	}
	rotated := &localca.ConcretePool{ActiveForSigning: found.ActiveForSigning}
	for _, ca := range found.CAs {
		if !strings.HasPrefix(ca.ID, rotatedCAIDPrefix) {
			rotated.CAs = append(rotated.CAs, ca)
		}
	}
	ca, err := localca.GenerateCA(rotatedCAIDPrefix+cn, localca.KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("generating CA %q for the egress pool: %v", cn, err)
	}
	rotated.CAs = append(rotated.CAs, ca)
	poolBytes, err := localca.Marshal(rotated)
	if err != nil {
		t.Fatalf("marshaling the rotated egress pool: %v", err)
	}
	// Only the pool changes: tls.crt and tls.key are the signer's, which stays.
	secret.Data[egressCAPoolSecretKey] = poolBytes
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("updating CA pool secret: %v", err)
	}

	want := rootsPEM(rotated)
	waitForEgressTrustBundle(t, ctx, clients, want)
	return want
}

// EgressTrustPool reads the egress MITM CA pool.
func EgressTrustPool(t *testing.T, ctx context.Context, clients *Clients) *localca.ConcretePool {
	t.Helper()
	pool, err := poolsecret.Get(ctx, clients.K8s.CoreV1().Secrets(egressCAPoolNamespace), egressCAPoolSecretName)
	if err != nil {
		t.Fatalf("reading the egress CA pool: %v", err)
	}
	return pool
}

// UpdateEgressTrustPool changes the egress MITM CA pool the way the
// `kubectl-ate admin` CA rotation commands do — mutate applied through
// poolsecret.Update, which keeps tls.crt and tls.key on the signing CA — and
// waits until the reconciler publishes the bundle of the resulting pool's
// roots. It returns the pool as written.
func UpdateEgressTrustPool(t *testing.T, ctx context.Context, clients *Clients, mutate func(*localca.ConcretePool) error) *localca.ConcretePool {
	t.Helper()
	pool, err := poolsecret.Update(ctx, clients.K8s.CoreV1().Secrets(egressCAPoolNamespace), egressCAPoolSecretName, mutate)
	if err != nil {
		t.Fatalf("updating the egress CA pool: %v", err)
	}
	waitForEgressTrustBundle(t, ctx, clients, rootsPEM(pool))
	return pool
}

// rootsPEM renders a pool the way the reconciler does: every root, in pool
// order.
func rootsPEM(pool *localca.ConcretePool) string {
	var b strings.Builder
	for _, ca := range pool.CAs {
		b.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.RootCertificate.Raw}))
	}
	return b.String()
}

// restoreEgressTrustPool puts back the pool contents RotateEgressTrustPool
// found, unless the pool has gone meanwhile: then its creator removed it, and
// recreating it would leave one behind.
func restoreEgressTrustPool(t *testing.T, clients *Clients, data map[string][]byte) {
	ctx := context.Background()
	secrets := clients.K8s.CoreV1().Secrets(egressCAPoolNamespace)
	secret, err := secrets.Get(ctx, egressCAPoolSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		t.Errorf("restoring the egress CA pool: %v", err)
		return
	}
	secret.Data = data
	if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Errorf("restoring the egress CA pool: %v", err)
	}
}

// SameCertificates reports whether two PEM bundles carry the same set of
// certificates. atelet shuffles a projected bundle's anchors (order carries no
// meaning), so a bundle of more than one is compared as a set.
func SameCertificates(a, b string) bool {
	return slices.Equal(certificateDERs(a), certificateDERs(b))
}

// certificateDERs returns the sorted DER bytes of a bundle's CERTIFICATE blocks.
func certificateDERs(bundle string) []string {
	var ders []string
	rest := []byte(bundle)
	for {
		var block *pem.Block
		if block, rest = pem.Decode(rest); block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			ders = append(ders, string(block.Bytes))
		}
	}
	slices.Sort(ders)
	return ders
}

// newEgressTrustPool builds a fresh single-CA pool Secret — the shape
// `kubectl-ate admin make-ca-pool` writes for the egress MITM CA.
func newEgressTrustPool(t *testing.T) *corev1.Secret {
	t.Helper()
	ca, err := localca.GenerateCA("mitm", localca.KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("generating CA for the egress pool: %v", err)
	}
	poolBytes, err := localca.Marshal(&localca.ConcretePool{CAs: []*localca.CA{ca}})
	if err != nil {
		t.Fatalf("marshaling the egress pool: %v", err)
	}
	certificateChain, err := ca.TLSCertificateChainPEM()
	if err != nil {
		t.Fatalf("encoding the egress CA certificate chain: %v", err)
	}
	privateKey, err := ca.TLSPrivateKeyPEM()
	if err != nil {
		t.Fatalf("encoding the egress CA private key: %v", err)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: egressCAPoolNamespace, Name: egressCAPoolSecretName},
		Type:       corev1.SecretTypeTLS,
		Data: map[string][]byte{
			egressCAPoolSecretKey:   poolBytes,
			corev1.TLSCertKey:       certificateChain,
			corev1.TLSPrivateKeyKey: privateKey,
		},
	}
}

// createEgressTrustPool creates secret and reports whether this call created
// it, tolerating a pool that already exists so concurrent first users cannot
// clobber each other. The creator — and only the creator — deletes it when its
// test ends, whereupon the reconciler deletes the bundle: a run leaves nothing
// behind, and no caller removes a pool it merely found.
func createEgressTrustPool(t *testing.T, ctx context.Context, clients *Clients, secret *corev1.Secret) bool {
	t.Helper()
	if _, err := clients.K8s.CoreV1().Secrets(egressCAPoolNamespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			t.Fatalf("creating CA pool secret %s/%s: %v", egressCAPoolNamespace, egressCAPoolSecretName, err)
		}
		return false
	}
	t.Cleanup(func() {
		_ = clients.K8s.CoreV1().Secrets(egressCAPoolNamespace).Delete(context.Background(), egressCAPoolSecretName, metav1.DeleteOptions{})
	})
	return true
}

// waitForEgressTrustBundle polls the reconciler-owned bundle until its
// contents match want, or are merely non-empty when want is "", keeping the
// reconcile latency out of later assertions. Accepted race: this polls the
// apiserver while atelet resolves from its informer cache, but the suites'
// start/resume latency dwarfs watch delivery — if a rotated-bundle
// assertion ever flakes, this lag is the first suspect.
func waitForEgressTrustBundle(t *testing.T, ctx context.Context, clients *Clients, want string) {
	t.Helper()
	var last string
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ctb, err := clients.K8s.CertificatesV1beta1().ClusterTrustBundles().Get(ctx, EgressTrustBundleObjectName, metav1.GetOptions{})
		if err == nil {
			if got := ctb.Spec.TrustBundle; got == want || (want == "" && got != "") {
				return
			} else {
				last = got
			}
		} else {
			last = "<" + err.Error() + ">"
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("timed out waiting for ClusterTrustBundle %q to carry the pool's root certificate (last observed: %.80q...); is atecontroller's EgressMITMTrustReconciler running?", EgressTrustBundleObjectName, last)
}
