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

// Package poolsecret keeps a localca pool in a Kubernetes Secret, the way
// `kubectl-ate admin make-ca-pool` writes it: the marshaled pool under
// PoolKey, which signing components read through a localca.RefreshingPool, and
// the signing CA's certificate chain and key under tls.crt and tls.key, which
// a TLS server that mints its own leaves (the egress gateway's dynamic CA)
// reads directly.
package poolsecret

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"

	"github.com/agent-substrate/substrate/internal/localca"
)

// PoolKey is the Secret key the marshaled pool is stored under.
const PoolKey = "pool"

// Data returns the Secret data for pool: the marshaled pool, and the chain and
// key of the CA it signs with.
func Data(pool *localca.ConcretePool) (map[string][]byte, error) {
	poolBytes, err := localca.Marshal(pool)
	if err != nil {
		return nil, fmt.Errorf("while marshaling pool: %w", err)
	}
	signer, err := pool.SigningCA()
	if err != nil {
		return nil, err
	}
	certificateChain, err := signer.TLSCertificateChainPEM()
	if err != nil {
		return nil, fmt.Errorf("while encoding the certificate chain of CA %q: %w", signer.ID, err)
	}
	privateKey, err := signer.TLSPrivateKeyPEM()
	if err != nil {
		return nil, fmt.Errorf("while encoding the private key of CA %q: %w", signer.ID, err)
	}
	return map[string][]byte{
		PoolKey:                 poolBytes,
		corev1.TLSCertKey:       certificateChain,
		corev1.TLSPrivateKeyKey: privateKey,
	}, nil
}

// Get reads the pool stored in the Secret name.
func Get(ctx context.Context, secrets typedcorev1.SecretInterface, name string) (*localca.ConcretePool, error) {
	secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("while reading CA pool secret %q: %w", name, err)
	}
	return decode(secret)
}

// Update applies mutate to the pool stored in the Secret name and writes the
// result back, with tls.crt and tls.key set to the chain and key of the CA the
// result signs with; other keys of the Secret are kept. It returns the pool as
// written. When another writer changed the Secret in between, mutate runs again
// on the new contents, so it must not have effects outside the pool. A mutation
// that leaves the Secret's data as it was writes nothing.
func Update(ctx context.Context, secrets typedcorev1.SecretInterface, name string, mutate func(*localca.ConcretePool) error) (*localca.ConcretePool, error) {
	var written *localca.ConcretePool
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := secrets.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("while reading CA pool secret %q: %w", name, err)
		}
		pool, err := decode(secret)
		if err != nil {
			return err
		}
		if err := mutate(pool); err != nil {
			return err
		}
		data, err := Data(pool)
		if err != nil {
			return err
		}
		written = pool
		if unchanged(secret.Data, data) {
			return nil
		}

		secret.Data = maps.Clone(secret.Data)
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		maps.Copy(secret.Data, data)
		if _, err := secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			// Returned unwrapped: RetryOnConflict recognizes a conflict by
			// the error itself.
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return written, nil
}

func decode(secret *corev1.Secret) (*localca.ConcretePool, error) {
	poolBytes, ok := secret.Data[PoolKey]
	if !ok {
		return nil, fmt.Errorf("CA pool secret %s/%s has no %q key", secret.Namespace, secret.Name, PoolKey)
	}
	pool, err := localca.Unmarshal(poolBytes)
	if err != nil {
		return nil, fmt.Errorf("while parsing CA pool secret %s/%s: %w", secret.Namespace, secret.Name, err)
	}
	return pool, nil
}

func unchanged(current, next map[string][]byte) bool {
	for key, value := range next {
		if !bytes.Equal(current[key], value) {
			return false
		}
	}
	return true
}
