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
	"context"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/agent-substrate/substrate/internal/ateclient"
	"github.com/agent-substrate/substrate/internal/localca"
	"github.com/agent-substrate/substrate/internal/localca/poolsecret"
	"github.com/agent-substrate/substrate/internal/localjwtauthority"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	// Both pool commands write one secret, so they share the flags naming it.
	poolSecretNamespaceFlag string
	poolSecretNameFlag      string
	makeCaPoolIDFlag        string
	makeCaPoolKeyTypeFlag   string
	makeJwtPoolKeyIDFlag    string

	// The CA rotation commands work on a CA pool secret named by the flags
	// above.
	rotateCAIDFlag            string
	addCAKeyTypeFlag          string
	addCAValidityFlag         time.Duration
	retireCAMinSigningAgeFlag time.Duration
)

var adminCmd = &cobra.Command{
	Use:   "admin",
	Short: "Administration and debugging commands",
}

var makeCaPoolCmd = &cobra.Command{
	Use:   "make-ca-pool",
	Short: "Make a new secret that contains a CA pool to be used by a signing controller",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		kconfig, err := ateclient.LoadKubeConfig(kubeconfig, k8sContext)
		if err != nil {
			return fmt.Errorf("while reading kubeconfig: %w", err)
		}

		kc, err := kubernetes.NewForConfig(kconfig)
		if err != nil {
			return fmt.Errorf("while creating Kubernetes client: %w", err)
		}

		keyType, err := parseKeyType(makeCaPoolKeyTypeFlag)
		if err != nil {
			return err
		}

		ca, err := localca.GenerateCA(
			makeCaPoolIDFlag,
			keyType,
			365*24*time.Hour,
		)
		if err != nil {
			return fmt.Errorf("while generating CA: %w", err)
		}

		pool := &localca.ConcretePool{
			CAs:              []*localca.CA{ca},
			ActiveForSigning: makeCaPoolIDFlag,
			ActivatedAt:      time.Now(),
		}

		data, err := poolsecret.Data(pool)
		if err != nil {
			return err
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: poolSecretNamespaceFlag,
				Name:      poolSecretNameFlag,
			},
			Type: corev1.SecretTypeTLS,
			Data: data,
		}

		_, err = kc.CoreV1().Secrets(poolSecretNamespaceFlag).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("while uploading pool state to secret: %w", err)
		}

		fmt.Printf("Successfully created CA pool secret %s/%s\n", poolSecretNamespaceFlag, poolSecretNameFlag)
		return nil
	},
}

var makeJwtPoolCmd = &cobra.Command{
	Use:   "make-jwt-pool",
	Short: "Make a new secret that contains a JWT authority pool to be used by the actor ID broker",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		kconfig, err := ateclient.LoadKubeConfig(kubeconfig, k8sContext)
		if err != nil {
			return fmt.Errorf("while reading kubeconfig: %w", err)
		}

		kc, err := kubernetes.NewForConfig(kconfig)
		if err != nil {
			return fmt.Errorf("while creating Kubernetes client: %w", err)
		}

		authority, err := localjwtauthority.GenerateECDSAP256Authority(makeJwtPoolKeyIDFlag)
		if err != nil {
			return fmt.Errorf("while generating JWT authority: %w", err)
		}

		pool := &localjwtauthority.ConcretePool{
			Authorities:      []*localjwtauthority.Authority{authority},
			ActiveForSigning: makeJwtPoolKeyIDFlag,
		}

		poolBytes, err := localjwtauthority.Marshal(pool)
		if err != nil {
			return fmt.Errorf("while marshaling pool: %w", err)
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: poolSecretNamespaceFlag,
				Name:      poolSecretNameFlag,
			},
			Data: map[string][]byte{
				"pool": poolBytes,
			},
		}

		_, err = kc.CoreV1().Secrets(poolSecretNamespaceFlag).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("while uploading pool state to secret: %w", err)
		}

		fmt.Printf("Successfully created JWT authority pool secret %s/%s\n", poolSecretNamespaceFlag, poolSecretNameFlag)
		return nil
	},
}

// defaultMinSigningAge is how long retire-ca waits after an activation by
// default: longer than the egress gateway's leaves live (sdsmint mints them
// for 15 minutes, agentgateway caches its minted leaves for 5) plus the time
// kubelet takes to refresh the Secret volume the gateway reads the CA from.
const defaultMinSigningAge = 20 * time.Minute

var addCACmd = &cobra.Command{
	Use:   "add-ca",
	Short: "Add a new CA to a CA pool secret, trusted but not yet signing",
	Long: `Add a new CA to a CA pool secret, trusted but not yet signing.

The new root joins the pool's trust anchors (for the egress MITM pool, the
egress-mitm.ate.dev trust bundle). The CA signing now also certifies the new
CA's key, so once activate-ca makes the new CA sign, what it signs still
verifies for workloads that loaded only the current root. Activate it with
activate-ca; no waiting is needed in between.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := flagCAPool()
		if err != nil {
			return err
		}
		var keyType *localca.KeyType
		if cmd.Flags().Changed("key-type") {
			kt, err := parseKeyType(addCAKeyTypeFlag)
			if err != nil {
				return err
			}
			keyType = &kt
		}
		return runAddCA(cmd.Context(), cmd.OutOrStdout(), target, rotateCAIDFlag, keyType, addCAValidityFlag)
	},
}

var activateCACmd = &cobra.Command{
	Use:   "activate-ca",
	Short: "Make a CA of a CA pool secret the one that signs",
	Long: `Make a CA of a CA pool secret the one that signs.

Only a CA added with add-ca while the current CA was signing can be activated:
its cross-certificate keeps what it signs verifiable for workloads that trust
only the current root. The secret's tls.crt and tls.key follow the new CA.
Retire the previous CA with retire-ca once the new one has signed long enough.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := flagCAPool()
		if err != nil {
			return err
		}
		return runActivateCA(cmd.Context(), cmd.OutOrStdout(), target, rotateCAIDFlag, time.Now())
	},
}

var retireCACmd = &cobra.Command{
	Use:   "retire-ca",
	Short: "Remove a CA that no longer signs from a CA pool secret",
	Long: `Remove a CA that no longer signs from a CA pool secret.

Its root leaves the pool's trust anchors. Refused for the CA that signs, and
until that CA has been signing for --min-signing-age: servers that read the
pool from a volume may sign with the retired CA until kubelet refreshes it,
and the leaves it signed stay in use until they expire.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := flagCAPool()
		if err != nil {
			return err
		}
		return runRetireCA(cmd.Context(), cmd.OutOrStdout(), target, rotateCAIDFlag, time.Now(), retireCAMinSigningAgeFlag)
	},
}

var getCAPoolCmd = &cobra.Command{
	Use:   "get-ca-pool",
	Short: "Show the CAs of a CA pool secret and which one signs",
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := flagCAPool()
		if err != nil {
			return err
		}
		pool, err := poolsecret.Get(cmd.Context(), target.secrets, target.name)
		if err != nil {
			return err
		}
		return printCAPool(cmd.OutOrStdout(), pool, time.Now())
	},
}

// caPool is the CA pool secret a command works on.
type caPool struct {
	secrets         typedcorev1.SecretInterface
	namespace, name string
}

func (c caPool) String() string { return c.namespace + "/" + c.name }

func runAddCA(ctx context.Context, out io.Writer, target caPool, id string, keyType *localca.KeyType, validity time.Duration) error {
	pool, err := poolsecret.Update(ctx, target.secrets, target.name, func(p *localca.ConcretePool) error {
		kt, err := newCAKeyType(p, keyType)
		if err != nil {
			return err
		}
		_, err = p.AddCA(id, kt, validity)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Added CA %q to %s. Its root is trusted from now on; activate it with activate-ca --ca-id=%s.\n\n",
		id, target, id)
	return printCAPool(out, pool, time.Now())
}

// newCAKeyType is the key type asked for, or else the signing CA's.
func newCAKeyType(pool *localca.ConcretePool, asked *localca.KeyType) (localca.KeyType, error) {
	if asked != nil {
		return *asked, nil
	}
	signer, err := pool.SigningCA()
	if err != nil {
		return 0, err
	}
	kt, err := signer.KeyType()
	if err != nil {
		return 0, fmt.Errorf("while choosing a key type like CA %q's (set --key-type): %w", signer.ID, err)
	}
	return kt, nil
}

func runActivateCA(ctx context.Context, out io.Writer, target caPool, id string, now time.Time) error {
	var changed bool
	pool, err := poolsecret.Update(ctx, target.secrets, target.name, func(p *localca.ConcretePool) error {
		var err error
		changed, err = p.Activate(id, now)
		return err
	})
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(out, "CA %q already signs in %s; nothing changed.\n\n", id, target)
	} else {
		fmt.Fprintf(out, "CA %q signs in %s from now on. Servers that read the pool from a volume switch once kubelet refreshes it, "+
			"typically within two minutes. Retire the previous CA with retire-ca after %s.\n\n",
			id, target, now.Add(defaultMinSigningAge).UTC().Format(time.RFC3339))
	}
	return printCAPool(out, pool, now)
}

func runRetireCA(ctx context.Context, out io.Writer, target caPool, id string, now time.Time, minSigningAge time.Duration) error {
	pool, err := poolsecret.Update(ctx, target.secrets, target.name, func(p *localca.ConcretePool) error {
		return p.Retire(id, now, minSigningAge)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Retired CA %q from %s; its root is no longer trusted by workloads that load the anchors from now on.\n\n",
		id, target)
	return printCAPool(out, pool, now)
}

func printCAPool(out io.Writer, pool *localca.ConcretePool, now time.Time) error {
	signer, err := pool.SigningCA()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSIGNING\tROOT EXPIRES\tCROSS-CERTIFIED BY")
	for _, ca := range pool.CAs {
		signing := "no"
		if ca == signer {
			signing = "yes"
			if !pool.ActivatedAt.IsZero() {
				signing = fmt.Sprintf("yes, for %s", now.Sub(pool.ActivatedAt).Round(time.Second))
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ca.ID, signing, ca.RootCertificate.NotAfter.UTC().Format(time.RFC3339), crossCertifiedBy(pool, ca))
	}
	return w.Flush()
}

// crossCertifiedBy names the CA of the pool that issued ca's cross-certificate.
func crossCertifiedBy(pool *localca.ConcretePool, ca *localca.CA) string {
	if ca.CrossCertificate == nil {
		return "-"
	}
	for _, issuer := range pool.CAs {
		if ca.CrossCertificate.CheckSignatureFrom(issuer.RootCertificate) == nil {
			return issuer.ID
		}
	}
	return "a retired CA"
}

func parseKeyType(s string) (localca.KeyType, error) {
	switch s {
	case "ED25519":
		return localca.KeyTypeED25519, nil
	case "ECDSAP256":
		return localca.KeyTypeECDSAP256, nil
	default:
		return 0, fmt.Errorf("unknown key type %q", s)
	}
}

// flagCAPool is the CA pool secret the flags name, on the kubeconfig's cluster.
func flagCAPool() (caPool, error) {
	kconfig, err := ateclient.LoadKubeConfig(kubeconfig, k8sContext)
	if err != nil {
		return caPool{}, fmt.Errorf("while reading kubeconfig: %w", err)
	}
	kc, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return caPool{}, fmt.Errorf("while creating Kubernetes client: %w", err)
	}
	return caPool{
		secrets:   kc.CoreV1().Secrets(poolSecretNamespaceFlag),
		namespace: poolSecretNamespaceFlag,
		name:      poolSecretNameFlag,
	}, nil
}

func init() {
	rootCmd.AddCommand(adminCmd)

	makeCaPoolCmd.Flags().StringVar(&makeCaPoolIDFlag, "ca-id", "", "The ID of the initial CA in the Pool")
	makeCaPoolCmd.Flags().StringVar(&poolSecretNamespaceFlag, "secret-namespace", "default", "Create the secret in this namespace")
	makeCaPoolCmd.Flags().StringVar(&poolSecretNameFlag, "name", "", "Create the secret with this name")
	makeCaPoolCmd.Flags().StringVar(&makeCaPoolKeyTypeFlag, "key-type", "ED25519", "CA key type.  One of [ED25519, ECDSAP256]")
	_ = makeCaPoolCmd.MarkFlagRequired("name")
	adminCmd.AddCommand(makeCaPoolCmd)

	makeJwtPoolCmd.Flags().StringVar(&makeJwtPoolKeyIDFlag, "key-id", "1", "The ID of the initial JWT signing key in the pool")
	makeJwtPoolCmd.Flags().StringVar(&poolSecretNamespaceFlag, "secret-namespace", "default", "Create the secret in this namespace")
	makeJwtPoolCmd.Flags().StringVar(&poolSecretNameFlag, "name", "", "Create the secret with this name")
	_ = makeJwtPoolCmd.MarkFlagRequired("name")
	adminCmd.AddCommand(makeJwtPoolCmd)

	for _, c := range []*cobra.Command{addCACmd, activateCACmd, retireCACmd, getCAPoolCmd} {
		c.Flags().StringVar(&poolSecretNamespaceFlag, "secret-namespace", "default", "The namespace of the CA pool secret")
		c.Flags().StringVar(&poolSecretNameFlag, "name", "", "The CA pool secret")
		_ = c.MarkFlagRequired("name")
		adminCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{addCACmd, activateCACmd, retireCACmd} {
		c.Flags().StringVar(&rotateCAIDFlag, "ca-id", "", "The ID of the CA")
		_ = c.MarkFlagRequired("ca-id")
	}
	addCACmd.Flags().StringVar(&addCAKeyTypeFlag, "key-type", "", "Key type of the new CA. One of [ED25519, ECDSAP256]; defaults to the signing CA's")
	addCACmd.Flags().DurationVar(&addCAValidityFlag, "validity", 365*24*time.Hour, "How long the new CA's root is valid")
	retireCACmd.Flags().DurationVar(&retireCAMinSigningAgeFlag, "min-signing-age", defaultMinSigningAge,
		"Refuse unless the signing CA has been signing at least this long: longer than the leaves the retired CA signed live, plus the Secret volume refresh")
}
