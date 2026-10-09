// Copyright 2026 The Agent Substrate Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package seededvolumes exercises external volumes seeded per actor from a
// CSI snapshot against a live Kind cluster with the CSI hostpath driver and
// the volume snapshot API (`ate-setup setup csi hostpath`): the snapshot is
// taken outside Substrate, as a Kubernetes VolumeSnapshot of a PVC a pod
// filled, and actors are created with its handle.
package seededvolumes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	atespace = "seededvolumes"

	storageClass  = "csi-hostpath-sc"
	snapshotClass = "csi-hostpath-snapclass"
	driver        = "hostpath.csi.k8s.io"

	// The hostpath driver keeps each volume as a directory named after its
	// ID under this path of its plugin container.
	driverPod     = "csi-hostpathplugin-0"
	driverDataDir = "/csi-data-dir"

	volumeName = "workspace"
	mountPath  = "/mnt/workspace"
	capacity   = "1Gi"

	seedFile    = "seed.txt"
	seedContent = "prepared outside the actor"

	// probeWrittenContent is the fixed string the probe's /writefile writes.
	probeWrittenContent = "written by probe"

	busybox = "busybox@sha256:1487d0af5f52b4ba31c7e465126ee2123fe3f2305d638e7827681e7cf6c83d5e"
)

// requireHostpathSnapshots skips unless the cluster has the hostpath
// StorageClass and the volume snapshot API this suite needs. A lane that
// installed them sets E2E_CSI_HOSTPATH=1, and there a missing one fails the
// suite instead: a skip there would pass having run nothing.
func requireHostpathSnapshots(ctx context.Context, t *testing.T, clients *e2e.Clients) {
	t.Helper()
	missing := t.Skipf
	if os.Getenv("E2E_CSI_HOSTPATH") == "1" {
		missing = t.Fatalf
	}
	if _, err := clients.K8s.StorageV1().StorageClasses().Get(ctx, storageClass, metav1.GetOptions{}); err != nil {
		missing("StorageClass %q not found (%v); install it with `ate-setup setup csi hostpath`", storageClass, err)
	}
	if _, err := kubectl("get", "volumesnapshotclass", snapshotClass); err != nil {
		missing("VolumeSnapshotClass %q not found (%v); install it with `ate-setup setup csi hostpath`", snapshotClass, err)
	}
}

// kubectl runs kubectl against the suite's cluster and returns its stdout.
func kubectl(args ...string) (string, error) {
	if e2e.KubeContext != "" {
		args = append([]string{"--context=" + e2e.KubeContext}, args...)
	}
	if e2e.KubeConfig != "" {
		args = append([]string{"--kubeconfig=" + e2e.KubeConfig}, args...)
	}
	cmd := exec.Command("kubectl", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// eventually polls check until it returns true or the timeout passes.
func eventually(t *testing.T, timeout time.Duration, what string, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		ok, err := check()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s (last error: %v)", timeout, what, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// takeSeedSnapshot fills a hostpath PVC with seedFile through a pod,
// snapshots it with a VolumeSnapshot and returns the snapshot's handle.
func takeSeedSnapshot(ctx context.Context, t *testing.T, clients *e2e.Clients, ns string) string {
	t.Helper()

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "seed", Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: new(string),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(capacity)},
			},
		},
	}
	*pvc.Spec.StorageClassName = storageClass
	if _, err := clients.K8s.CoreV1().PersistentVolumeClaims(ns).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating PVC: %v", err)
	}

	writer := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "seed-writer", Namespace: ns},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:         "writer",
				Image:        busybox,
				Command:      []string{"sh", "-c", fmt.Sprintf("printf %%s %q > /data/%s && sync", seedContent, seedFile)},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "data",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name}},
			}},
		},
	}
	if _, err := clients.K8s.CoreV1().Pods(ns).Create(ctx, writer, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating seed writer pod: %v", err)
	}
	eventually(t, 3*time.Minute, "the seed writer pod to succeed", func() (bool, error) {
		pod, err := clients.K8s.CoreV1().Pods(ns).Get(ctx, writer.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if pod.Status.Phase == corev1.PodFailed {
			t.Fatalf("seed writer pod failed: %+v", pod.Status)
		}
		return pod.Status.Phase == corev1.PodSucceeded, fmt.Errorf("phase %s", pod.Status.Phase)
	})

	snapshot := fmt.Sprintf(`{"apiVersion":"snapshot.storage.k8s.io/v1","kind":"VolumeSnapshot","metadata":{"name":"seed","namespace":%q},"spec":{"volumeSnapshotClassName":%q,"source":{"persistentVolumeClaimName":%q}}}`, ns, snapshotClass, pvc.Name)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	if e2e.KubeContext != "" {
		cmd.Args = append(cmd.Args, "--context="+e2e.KubeContext)
	}
	if e2e.KubeConfig != "" {
		cmd.Args = append(cmd.Args, "--kubeconfig="+e2e.KubeConfig)
	}
	cmd.Stdin = strings.NewReader(snapshot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creating VolumeSnapshot: %v: %s", err, out)
	}

	var content string
	eventually(t, 3*time.Minute, "the VolumeSnapshot to be ready", func() (bool, error) {
		out, err := kubectl("get", "volumesnapshot", "-n", ns, "seed", "-o", "jsonpath={.status.readyToUse} {.status.boundVolumeSnapshotContentName}")
		if err != nil {
			return false, err
		}
		ready, name, _ := strings.Cut(out, " ")
		content = name
		return ready == "true" && name != "", fmt.Errorf("status %q", out)
	})
	handle, err := kubectl("get", "volumesnapshotcontent", content, "-o", "jsonpath={.status.snapshotHandle}")
	if err != nil || handle == "" {
		t.Fatalf("reading the snapshot handle of VolumeSnapshotContent %s: %q, %v", content, handle, err)
	}
	return handle
}

// createTemplate builds a probe ActorTemplate with one seeded external
// volume mounted at mountPath.
func createTemplate(ctx context.Context, t *testing.T, clients *e2e.Clients, ns *e2e.Namespace) *ateapipb.ActorTemplate {
	t.Helper()

	env, err := e2e.CheckEnv("BUCKET_NAME")
	if err != nil {
		t.Fatalf("CheckEnv: %v", err)
	}
	probeAtespace, _ := e2e.DeployProbe(t, env["BUCKET_NAME"], "seededvolumes")
	src := e2e.SubstrateFixture{
		Atespace:      probeAtespace,
		Name:          e2e.ProbeName,
		PoolNamespace: probeAtespace,
		PoolName:      e2e.ProbeName,
		DeployWith:    "the seededvolumes suite's own DeployProbe",
	}
	return e2e.CreateSubstrateTemplateFrom(ctx, t, clients, ns.Name, src, e2e.SubstrateTemplateOptions{
		Atespace:     atespace,
		Name:         "probe-" + ns.Name,
		PoolName:     e2e.ProbeName,
		PoolReplicas: 3,
		Labels:       map[string]string{"seededvolumes": ns.Name},
		SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation: fmt.Sprintf("gs://%s/%s/", env["BUCKET_NAME"], ns.Name),
		},
		Modify: func(tmpl *ateapipb.ActorTemplate) {
			tmpl.Containers[0].VolumeMounts = append(tmpl.Containers[0].VolumeMounts,
				&ateapipb.VolumeMount{Name: volumeName, MountPath: mountPath})
			tmpl.Volumes = append(tmpl.Volumes, &ateapipb.Volume{
				Name: volumeName,
				ExternalVolumeTemplate: &ateapipb.ExternalVolumeTemplate{
					Capacity:         capacity,
					StorageClassName: storageClass,
					Seeded:           true,
				},
			})
		},
	})
}

func createActor(ctx context.Context, clients *e2e.Clients, tmpl *ateapipb.ActorTemplate, ref resources.ActorRef, seeds ...*ateapipb.VolumeSeed) (*ateapipb.Actor, error) {
	return clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{
		Actor: &ateapipb.Actor{
			Metadata:      &ateapipb.ResourceMetadata{Atespace: ref.Atespace, Name: ref.Name},
			ActorTemplate: e2e.TemplateRef(tmpl),
			VolumeSeeds:   seeds,
		},
	})
}

// startActor creates and resumes an actor and registers its deletion.
func startActor(ctx context.Context, t *testing.T, clients *e2e.Clients, tmpl *ateapipb.ActorTemplate, ref resources.ActorRef, seeds ...*ateapipb.VolumeSeed) {
	t.Helper()
	if _, err := createActor(ctx, clients, tmpl, ref, seeds...); err != nil {
		t.Fatalf("CreateActor %s: %v", ref, err)
	}
	t.Cleanup(func() {
		_, _ = clients.SubstrateAPI.DeleteActor(context.Background(), &ateapipb.DeleteActorRequest{Actor: ref.ToObjectRef()})
	})
	if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: ref.ToObjectRef()}); err != nil {
		t.Fatalf("ResumeActor %s: %v", ref, err)
	}
}

func probeJSON(ctx context.Context, t *testing.T, router *e2e.RouterClient, ref resources.ActorRef, path string) map[string]string {
	t.Helper()
	resp, err := router.Get(ctx, ref, path)
	if err != nil {
		t.Fatalf("GET %s on %s: %v", path, ref, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s on %s: status %d: %s", path, ref, resp.StatusCode, body)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return out
}

func requireContent(ctx context.Context, t *testing.T, router *e2e.RouterClient, ref resources.ActorRef, path, want string) {
	t.Helper()
	got := probeJSON(ctx, t, router, ref, "/readfile?path="+path)
	if got["error"] != "" {
		t.Fatalf("%s: reading %s: %s", ref, path, got["error"])
	}
	if got["content"] != want {
		t.Errorf("%s: content at %s = %q, want %q", ref, path, got["content"], want)
	}
}

func requireAbsent(ctx context.Context, t *testing.T, router *e2e.RouterClient, ref resources.ActorRef, path string) {
	t.Helper()
	if got := probeJSON(ctx, t, router, ref, "/readfile?path="+path); got["error"] == "" {
		t.Errorf("%s: %s is readable (%q), want it absent", ref, path, got["content"])
	}
}

func requireWrite(ctx context.Context, t *testing.T, router *e2e.RouterClient, ref resources.ActorRef, path string) {
	t.Helper()
	if got := probeJSON(ctx, t, router, ref, "/writefile?path="+path); got["error"] != "" {
		t.Fatalf("%s: writing %s: %s", ref, path, got["error"])
	}
}

func getActor(ctx context.Context, t *testing.T, clients *e2e.Clients, ref resources.ActorRef) *ateapipb.Actor {
	t.Helper()
	actor, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: ref.ToObjectRef()})
	if err != nil {
		t.Fatalf("GetActor %s: %v", ref, err)
	}
	return actor
}

// storageVolumeID returns the driver's ID of the actor's seeded volume.
func storageVolumeID(ctx context.Context, t *testing.T, clients *e2e.Clients, ref resources.ActorRef) string {
	t.Helper()
	for _, vol := range getActor(ctx, t, clients, ref).GetStatus().GetActorVolumes() {
		if vol.GetVolumeName() == volumeName {
			return vol.GetStorageVolumeId()
		}
	}
	t.Fatalf("%s has no %q volume", ref, volumeName)
	return ""
}

// driverHasVolume reports whether the hostpath driver still holds volumeID.
func driverHasVolume(volumeID string) (bool, error) {
	out, err := kubectl("exec", "-n", "default", driverPod, "-c", "hostpath", "--", "ls", driverDataDir)
	if err != nil {
		return false, err
	}
	for _, name := range strings.Fields(out) {
		if name == volumeID {
			return true, nil
		}
	}
	return false, nil
}

func TestSeededVolumes(t *testing.T) {
	ctx := context.Background()
	clients := e2e.GetClients()
	requireHostpathSnapshots(ctx, t, clients)

	ns := e2e.CreateNamespace(t)
	handle := takeSeedSnapshot(ctx, t, clients, ns.Name)
	t.Logf("seed snapshot handle: %s", handle)
	tmpl := createTemplate(ctx, t, clients, ns)

	router, err := e2e.NewRouterClient(ctx)
	if err != nil {
		t.Fatalf("NewRouterClient: %v", err)
	}
	defer router.Close()

	seed := &ateapipb.VolumeSeed{VolumeName: volumeName, Driver: driver, SnapshotHandle: handle}
	actorA := resources.ActorRef{Atespace: atespace, Name: "seeded-a-" + ns.Name}
	actorB := resources.ActorRef{Atespace: atespace, Name: "seeded-b-" + ns.Name}
	unseeded := resources.ActorRef{Atespace: atespace, Name: "unseeded-" + ns.Name}
	seedPath := mountPath + "/" + seedFile
	writtenPath := mountPath + "/written-by-a.txt"

	t.Run("StartsWithSnapshotFilesReadWrite", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, actorA, seed)
		requireContent(ctx, t, router, actorA, seedPath, seedContent)
		requireWrite(ctx, t, router, actorA, writtenPath)
		requireContent(ctx, t, router, actorA, writtenPath, probeWrittenContent)
	})

	t.Run("ActorsFromOneHandleAreIndependent", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, actorB, seed)
		requireContent(ctx, t, router, actorB, seedPath, seedContent)
		requireAbsent(ctx, t, router, actorB, writtenPath)
		if a, b := storageVolumeID(ctx, t, clients, actorA), storageVolumeID(ctx, t, clients, actorB); a == b {
			t.Errorf("both actors have storage volume %q, want one each", a)
		}
	})

	t.Run("UnseededActorHasNoVolumeOrMount", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, unseeded)
		if vols := getActor(ctx, t, clients, unseeded).GetStatus().GetActorVolumes(); len(vols) != 0 {
			t.Errorf("unseeded actor has volumes %v, want none", vols)
		}
		requireAbsent(ctx, t, router, unseeded, seedPath)
	})

	t.Run("PauseResumeKeepsContent", func(t *testing.T) {
		if _, err := clients.SubstrateAPI.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("PauseActor: %v", err)
		}
		eventually(t, 2*time.Minute, "actor A to pause", func() (bool, error) {
			state := getActor(ctx, t, clients, actorA).GetStatus().GetState()
			return state == ateapipb.ActorState_ACTOR_STATE_PAUSED, fmt.Errorf("state %v", state)
		})
		if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("ResumeActor: %v", err)
		}
		requireContent(ctx, t, router, actorA, seedPath, seedContent)
		requireContent(ctx, t, router, actorA, writtenPath, probeWrittenContent)
	})

	t.Run("DeleteDeletesTheVolume", func(t *testing.T) {
		volumeID := storageVolumeID(ctx, t, clients, actorA)
		if has, err := driverHasVolume(volumeID); err != nil || !has {
			t.Fatalf("driver holds volume %q before the delete = %v, %v; want true", volumeID, has, err)
		}
		if _, err := clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("DeleteActor: %v", err)
		}
		eventually(t, 2*time.Minute, "actor A and its volume to be gone", func() (bool, error) {
			_, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: actorA.ToObjectRef()})
			if status.Code(err) != codes.NotFound {
				return false, fmt.Errorf("GetActor: %v", err)
			}
			has, err := driverHasVolume(volumeID)
			return err == nil && !has, err
		})
	})

	t.Run("RefusesBadSeedsAtCreate", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			seed     *ateapipb.VolumeSeed
			wantCode codes.Code
			wantMsg  string
		}{
			{
				name:     "unknown driver",
				seed:     &ateapipb.VolumeSeed{VolumeName: volumeName, Driver: "unknown.csi.example.com", SnapshotHandle: handle},
				wantCode: codes.InvalidArgument,
				wantMsg:  `names driver "unknown.csi.example.com"`,
			},
			{
				name:     "missing handle",
				seed:     &ateapipb.VolumeSeed{VolumeName: volumeName, Driver: driver, SnapshotHandle: "no-such-snapshot"},
				wantCode: codes.FailedPrecondition,
				wantMsg:  `holds no snapshot "no-such-snapshot"`,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ref := resources.ActorRef{Atespace: atespace, Name: "refused-" + ns.Name}
				_, err := createActor(ctx, clients, tmpl, ref, tc.seed)
				if status.Code(err) != tc.wantCode || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("CreateActor = %v, want %v containing %q", err, tc.wantCode, tc.wantMsg)
				}
				t.Logf("refused as expected: %v", err)
			})
		}
	})
}
