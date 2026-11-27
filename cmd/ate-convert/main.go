// Copyright 2026 The Agent Substrate Authors
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

// ate-convert converts a quiesced Substrate 1.x (Giant Swarm line) install
// for Substrate 2.x: its PostgreSQL rows from schema public into schema
// substrate, its snapshot objects in place, and its WorkerPool,
// SandboxConfig and CSIDriverConfig objects to ate.dev/v1.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.ate.dev/substrate/cmd/ate-convert/internal/crds"
	"go.ate.dev/substrate/cmd/ate-convert/internal/snapshot"
	"go.ate.dev/substrate/cmd/ateapi/forkconvert"
	"go.ate.dev/substrate/pkg/objectstorage"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const usage = `usage: ate-convert [flags]            convert the database and the snapshots
       ate-convert db [flags]         convert the database
       ate-convert snapshots [flags]  convert the snapshots
       ate-convert crds prepare|restore [flags]
Flags --dry-run and --verify apply to the first three.
Substrate must be quiesced first: ate-api, ate-controller, the atelets and
egress at zero replicas, and every actor SUSPENDED, CRASHED or PAUSED.
The PostgreSQL connection strings are read from ATE_CONVERT_SOURCE_DSN and
ATE_CONVERT_TARGET_DSN (default: the source's); the object store from the
environment ate-api's objectstore plugin reads (ATE_STORAGE_BACKEND, AWS_*).
`

var errUsage = errors.New("bad arguments; see --help")

type config struct {
	dryRun, verify bool

	sourceSchema, targetSchema, targetRole string

	workerBinary, runsc, runscFlags, workDir string
	assetURL, assetSHA256                    string
	uploadMemory                             int64

	kubeconfig, kubeContext, stateDir, workerImage, sandboxVersion string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ate-convert:", err)
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usage)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	cmd := "all"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	phase := ""
	if cmd == "crds" {
		if len(args) == 0 || (args[0] != "prepare" && args[0] != "restore") {
			return errUsage
		}
		phase, args = args[0], args[1:]
	}
	var c config
	fs := flag.NewFlagSet("ate-convert", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage); fs.PrintDefaults() }
	fs.BoolVar(&c.dryRun, "dry-run", false, "read and convert everything, write nothing, and print what would change and every refusal")
	fs.BoolVar(&c.verify, "verify", false, "compare the converted database and snapshots with the 1.x ones")
	fs.StringVar(&c.sourceSchema, "source-schema", "public", "schema of the 1.x tables, never written")
	fs.StringVar(&c.targetSchema, "target-schema", "substrate", "schema of the 2.x tables; the 2.x installer's PostgreSQL setup creates it")
	fs.StringVar(&c.targetRole, "target-role", "substrate_owner", "role the target connection assumes, the owner of the target schema; empty assumes none")
	fs.StringVar(&c.workerBinary, "worker-binary", "", "ateworker-gvisor built with the ateconvert tag")
	fs.StringVar(&c.runsc, "runsc", "", "runsc of the 2.x default SandboxConfig version")
	fs.StringVar(&c.runscFlags, "runsc-flags", "", "the worker's kodata/internal_flags.json")
	fs.StringVar(&c.workDir, "work-dir", "", "scratch directory, with room for the largest snapshot twice")
	fs.StringVar(&c.assetURL, "sandbox-asset-url", "", "URL of the gVisor asset of the 2.x default SandboxConfig version, recorded in converted manifests")
	fs.StringVar(&c.assetSHA256, "sandbox-asset-sha256", "", "sha256 of that asset")
	fs.Int64Var(&c.uploadMemory, "upload-memory", 1<<30, "bytes of memory uploads may use")
	fs.StringVar(&c.kubeconfig, "kubeconfig", "", "kubeconfig; empty uses the in-cluster or default one")
	fs.StringVar(&c.kubeContext, "context", "", "kubeconfig context")
	fs.StringVar(&c.stateDir, "state-dir", "", "crds: directory the exported objects are kept in, between prepare and restore")
	fs.StringVar(&c.workerImage, "worker-image", "", "crds: the 2.x ateworker-gvisor image every WorkerPool gets")
	fs.StringVar(&c.sandboxVersion, "sandbox-version", "v1alpha1", "crds: version name a SandboxConfig's 1.x assets become")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if c.dryRun && c.verify {
		return errors.New("--dry-run and --verify exclude each other")
	}
	switch cmd {
	case "all", "db", "snapshots":
		return convert(ctx, &c, cmd, out)
	case "crds":
		return convertCRDs(ctx, &c, phase, out)
	default:
		return errUsage
	}
}

func convert(ctx context.Context, c *config, cmd string, out io.Writer) error {
	sourceDSN := os.Getenv("ATE_CONVERT_SOURCE_DSN")
	if sourceDSN == "" {
		return errors.New("ATE_CONVERT_SOURCE_DSN is not set")
	}
	targetDSN := os.Getenv("ATE_CONVERT_TARGET_DSN")
	if targetDSN == "" {
		targetDSN = sourceDSN
	}
	source, err := pgxpool.New(ctx, sourceDSN)
	if err != nil {
		return fmt.Errorf("source connection: %w", err)
	}
	defer source.Close()
	target, err := targetPool(ctx, targetDSN, c.targetSchema, c.targetRole)
	if err != nil {
		return err
	}
	defer target.Close()

	db, snapshots := cmd != "snapshots", cmd != "db"
	if c.verify {
		var errs []error
		if db {
			errs = append(errs, forkconvert.Verify(ctx, source, c.sourceSchema, target, c.targetSchema, out))
		}
		if snapshots {
			errs = append(errs, convertSnapshots(ctx, c, source, out))
		}
		return errors.Join(errs...)
	}

	src, err := forkconvert.Load(ctx, source, c.sourceSchema)
	if err != nil {
		return err
	}
	plan := forkconvert.Build(src)
	printPlan(out, plan, db)
	if len(plan.Refusals) > 0 {
		return fmt.Errorf("%d refusals; nothing was written", len(plan.Refusals))
	}
	if snapshots {
		if err := runSnapshots(ctx, c, plan, out); err != nil {
			return err
		}
	}
	if !db {
		return nil
	}
	exists, hasTables, err := forkconvert.SchemaExists(ctx, target, c.targetSchema)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("schema %q does not exist; run the 2.x installer's PostgreSQL setup (pkg/postgressetup) first", c.targetSchema)
	}
	if c.dryRun {
		if !hasTables {
			fmt.Fprintf(out, "dry run: %s has no tables yet, so the writes were not rehearsed\n", c.targetSchema)
			return nil
		}
		if err := forkconvert.Write(ctx, target, c.targetSchema, plan, false); err != nil {
			return err
		}
		fmt.Fprintf(out, "dry run: the writes to %s were rehearsed and rolled back\n", c.targetSchema)
		return nil
	}
	if err := forkconvert.Migrate(ctx, target); err != nil {
		return err
	}
	if err := forkconvert.Write(ctx, target, c.targetSchema, plan, true); err != nil {
		return err
	}
	fmt.Fprintf(out, "database converted into %s\n", c.targetSchema)
	return nil
}

// targetPool connects to the target schema as the role that owns it, the
// way ate-api's migrations do.
func targetPool(ctx context.Context, dsn, schema, role string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("target connection: invalid connection string")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = pgx.Identifier{schema}.Sanitize()
	if role != "" {
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET ROLE "+pgx.Identifier{role}.Sanitize())
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("target connection: %w", err)
	}
	return pool, nil
}

func printPlan(out io.Writer, plan *forkconvert.Plan, db bool) {
	if db {
		for _, c := range plan.Counts {
			fmt.Fprintf(out, "table %-26s %d rows -> %d\n", c.Table, c.Source, c.Target)
		}
	}
	for _, s := range plan.Snapshots {
		fmt.Fprintf(out, "snapshot %s of %v\n", s.URI, s.Holders)
	}
	for _, n := range plan.Notes {
		fmt.Fprintln(out, "note:", n)
	}
	for _, r := range plan.Refusals {
		fmt.Fprintln(out, "REFUSED:", r)
	}
}

func converter(ctx context.Context, c *config, out io.Writer) (*snapshot.Converter, error) {
	store, err := objectstorage.NewFromEnv(ctx)
	if err != nil {
		return nil, err
	}
	up, err := objectstorage.NewUploader(store, c.uploadMemory)
	if err != nil {
		return nil, err
	}
	conv := &snapshot.Converter{
		Store: store, Uploader: up, WorkerBinary: c.workerBinary, Runsc: c.runsc, RunscFlags: c.runscFlags, WorkDir: c.workDir,
		Asset: snapshot.Asset{URL: c.assetURL, SHA256: c.assetSHA256}, Log: out,
	}
	if c.dryRun {
		return conv, nil
	}
	for name, v := range map[string]string{"--worker-binary": c.workerBinary, "--runsc": c.runsc, "--runsc-flags": c.runscFlags, "--work-dir": c.workDir} {
		if v == "" {
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	if !c.verify && (c.assetURL == "" || c.assetSHA256 == "") {
		return nil, errors.New("--sandbox-asset-url and --sandbox-asset-sha256 are required")
	}
	return conv, nil
}

func jobs(plan *forkconvert.Plan) []snapshot.Job {
	var out []snapshot.Job
	for _, s := range plan.Snapshots {
		job := snapshot.Job{URI: s.URI}
		for _, ctr := range s.Containers {
			sc := snapshot.Container{Name: ctr.Name}
			for _, m := range ctr.Mounts {
				sc.Mounts = append(sc.Mounts, snapshot.Mount{Volume: m.Volume, Path: m.Path})
			}
			job.Containers = append(job.Containers, sc)
		}
		out = append(out, job)
	}
	return out
}

// runSnapshots converts (or, in a dry run, inspects) every snapshot, one at
// a time, and reports every failure.
func runSnapshots(ctx context.Context, c *config, plan *forkconvert.Plan, out io.Writer) error {
	conv, err := converter(ctx, c, out)
	if err != nil {
		return err
	}
	var errs []error
	for _, job := range jobs(plan) {
		var err error
		switch {
		case c.dryRun:
			err = conv.Plan(ctx, job)
		case c.verify:
			err = conv.Verify(ctx, job)
		default:
			err = conv.Convert(ctx, job)
		}
		if err != nil {
			fmt.Fprintf(out, "FAILED snapshot %s: %v\n", job.URI, err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%d of %d snapshots failed", len(errs), len(plan.Snapshots))
	}
	return nil
}

func convertSnapshots(ctx context.Context, c *config, source *pgxpool.Pool, out io.Writer) error {
	src, err := forkconvert.Load(ctx, source, c.sourceSchema)
	if err != nil {
		return err
	}
	return runSnapshots(ctx, c, forkconvert.Build(src), out)
}

func convertCRDs(ctx context.Context, c *config, phase string, out io.Writer) error {
	if c.stateDir == "" {
		return errors.New("--state-dir is required")
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = c.kubeconfig
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: c.kubeContext}).ClientConfig()
	if err != nil {
		return err
	}
	ext, err := apiextensions.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return err
	}
	clients := crds.Clients{CRDs: ext, Dynamic: dyn}
	if phase == "restore" {
		return crds.Restore(ctx, clients, c.stateDir, c.dryRun, out)
	}
	return crds.Prepare(ctx, clients, c.stateDir, crds.Options{WorkerImage: c.workerImage, SandboxVersion: c.sandboxVersion}, c.dryRun, out)
}
