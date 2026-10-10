// Copyright 2026 Google LLC
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

// Package existingvolumes exercises existing volumes against a live Kind
// cluster with the CSI NFS driver (`ate-setup setup csi nfs`): a
// read-write-many volume is provisioned and filled outside Substrate, as a
// Kubernetes PVC a pod prepares, and actors mount directories of it.
package existingvolumes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/e2e"
	"github.com/agent-substrate/substrate/internal/nodepath"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	atespace = "existingvolumes"
	driver   = "nfs.csi.k8s.io"

	workspaceVolume = "workspace"
	mirrorsVolume   = "mirrors"
	workspacePath   = "/workspace"
	mirrorsPath     = "/mirrors"

	mirrorFile    = "mirror.txt"
	mirrorContent = "a file of the mirrors"

	// probeWrittenContent is the fixed string the probe's /writefile writes.
	probeWrittenContent = "written by probe"

	busybox = "busybox@sha256:1487d0af5f52b4ba31c7e465126ee2123fe3f2305d638e7827681e7cf6c83d5e"
)

// requireNFS skips unless the cluster has the NFS StorageClass. A lane that
// installed it sets E2E_CSI_NFS=1, and there a missing one fails the suite
// instead: a skip there would pass having run nothing.
func requireNFS(ctx context.Context, t *testing.T, clients *e2e.Clients) {
	t.Helper()
	missing := t.Skipf
	if os.Getenv("E2E_CSI_NFS") == "1" {
		missing = t.Fatalf
	}
	if _, err := clients.K8s.StorageV1().StorageClasses().Get(ctx, e2e.StorageClass, metav1.GetOptions{}); err != nil {
		missing("StorageClass %q not found (%v); install it with `ate-setup setup csi nfs`", e2e.StorageClass, err)
	}
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

// runPod creates a pod that runs to completion and returns its output.
func runPod(ctx context.Context, t *testing.T, clients *e2e.Clients, pod *corev1.Pod) string {
	t.Helper()
	ns, name := pod.Namespace, pod.Name
	if _, err := clients.K8s.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating pod %s: %v", name, err)
	}
	eventually(t, 3*time.Minute, "pod "+name+" to succeed", func() (bool, error) {
		got, err := clients.K8s.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if got.Status.Phase == corev1.PodFailed {
			t.Fatalf("pod %s failed: %+v", name, got.Status)
		}
		return got.Status.Phase == corev1.PodSucceeded, fmt.Errorf("phase %s", got.Status.Phase)
	})
	out, err := clients.K8s.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{}).Do(ctx).Raw()
	if err != nil {
		t.Fatalf("reading the logs of pod %s: %v", name, err)
	}
	return string(out)
}

// runOnVolume runs script in a pod that mounts the claim at /data and
// returns its output.
func runOnVolume(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, claim, name, script string) string {
	t.Helper()
	return runPod(ctx, t, clients, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:         "sh",
				Image:        busybox,
				Command:      []string{"sh", "-c", script},
				VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "data",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}},
			}},
		},
	})
}

// runOnNode runs script on node, in the host's PID namespace and with the
// node's Substrate directory mounted at its own path, and returns its output.
// The actor's UID reaches the script as $ACTOR_UID rather than in its text, so
// the pod's own processes never match a search of the node's command lines
// for it.
func runOnNode(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, node, actorUID, name, script string) string {
	t.Helper()
	return runPod(ctx, t, clients, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			NodeName:      node,
			HostPID:       true,
			Containers: []corev1.Container{{
				Name:         "sh",
				Image:        busybox,
				Command:      []string{"sh", "-c", script},
				Env:          []corev1.EnvVar{{Name: "ACTOR_UID", Value: actorUID}},
				VolumeMounts: []corev1.VolumeMount{{Name: "ate", MountPath: nodepath.BasePath}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "ate",
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: nodepath.BasePath}},
			}},
		},
	})
}

// nodeActorState is what a node holds of an actor: its directory under
// nodepath.ActorsDir, the mounts under it in the host's mount namespace, and
// the processes naming its UID (the sandbox's runsc processes run with the
// actor's bundle path on their command line).
type nodeActorState struct {
	Dir    bool
	Mounts int
	Procs  int
}

// inspectNode reads what node holds of the actor.
func inspectNode(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, node, actorUID, name string) nodeActorState {
	t.Helper()
	out := runOnNode(ctx, t, clients, ns, node, actorUID, name, fmt.Sprintf(`
dir=%s/$ACTOR_UID
if [ -d "$dir" ]; then echo dir=present; else echo dir=absent; fi
echo mounts=$(grep -c "/actors/$ACTOR_UID/" /proc/1/mountinfo)
printf %%s "$ACTOR_UID" > /tmp/uid
n=0
for c in /proc/[0-9]*/cmdline; do
  [ "$c" = "/proc/$$/cmdline" ] && continue
  grep -q -F -f /tmp/uid "$c" 2>/dev/null && n=$((n+1))
done
echo procs=$n
`, nodepath.ActorsDir))
	var state nodeActorState
	for _, line := range strings.Fields(out) {
		key, value, _ := strings.Cut(line, "=")
		n, _ := strconv.Atoi(value)
		switch key {
		case "dir":
			state.Dir = value == "present"
		case "mounts":
			state.Mounts = n
		case "procs":
			state.Procs = n
		}
	}
	t.Logf("node %s holds of actor %s: %+v", node, actorUID, state)
	return state
}

// blockCheckpoint makes the actor's checkpoint-state path on its node a file,
// so the node's next checkpoint of it fails creating that directory before it
// touches the sandbox: the suspend fails, the sandbox stays up.
func blockCheckpoint(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, node, actorUID string) {
	t.Helper()
	// The directory name is atelet's (ateletpath.CheckpointStateDir).
	runOnNode(ctx, t, clients, ns, node, actorUID, "block-checkpoint",
		fmt.Sprintf(`p=%s/$ACTOR_UID/checkpoint-state && rm -rf "$p" && touch "$p" && ls -l "$p"`, nodepath.ActorsDir))
}

func getActor(ctx context.Context, t *testing.T, clients *e2e.Clients, ref resources.ActorRef) *ateapipb.Actor {
	t.Helper()
	actor, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: ref.ToObjectRef()})
	if err != nil {
		t.Fatalf("GetActor %s: %v", ref, err)
	}
	return actor
}

// allocatedActors returns how many actors the worker reports allocated.
func allocatedActors(ctx context.Context, t *testing.T, clients *e2e.Clients, worker string) int32 {
	t.Helper()
	got, err := clients.SubstrateAPI.GetWorker(ctx, &ateapipb.GetWorkerRequest{Worker: &ateapipb.ObjectRef{Name: worker}})
	if err != nil {
		t.Fatalf("GetWorker %s: %v", worker, err)
	}
	return got.GetStatus().GetAllocated().GetActors()
}

// prepareVolume provisions a read-write-many PVC named claim, lays out the
// sessions directory with one session directory made ahead (the others are
// Substrate's to create) and a mirrors directory with mirrorFile, and returns
// the claim and its PersistentVolume's name and handle.
func prepareVolume(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, claim string) (_, pvName, handle string) {
	t.Helper()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: claim, Namespace: ns},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
			StorageClassName: &e2e.StorageClass,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	if _, err := clients.K8s.CoreV1().PersistentVolumeClaims(ns).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating PVC: %v", err)
	}
	runOnVolume(ctx, t, clients, ns, pvc.Name, "prepare-"+claim, fmt.Sprintf(
		"mkdir -p /data/sessions && mkdir -m 0777 /data/sessions/b && mkdir -p /data/mirrors && printf %%s %q > /data/mirrors/%s && sync",
		mirrorContent, mirrorFile))

	bound, err := clients.K8s.CoreV1().PersistentVolumeClaims(ns).Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading PVC: %v", err)
	}
	pv, err := clients.K8s.CoreV1().PersistentVolumes().Get(ctx, bound.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("reading PersistentVolume %q: %v", bound.Spec.VolumeName, err)
	}
	if pv.Spec.CSI == nil || pv.Spec.CSI.Driver != driver {
		t.Fatalf("PersistentVolume %q is not a volume of %s: %+v", pv.Name, driver, pv.Spec.PersistentVolumeSource)
	}
	return pvc.Name, pv.Name, pv.Spec.CSI.VolumeHandle
}

// createTemplate builds a probe ActorTemplate that mounts the existing
// volume "workspace" at workspacePath and "mirrors" read-only at mirrorsPath.
func createTemplate(ctx context.Context, t *testing.T, clients *e2e.Clients, ns *e2e.Namespace) *ateapipb.ActorTemplate {
	t.Helper()
	env, err := e2e.CheckEnv("BUCKET_NAME")
	if err != nil {
		t.Fatalf("CheckEnv: %v", err)
	}
	probeAtespace, _ := e2e.DeployProbe(t, env["BUCKET_NAME"], "existingvolumes")
	src := e2e.SubstrateFixture{
		Atespace:      probeAtespace,
		Name:          e2e.ProbeName,
		PoolNamespace: probeAtespace,
		PoolName:      e2e.ProbeName,
		DeployWith:    "the existingvolumes suite's own DeployProbe",
	}
	return e2e.CreateSubstrateTemplateFrom(ctx, t, clients, ns.Name, src, e2e.SubstrateTemplateOptions{
		Atespace:     atespace,
		Name:         "probe-" + ns.Name,
		PoolName:     e2e.ProbeName,
		PoolReplicas: 3,
		Labels:       map[string]string{"existingvolumes": ns.Name},
		SnapshotConfig: &ateapipb.SnapshotConfig{
			StorageLocation: fmt.Sprintf("gs://%s/%s/", env["BUCKET_NAME"], ns.Name),
		},
		Modify: func(tmpl *ateapipb.ActorTemplate) {
			tmpl.Containers[0].VolumeMounts = append(tmpl.Containers[0].VolumeMounts,
				&ateapipb.VolumeMount{Name: workspaceVolume, MountPath: workspacePath},
				&ateapipb.VolumeMount{Name: mirrorsVolume, MountPath: mirrorsPath, ReadOnly: true})
			tmpl.Volumes = append(tmpl.Volumes,
				&ateapipb.Volume{Name: workspaceVolume, ExistingVolume: &ateapipb.ExistingVolumeSource{}},
				&ateapipb.Volume{Name: mirrorsVolume, ExistingVolume: &ateapipb.ExistingVolumeSource{}})
		},
	})
}

// sessionVolumes supply both existing volumes from one handle: the
// session's own directory read-write and the mirrors read-only.
func sessionVolumes(handle, session string) []*ateapipb.ExistingVolume {
	return []*ateapipb.ExistingVolume{
		{Name: workspaceVolume, Driver: driver, VolumeHandle: handle, AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY, SubPath: "sessions/" + session},
		{Name: mirrorsVolume, Driver: driver, VolumeHandle: handle, AccessMode: ateapipb.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_ONLY_MANY, SubPath: "mirrors"},
	}
}

func createActor(ctx context.Context, clients *e2e.Clients, tmpl *ateapipb.ActorTemplate, ref resources.ActorRef, evs []*ateapipb.ExistingVolume) (*ateapipb.Actor, error) {
	return clients.SubstrateAPI.CreateActor(ctx, &ateapipb.CreateActorRequest{
		Actor: &ateapipb.Actor{
			Metadata:        &ateapipb.ResourceMetadata{Atespace: ref.Atespace, Name: ref.Name},
			ActorTemplate:   e2e.TemplateRef(tmpl),
			ExistingVolumes: evs,
		},
	})
}

// deleteActorAtEnd registers the deletion of an actor when the whole suite
// ends: a later subtest uses an actor an earlier one started.
func deleteActorAtEnd(t *testing.T, clients *e2e.Clients, ref resources.ActorRef) {
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = clients.SubstrateAPI.SuspendActor(cleanupCtx, &ateapipb.SuspendActorRequest{Actor: ref.ToObjectRef()})
		_, _ = clients.SubstrateAPI.DeleteActor(cleanupCtx, &ateapipb.DeleteActorRequest{Actor: ref.ToObjectRef()})
	})
}

// startActor creates and resumes an actor.
func startActor(ctx context.Context, t *testing.T, clients *e2e.Clients, tmpl *ateapipb.ActorTemplate, ref resources.ActorRef, evs []*ateapipb.ExistingVolume) {
	t.Helper()
	if _, err := createActor(ctx, clients, tmpl, ref, evs); err != nil {
		t.Fatalf("CreateActor %s: %v", ref, err)
	}
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

func requireWriteRefused(ctx context.Context, t *testing.T, router *e2e.RouterClient, ref resources.ActorRef, path string) {
	t.Helper()
	got := probeJSON(ctx, t, router, ref, "/writefile?path="+path)
	if got["error"] == "" {
		t.Fatalf("%s: writing %s succeeded, want it refused", ref, path)
	}
	t.Logf("%s: write to %s refused as expected: %s", ref, path, got["error"])
}

// volumeFiles lists the regular files on the volume, relative to its root.
func volumeFiles(ctx context.Context, t *testing.T, clients *e2e.Clients, ns, claim, name string) []string {
	t.Helper()
	out := runOnVolume(ctx, t, clients, ns, claim, name, "cd /data && find . -type f | sort")
	return strings.Fields(out)
}

func requireFiles(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("volume files %v lack %s", got, w)
		}
	}
}

func TestExistingVolumes(t *testing.T) {
	ctx := context.Background()
	clients := e2e.GetClients()
	requireNFS(ctx, t, clients)

	ns := e2e.CreateNamespace(t)
	claim, pvName, handle := prepareVolume(ctx, t, clients, ns.Name, "workspace")
	t.Logf("existing volume: PersistentVolume %s, handle %s", pvName, handle)
	tmpl := createTemplate(ctx, t, clients, ns)

	router, err := e2e.NewRouterClient(ctx)
	if err != nil {
		t.Fatalf("NewRouterClient: %v", err)
	}
	defer router.Close()

	actorA := resources.ActorRef{Atespace: atespace, Name: "session-a-" + ns.Name}
	actorB := resources.ActorRef{Atespace: atespace, Name: "session-b-" + ns.Name}
	plain := resources.ActorRef{Atespace: atespace, Name: "plain-" + ns.Name}
	for _, ref := range []resources.ActorRef{actorA, actorB, plain} {
		deleteActorAtEnd(t, clients, ref)
	}
	mirrorPath := mirrorsPath + "/" + mirrorFile

	// Session a's directory does not exist when the actor is created: the
	// read-write mount of it creates it, 0770.
	t.Run("MountsSubPathsAtDeclaredPaths", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, actorA, sessionVolumes(handle, "a"))
		requireContent(ctx, t, router, actorA, mirrorPath, mirrorContent)
		requireWrite(ctx, t, router, actorA, workspacePath+"/a.txt")
		requireContent(ctx, t, router, actorA, workspacePath+"/a.txt", probeWrittenContent)
		requireFiles(t, volumeFiles(ctx, t, clients, ns.Name, claim, "inspect-a"), "./sessions/a/a.txt", "./mirrors/"+mirrorFile)
		if mode := strings.TrimSpace(runOnVolume(ctx, t, clients, ns.Name, claim, "inspect-a-mode", "stat -c %a /data/sessions/a")); mode != "770" {
			t.Errorf("the created session directory has mode %s, want 770", mode)
		}
	})

	// Session b's directory was made ahead of the actor, as a caller may do.
	t.Run("ActorsWriteOnlyTheirOwnSubPath", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, actorB, sessionVolumes(handle, "b"))
		requireContent(ctx, t, router, actorB, mirrorPath, mirrorContent)
		requireAbsent(ctx, t, router, actorB, workspacePath+"/a.txt")
		requireWrite(ctx, t, router, actorB, workspacePath+"/b.txt")
		requireWriteRefused(ctx, t, router, actorB, mirrorsPath+"/b.txt")
		requireWriteRefused(ctx, t, router, actorA, mirrorsPath+"/a.txt")
		files := volumeFiles(ctx, t, clients, ns.Name, claim, "inspect-b")
		requireFiles(t, files, "./sessions/a/a.txt", "./sessions/b/b.txt")
		for _, f := range files {
			if strings.HasPrefix(f, "./mirrors/") && f != "./mirrors/"+mirrorFile {
				t.Errorf("the read-only mount wrote %s", f)
			}
		}
	})

	t.Run("NoReferenceNoMounts", func(t *testing.T) {
		startActor(ctx, t, clients, tmpl, plain, nil)
		requireAbsent(ctx, t, router, plain, mirrorPath)
		if got := probeJSON(ctx, t, router, plain, "/writefile?path="+workspacePath+"/plain.txt"); got["error"] == "" {
			t.Errorf("%s wrote %s/plain.txt: the unsupplied volume is mounted", plain, workspacePath)
		}
	})

	t.Run("PauseResumeDeleteLeaveTheVolume", func(t *testing.T) {
		if _, err := clients.SubstrateAPI.PauseActor(ctx, &ateapipb.PauseActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("PauseActor: %v", err)
		}
		eventually(t, 2*time.Minute, "actor A to pause", func() (bool, error) {
			actor, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: actorA.ToObjectRef()})
			if err != nil {
				return false, err
			}
			state := actor.GetStatus().GetState()
			return state == ateapipb.ActorState_ACTOR_STATE_PAUSED, fmt.Errorf("state %v", state)
		})
		if _, err := e2e.ResumeActorAwaitCapacity(t, ctx, clients, &ateapipb.ResumeActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("ResumeActor: %v", err)
		}
		requireContent(ctx, t, router, actorA, workspacePath+"/a.txt", probeWrittenContent)
		requireContent(ctx, t, router, actorA, mirrorPath, mirrorContent)

		// A running actor is not deletable; the suspend unmounts its volumes.
		if _, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("SuspendActor: %v", err)
		}
		if _, err := clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: actorA.ToObjectRef()}); err != nil {
			t.Fatalf("DeleteActor: %v", err)
		}
		eventually(t, 2*time.Minute, "actor A to be gone", func() (bool, error) {
			_, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: actorA.ToObjectRef()})
			return status.Code(err) == codes.NotFound, fmt.Errorf("GetActor: %v", err)
		})
		if _, err := clients.K8s.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{}); err != nil {
			t.Fatalf("PersistentVolume %s after the delete: %v", pvName, err)
		}
		requireFiles(t, volumeFiles(ctx, t, clients, ns.Name, claim, "inspect-after-delete"), "./sessions/a/a.txt", "./sessions/b/b.txt", "./mirrors/"+mirrorFile)
		// The other actor still works on the volume.
		requireContent(ctx, t, router, actorB, workspacePath+"/b.txt", probeWrittenContent)
	})

	t.Run("RefusesBadReferencesAtCreate", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			mod      func(ev *ateapipb.ExistingVolume)
			wantCode codes.Code
			wantMsg  string
		}{
			{
				name:     "unknown driver",
				mod:      func(ev *ateapipb.ExistingVolume) { ev.Driver = "unknown.csi.example.com" },
				wantCode: codes.FailedPrecondition,
				wantMsg:  `unknown driver "unknown.csi.example.com"`,
			},
			{
				name:     "missing volume",
				mod:      func(ev *ateapipb.ExistingVolume) { ev.VolumeHandle = "no-such-volume" },
				wantCode: codes.FailedPrecondition,
				wantMsg:  `no PersistentVolume of driver "nfs.csi.k8s.io" holds volume "no-such-volume"`,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				evs := sessionVolumes(handle, "refused")
				tc.mod(evs[0])
				ref := resources.ActorRef{Atespace: atespace, Name: "refused-" + ns.Name}
				_, err := createActor(ctx, clients, tmpl, ref, evs)
				if status.Code(err) != tc.wantCode || !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("CreateActor = %v, want %v containing %q", err, tc.wantCode, tc.wantMsg)
				}
				t.Logf("refused as expected: %v", err)
			})
		}
	})

	// A suspend that fails on the node crashes the actor while its sandbox is
	// still up, mounts and all. The crash tears the workload down and frees
	// the worker, the crashed record deletes, and nothing of the actor is left
	// on the node or holding the volume. Its own volume, so that the
	// PersistentVolume's deletion at the end proves the last point: the NFS
	// driver refuses to delete a volume a sandbox still has files open on.
	t.Run("DeleteAfterFailedSuspend", func(t *testing.T) {
		claim, pvName, handle := prepareVolume(ctx, t, clients, ns.Name, "failed-suspend")
		t.Logf("existing volume: PersistentVolume %s, handle %s", pvName, handle)
		ref := resources.ActorRef{Atespace: atespace, Name: "failed-suspend-" + ns.Name}
		deleteActorAtEnd(t, clients, ref)
		startActor(ctx, t, clients, tmpl, ref, sessionVolumes(handle, "c"))
		requireWrite(ctx, t, router, ref, workspacePath+"/c.txt")

		actor := getActor(ctx, t, clients, ref)
		uid := actor.GetMetadata().GetUid()
		assignment := actor.GetStatus().GetWorkerAssignment()
		node, worker := assignment.GetNodeName(), assignment.GetWorker().GetName()
		if uid == "" || node == "" || worker == "" {
			t.Fatalf("%s runs with uid %q on node %q, worker %q", ref, uid, node, worker)
		}
		// The inspection has to see the running actor, or its later silence
		// proves nothing.
		if before := inspectNode(ctx, t, clients, ns.Name, node, uid, "inspect-running"); !before.Dir || before.Mounts == 0 || before.Procs == 0 {
			t.Fatalf("node %s shows nothing of the running actor (%+v): the inspection cannot prove a teardown", node, before)
		}
		allocated := allocatedActors(ctx, t, clients, worker)

		blockCheckpoint(ctx, t, clients, ns.Name, node, uid)
		_, err := clients.SubstrateAPI.SuspendActor(ctx, &ateapipb.SuspendActorRequest{Actor: ref.ToObjectRef()})
		if err == nil {
			t.Fatal("SuspendActor succeeded with the actor's checkpoint directory blocked")
		}
		t.Logf("SuspendActor failed as arranged: %v", err)
		eventually(t, time.Minute, "the actor to be CRASHED", func() (bool, error) {
			actor = getActor(ctx, t, clients, ref)
			state := actor.GetStatus().GetState()
			return state == ateapipb.ActorState_ACTOR_STATE_CRASHED, fmt.Errorf("state %v", state)
		})
		if msg := actor.GetStatus().GetCrash().GetMessage(); !strings.Contains(msg, "Checkpoint") {
			t.Errorf("crash message %q, want the failed Checkpoint", msg)
		}
		if got := actor.GetStatus().GetWorkerAssignment(); got != nil {
			t.Errorf("the crashed actor still holds its worker (%v): the crash did not tear its workload down", got)
		}
		if after := inspectNode(ctx, t, clients, ns.Name, node, uid, "inspect-crashed"); after.Dir || after.Mounts != 0 || after.Procs != 0 {
			t.Errorf("node %s still holds the crashed actor: %+v", node, after)
		}
		if got := allocatedActors(ctx, t, clients, worker); got != allocated-1 {
			t.Errorf("worker %s allocated actors = %d after the crash, want %d", worker, got, allocated-1)
		}

		if _, err := clients.SubstrateAPI.DeleteActor(ctx, &ateapipb.DeleteActorRequest{Actor: ref.ToObjectRef()}); err != nil {
			t.Fatalf("DeleteActor: %v", err)
		}
		eventually(t, time.Minute, "the actor to be gone", func() (bool, error) {
			_, err := clients.SubstrateAPI.GetActor(ctx, &ateapipb.GetActorRequest{Actor: ref.ToObjectRef()})
			return status.Code(err) == codes.NotFound, fmt.Errorf("GetActor: %v", err)
		})
		if final := inspectNode(ctx, t, clients, ns.Name, node, uid, "inspect-deleted"); final.Dir || final.Mounts != 0 || final.Procs != 0 {
			t.Errorf("node %s still holds the deleted actor: %+v", node, final)
		}

		// Nothing of the actor holds the volume: its PersistentVolume deletes
		// with the claim.
		if err := clients.K8s.CoreV1().PersistentVolumeClaims(ns.Name).Delete(ctx, claim, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("deleting PVC %s: %v", claim, err)
		}
		eventually(t, 2*time.Minute, "PersistentVolume "+pvName+" to be deleted", func() (bool, error) {
			pv, err := clients.K8s.CoreV1().PersistentVolumes().Get(ctx, pvName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return false, fmt.Errorf("phase %s", pv.Status.Phase)
		})
	})
}
