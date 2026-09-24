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

package poolsecret

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/agent-substrate/substrate/internal/localca"
)

const (
	namespace = "ate-system"
	name      = "egress-mitm-ca-pool"
)

func newClient(t *testing.T) (*fake.Clientset, *localca.ConcretePool) {
	t.Helper()
	ca, err := localca.GenerateCA("1", localca.KeyTypeECDSAP256, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}
	pool := &localca.ConcretePool{CAs: []*localca.CA{ca}, ActiveForSigning: "1"}
	data, err := Data(pool)
	if err != nil {
		t.Fatalf("Data: %v", err)
	}
	data["unrelated"] = []byte("kept")
	return fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       data,
	}), pool
}

func stored(t *testing.T, client *fake.Clientset) *corev1.Secret {
	t.Helper()
	secret, err := client.CoreV1().Secrets(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading the secret: %v", err)
	}
	return secret
}

func updates(client *fake.Clientset) int {
	n := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			n++
		}
	}
	return n
}

func TestUpdateFollowsTheSigningCA(t *testing.T) {
	ctx := context.Background()
	client, _ := newClient(t)
	secrets := client.CoreV1().Secrets(namespace)
	before := stored(t, client)

	if _, err := Update(ctx, secrets, name, func(p *localca.ConcretePool) error {
		_, err := p.AddCA("2", localca.KeyTypeECDSAP256, 365*24*time.Hour)
		return err
	}); err != nil {
		t.Fatalf("Update(AddCA): %v", err)
	}
	added := stored(t, client)
	if !bytes.Equal(added.Data[corev1.TLSCertKey], before.Data[corev1.TLSCertKey]) || !bytes.Equal(added.Data[corev1.TLSPrivateKeyKey], before.Data[corev1.TLSPrivateKeyKey]) {
		t.Errorf("adding a CA changed tls.crt/tls.key: the gateway would sign with a CA that is not active")
	}
	if got := string(added.Data["unrelated"]); got != "kept" {
		t.Errorf("unrelated key = %q, want kept", got)
	}

	written, err := Update(ctx, secrets, name, func(p *localca.ConcretePool) error {
		_, err := p.Activate("2", time.Now())
		return err
	})
	if err != nil {
		t.Fatalf("Update(Activate): %v", err)
	}
	activated := stored(t, client)
	two := written.CA("2")
	wantChain, _ := two.TLSCertificateChainPEM()
	wantKey, _ := two.TLSPrivateKeyPEM()
	if !bytes.Equal(activated.Data[corev1.TLSCertKey], wantChain) || !bytes.Equal(activated.Data[corev1.TLSPrivateKeyKey], wantKey) {
		t.Errorf("after activating CA 2, tls.crt/tls.key are not CA 2's cross-certificate and key")
	}

	got, err := Get(ctx, secrets, name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ActiveForSigning != "2" || len(got.CAs) != 2 {
		t.Errorf("stored pool: active %q with %d CAs, want 2 with 2", got.ActiveForSigning, len(got.CAs))
	}
}

func TestUpdateWritesNothingWithoutChange(t *testing.T) {
	client, _ := newClient(t)
	if _, err := Update(context.Background(), client.CoreV1().Secrets(namespace), name, func(p *localca.ConcretePool) error {
		_, err := p.Activate("1", time.Now())
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n := updates(client); n != 0 {
		t.Errorf("a no-op mutation wrote the secret %d times, want 0", n)
	}
}

func TestUpdateRetriesAConflict(t *testing.T) {
	client, _ := newClient(t)
	conflicts := 1
	client.PrependReactor("update", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		if conflicts == 0 {
			return false, nil, nil
		}
		conflicts--
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, name, nil)
	})

	calls := 0
	if _, err := Update(context.Background(), client.CoreV1().Secrets(namespace), name, func(p *localca.ConcretePool) error {
		calls++
		_, err := p.AddCA("2", localca.KeyTypeED25519, time.Hour)
		return err
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if calls != 2 {
		t.Errorf("mutate ran %d times, want 2 (once more after the conflict)", calls)
	}
	pool, err := Get(context.Background(), client.CoreV1().Secrets(namespace), name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pool.CA("2") == nil {
		t.Errorf("the retried write did not land")
	}
}

func TestUpdateKeepsTheSecretOnARefusal(t *testing.T) {
	client, _ := newClient(t)
	_, err := Update(context.Background(), client.CoreV1().Secrets(namespace), name, func(p *localca.ConcretePool) error {
		return p.Retire("1", time.Now(), 0)
	})
	if err == nil || !strings.Contains(err.Error(), "active for signing") {
		t.Fatalf("Update(Retire the signing CA) error = %v, want the refusal", err)
	}
	if n := updates(client); n != 0 {
		t.Errorf("a refused mutation wrote the secret %d times, want 0", n)
	}
}

func TestGetWithoutPool(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}})
	if _, err := Get(context.Background(), client.CoreV1().Secrets(namespace), name); err == nil || !strings.Contains(err.Error(), `no "pool" key`) {
		t.Errorf("Get error = %v, want a missing pool key", err)
	}
}
