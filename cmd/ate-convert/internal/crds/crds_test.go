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

package crds

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

var opts = Options{WorkerImage: "ghcr.io/giantswarm/ateworker-gvisor:2.0.0", SandboxVersion: "v1alpha1"}

func obj(t *testing.T, kind string, o map[string]any) *unstructured.Unstructured {
	t.Helper()
	u := &unstructured.Unstructured{Object: o}
	u.SetAPIVersion("ate.dev/v1alpha1")
	u.SetKind(kind)
	return u
}

func workerPool(t *testing.T) *unstructured.Unstructured {
	return obj(t, "WorkerPool", map[string]any{
		"metadata": map[string]any{
			"name": "gvisor", "namespace": "ate-system", "uid": "x", "resourceVersion": "7", "generation": int64(3),
			"labels": map[string]any{"app.kubernetes.io/managed-by": "Helm"}, "annotations": map[string]any{"meta.helm.sh/release-name": "substrate"},
			"finalizers": []any{"ate.dev/workers"},
		},
		"spec": map[string]any{
			"replicas": int64(2), "sandboxClass": "gvisor", "workerImage": "ghcr.io/giantswarm/ateom-gvisor:1.18.0",
			"template": map[string]any{"nodeSelector": map[string]any{"sandbox": "gvisor"}},
		},
		"status": map[string]any{"replicas": int64(2), "workerImage": "old"},
	})
}

func sandboxConfig(t *testing.T) *unstructured.Unstructured {
	return obj(t, "SandboxConfig", map[string]any{
		"metadata": map[string]any{"name": "gvisor-default", "annotations": map[string]any{"sandboxconfig.ate.dev/is-class-default": "true"}},
		"spec": map[string]any{
			"sandboxClass": "gvisor", "pauseImage": "pause@sha256:x",
			"assets": map[string]any{"amd64": map[string]any{"gvisor": map[string]any{"url": "gs://gvisor/x", "sha256": "ab"}}},
		},
	})
}

func csiDriverConfig(t *testing.T) *unstructured.Unstructured {
	return obj(t, "CSIDriverConfig", map[string]any{
		"metadata": map[string]any{"name": "nfs"},
		"spec":     map[string]any{"driverName": "nfs.csi.k8s.io", "controllerEndpoint": "dns:///nfs:443", "tls": map[string]any{"enabled": true}},
	})
}

func TestConvertObjects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  int
		in    *unstructured.Unstructured
		want  map[string]any
		notes []string
	}{{
		name: "WorkerPool",
		kind: 2,
		in:   workerPool(t),
		want: map[string]any{
			"apiVersion": "ate.dev/v1", "kind": "WorkerPool",
			"metadata": map[string]any{
				"name": "gvisor", "namespace": "ate-system",
				"labels": map[string]any{"app.kubernetes.io/managed-by": "Helm"}, "annotations": map[string]any{"meta.helm.sh/release-name": "substrate"},
			},
			"spec": map[string]any{
				"replicas": int64(2), "workerImage": opts.WorkerImage,
				"sandboxClasses": []any{map[string]any{"name": "gvisor"}},
				"template":       map[string]any{"nodeSelector": map[string]any{"sandbox": "gvisor"}},
			},
		},
	}, {
		name: "SandboxConfig",
		kind: 1,
		in:   sandboxConfig(t),
		want: map[string]any{
			"apiVersion": "ate.dev/v1", "kind": "SandboxConfig",
			"metadata": map[string]any{"name": "gvisor-default"},
			"spec": map[string]any{
				"sandboxClass": "gvisor", "defaultVersion": "v1alpha1",
				"versions": []any{map[string]any{"name": "v1alpha1", "state": "Enabled", "assets": map[string]any{"amd64": map[string]any{"gvisor": map[string]any{"url": "gs://gvisor/x", "sha256": "ab"}}}}},
			},
		},
		notes: []string{"sandboxconfig.ate.dev/is-class-default removed", "spec.pauseImage dropped: 2.x sandboxes have no pause container"},
	}, {
		name: "CSIDriverConfig",
		kind: 0,
		in:   csiDriverConfig(t),
		want: map[string]any{
			"apiVersion": "ate.dev/v1", "kind": "CSIDriverConfig",
			"metadata": map[string]any{"name": "nfs"},
			"spec":     map[string]any{"driverName": "nfs.csi.k8s.io", "controllerEndpoint": "dns:///nfs:443", "tls": map[string]any{"enabled": true}},
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, notes, err := Kinds[tc.kind].convert(tc.in, opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Object)
			require.Equal(t, tc.notes, notes)
		})
	}

	t.Run("WorkerPool without a class defaults to gvisor", func(t *testing.T) {
		wp := workerPool(t)
		unstructured.RemoveNestedField(wp.Object, "spec", "sandboxClass")
		got, _, err := convertWorkerPool(wp, opts)
		require.NoError(t, err)
		classes, _, _ := unstructured.NestedSlice(got.Object, "spec", "sandboxClasses")
		require.Equal(t, []any{map[string]any{"name": "gvisor"}}, classes)
	})
	t.Run("WorkerPool without a worker image", func(t *testing.T) {
		_, _, err := convertWorkerPool(workerPool(t), Options{})
		require.ErrorContains(t, err, "worker image")
	})
}

func crd(k Kind, versions ...string) *apiextensionsv1.CustomResourceDefinition {
	c := &apiextensionsv1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: k.crdName()}}
	for i, v := range versions {
		c.Spec.Versions = append(c.Spec.Versions, apiextensionsv1.CustomResourceDefinitionVersion{Name: v, Served: true, Storage: i == 0})
	}
	c.Status.StoredVersions = []string{versions[0]}
	return c
}

func TestPrepareAndRestore(t *testing.T) {
	ctx := t.Context()
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{}
	for _, k := range Kinds {
		for _, v := range []string{v1alpha1, v1} {
			listKinds[k.gvr(v)] = map[string]string{"workerpools": "WorkerPoolList", "sandboxconfigs": "SandboxConfigList", "csidriverconfigs": "CSIDriverConfigList"}[k.Plural]
		}
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, workerPool(t), sandboxConfig(t), csiDriverConfig(t))
	ext := apiextensionsfake.NewSimpleClientset(crd(Kinds[0], v1alpha1), crd(Kinds[1], v1alpha1), crd(Kinds[2], v1alpha1))
	c := Clients{CRDs: ext, Dynamic: dyn}
	dir := t.TempDir()

	var log bytes.Buffer
	require.NoError(t, Prepare(ctx, c, dir, opts, true, &log))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "a dry run writes no export")
	_, err = dyn.Resource(Kinds[2].gvr(v1alpha1)).Namespace("ate-system").Get(ctx, "gvisor", metav1.GetOptions{})
	require.NoError(t, err, "a dry run deletes nothing")

	require.NoError(t, Prepare(ctx, c, dir, opts, false, &log))
	for _, k := range Kinds {
		got, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, k.crdName(), metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, []string{v1}, got.Status.StoredVersions)
		require.Len(t, got.Spec.Versions, 2)
		require.False(t, got.Spec.Versions[0].Storage)
		require.True(t, got.Spec.Versions[1].Storage)
		list, err := dyn.Resource(k.gvr(v1alpha1)).List(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.Empty(t, list.Items)
	}
	// A second run finds the CRDs done.
	require.NoError(t, Prepare(ctx, c, dir, opts, false, &log))

	require.ErrorContains(t, Restore(ctx, c, dir, false, &log), "apply the v1 CRD first")
	for _, k := range Kinds {
		got, err := ext.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, k.crdName(), metav1.GetOptions{})
		require.NoError(t, err)
		got.Spec.Versions = got.Spec.Versions[1:]
		_, err = ext.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, got, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	require.NoError(t, Restore(ctx, c, dir, false, &log))
	wp, err := dyn.Resource(Kinds[2].gvr(v1)).Namespace("ate-system").Get(ctx, "gvisor", metav1.GetOptions{})
	require.NoError(t, err)
	image, _, _ := unstructured.NestedString(wp.Object, "spec", "workerImage")
	require.Equal(t, opts.WorkerImage, image)
	require.NoError(t, Restore(ctx, c, dir, false, &log), "a second restore leaves existing objects")
	require.Contains(t, log.String(), "workerpools ate-system/gvisor: exists, left as it is")
}
