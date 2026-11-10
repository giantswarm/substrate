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

package demo

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// olderGVisor is the gVisor build the per-test SandboxConfig names first: a
// build the atelet keeps trusting after its SandboxConfig moves on
// (retainedSandboxAssets in cmd/atelet), so a full-scope snapshot it
// captured still restores on it.
var olderGVisor = map[string]map[string]v1alpha1.AssetFile{
	"amd64": {"gvisor": {
		URL:    "gs://gvisor/releases/nightly/2026-09-02/x86_64/gvisor.tar.zstd",
		SHA256: "d547d81401461fd1c679c5c4fa0a6c2b8ef7dc3c22ce23c9e25dcc4c69cfd06f",
	}},
	"arm64": {"gvisor": {
		URL:    "gs://gvisor/releases/nightly/2026-09-02/aarch64/gvisor.tar.zstd",
		SHA256: "a64916f9813ce7e4841a30480a599337f7dda07b421c6bf0123db2212aa7d1df",
	}},
}

// TestSandboxConfigChange covers an actor whose SandboxConfig names another
// gVisor build between its suspend and its resume. A data-scope snapshot
// boots the guest fresh, so its restore runs the SandboxConfig's current
// build; a full-scope snapshot loads a checkpoint image only the build that
// wrote it reads, so its restore keeps that build. The actor's
// status.sandbox_assets (kubectl ate's SANDBOX column) names the build each
// run is on.
func TestSandboxConfigChange(t *testing.T) {
	if e2e.IsMicroVM() {
		t.Skip("the SandboxConfig under test names gVisor builds")
	}
	t.Parallel()
	for _, tc := range []struct {
		name     string
		onCommit ateapipb.SnapshotContentScope
	}{
		{name: "onCommit:Data", onCommit: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA},
		{name: "onCommit:Full", onCommit: ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_FULL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSandboxConfigChangeTestCase(t, tc.onCommit)
		})
	}
}

func runSandboxConfigChangeTestCase(t *testing.T, onCommit ateapipb.SnapshotContentScope) {
	nsObj := e2e.CreateNamespace(t)
	ctx := context.Background()
	clients := e2e.GetClients()

	env, err := e2e.CheckEnv("BUCKET_NAME")
	if err != nil {
		t.Fatalf("CheckEnv failed: %v", err)
	}

	src := e2e.SubstrateCounterFixture()
	srcTmpl, err := clients.SubstrateAPI.GetActorTemplate(ctx, &ateapipb.GetActorTemplateRequest{
		ActorTemplate: &ateapipb.ObjectRef{Atespace: src.Atespace, Name: src.Name},
	})
	if err != nil {
		t.Fatalf("failed to get ActorTemplate %s/%s (deploy with: %s): %v", src.Atespace, src.Name, src.DeployWith, err)
	}
	sandboxConfigs := clients.SubstrateK8s.ApiV1alpha1().SandboxConfigs()
	current, err := sandboxConfigs.Get(ctx, srcTmpl.GetSandboxConfig().GetConfigName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get the fixture's SandboxConfig: %v", err)
	}
	if maps.EqualFunc(current.Spec.Assets, olderGVisor, maps.Equal) {
		t.Skipf("SandboxConfig %q already names the older gVisor build", current.Name)
	}

	// A SandboxConfig of this test's own, on the older build first.
	configName := "sandbox-change-" + nsObj.Name
	if _, err := sandboxConfigs.Create(ctx, &v1alpha1.SandboxConfig{
		ObjectMeta: metav1.ObjectMeta{Name: configName},
		Spec: v1alpha1.SandboxConfigSpec{
			SandboxClass: current.Spec.SandboxClass,
			PauseImage:   current.Spec.PauseImage,
			Assets:       olderGVisor,
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create SandboxConfig %q: %v", configName, err)
	}
	t.Cleanup(func() {
		if err := sandboxConfigs.Delete(context.Background(), configName, metav1.DeleteOptions{}); err != nil {
			t.Logf("failed to delete SandboxConfig %q: %v", configName, err)
		}
	})

	tmplName := "sandbox-change-" + nsObj.Name
	e2e.CreateSubstrateCounterTemplate(ctx, t, clients, nsObj.Name, e2e.SubstrateTemplateOptions{
		Atespace:     demoAtespace,
		Name:         tmplName,
		PoolName:     "sandbox-change",
		PoolReplicas: 2,
		Labels:       map[string]string{"demo": nsObj.Name},
		SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation: "gs://" + env["BUCKET_NAME"] + "/ate-demo-" + nsObj.Name,
			OnCommit:        onCommit,
		},
		Modify: func(tmpl *ateapipb.ActorTemplate) {
			tmpl.SandboxConfig.ConfigName = configName
		},
	})

	actorID := "sandbox-change-" + nsObj.Name
	actor := &ateapipb.ObjectRef{Atespace: demoAtespace, Name: actorID}
	actorRef := resources.ActorRef{Atespace: demoAtespace, Name: actorID}
	if _, err := clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{Actor: &ateapipb.Actor{
		Metadata:      &ateapipb.ResourceMetadata{Atespace: demoAtespace, Name: actorID},
		ActorTemplate: &ateapipb.ObjectRef{Atespace: demoAtespace, Name: tmplName},
	}}); err != nil {
		t.Fatalf("failed to create Actor: %v", err)
	}
	defer func() {
		clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: actor})
	}()

	t.Logf("Resuming Actor %q on the older gVisor build...", actorID)
	if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: actor}); err != nil {
		t.Fatalf("failed to resume Actor: %v", err)
	}
	// The first resume also absorbs the freshly created pool's worker startup.
	waitForActorStateWithTimeout(ctx, t, clients, actorID, ateapipb.ActorState_ACTOR_STATE_RUNNING, 120*time.Second)
	resp, err := callActor(t, actorRef)
	if err != nil {
		t.Fatalf("failed to call actor: %v", err)
	}
	validateCounterResponse(t, resp, "on the older build", 1, 1)
	expectActorGVisor(ctx, t, clients, actor, "on the older build", olderGVisor)

	// The SandboxConfig moves on while the actor runs; the running sandbox
	// and the suspend's snapshot stay on the build the actor started with.
	t.Logf("Moving SandboxConfig %q to the current gVisor build...", configName)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sc, err := sandboxConfigs.Get(ctx, configName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		sc.Spec.Assets = current.Spec.Assets
		_, err = sandboxConfigs.Update(ctx, sc, metav1.UpdateOptions{})
		return err
	}); err != nil {
		t.Fatalf("failed to update SandboxConfig %q: %v", configName, err)
	}

	t.Logf("Suspending Actor %q...", actorID)
	if _, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: actor}); err != nil {
		t.Fatalf("failed to suspend Actor: %v", err)
	}
	waitForActorState(ctx, t, clients, actorID, ateapipb.ActorState_ACTOR_STATE_SUSPENDED)

	t.Logf("Resuming Actor %q after the SandboxConfig change...", actorID)
	if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: actor}); err != nil {
		t.Fatalf("failed to resume Actor after the SandboxConfig change: %v", err)
	}
	waitForActorState(ctx, t, clients, actorID, ateapipb.ActorState_ACTOR_STATE_RUNNING)
	resp, err = callActor(t, actorRef)
	if err != nil {
		t.Fatalf("failed to call actor after the SandboxConfig change: %v", err)
	}
	if onCommit == ateapipb.SnapshotContentScope_SNAPSHOT_CONTENT_SCOPE_DATA {
		// A fresh guest on the current build, the durable dir carried over.
		validateCounterResponse(t, resp, "data-scope restore after the SandboxConfig change", 1, 2)
		expectActorGVisor(ctx, t, clients, actor, "data-scope restore after the SandboxConfig change", current.Spec.Assets)
	} else {
		// The checkpoint image restored on the build that wrote it.
		validateCounterResponse(t, resp, "full-scope restore after the SandboxConfig change", 2, 2)
		expectActorGVisor(ctx, t, clients, actor, "full-scope restore after the SandboxConfig change", olderGVisor)
	}
}

// expectActorGVisor checks that the actor's status names one of the
// architectures' gvisor asset of want as the build it runs on.
func expectActorGVisor(ctx context.Context, t *testing.T, clients *e2e.Clients, actor *ateapipb.ObjectRef, stage string, want map[string]map[string]v1alpha1.AssetFile) {
	t.Helper()
	got, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: actor})
	if err != nil {
		t.Fatalf("[%s] failed to get Actor: %v", stage, err)
	}
	assets := got.GetStatus().GetSandboxAssets()
	for _, a := range assets {
		if a.GetName() != "gvisor" {
			continue
		}
		for _, files := range want {
			if f, ok := files["gvisor"]; ok && f.URL == a.GetUrl() && f.SHA256 == a.GetSha256() {
				return
			}
		}
	}
	t.Errorf("[%s] Actor status.sandbox_assets = %v, want the gvisor asset of %v", stage, assets, want)
}
