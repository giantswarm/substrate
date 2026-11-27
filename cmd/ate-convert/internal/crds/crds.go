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

// Package crds moves the WorkerPool, SandboxConfig and CSIDriverConfig
// objects of a 1.x install from ate.dev/v1alpha1 to ate.dev/v1. The v1 CRDs
// serve only v1 and there is no conversion webhook, so the objects are
// exported, deleted, the CRDs' storedVersions set to [v1] through a
// transitional CRD, and the objects created again in the v1 shape once the
// v1 CRDs are applied.
package crds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
)

const (
	group    = "ate.dev"
	v1alpha1 = "v1alpha1"
	v1       = "v1"

	classDefaultAnnotation = "sandboxconfig.ate.dev/is-class-default"
)

// Kind is one CRD this package migrates.
type Kind struct {
	Plural  string
	convert func(*unstructured.Unstructured, Options) (*unstructured.Unstructured, []string, error)
}

// Options are the values the v1 shape needs and the v1alpha1 objects lack.
type Options struct {
	// WorkerImage replaces every WorkerPool's spec.workerImage: the 1.x
	// worker image does not run 2.x actors.
	WorkerImage string
	// SandboxVersion names the version a SandboxConfig's 1.x assets become.
	SandboxVersion string
}

// Kinds are the CRDs to migrate, in the order objects are created again.
var Kinds = []Kind{
	{Plural: "csidriverconfigs", convert: convertCSIDriverConfig},
	{Plural: "sandboxconfigs", convert: convertSandboxConfig},
	{Plural: "workerpools", convert: convertWorkerPool},
}

func (k Kind) crdName() string { return k.Plural + "." + group }

func (k Kind) gvr(version string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: k.Plural}
}

// v1Object starts the v1 object of o: its identity, labels, annotations and
// owner references. Status and server-set metadata are dropped; finalizers
// are added again by the controllers that own them.
func v1Object(o *unstructured.Unstructured) *unstructured.Unstructured {
	out := &unstructured.Unstructured{Object: map[string]any{}}
	out.SetAPIVersion(group + "/" + v1)
	out.SetKind(o.GetKind())
	out.SetName(o.GetName())
	out.SetNamespace(o.GetNamespace())
	out.SetLabels(o.GetLabels())
	out.SetAnnotations(o.GetAnnotations())
	out.SetOwnerReferences(o.GetOwnerReferences())
	return out
}

func spec(o *unstructured.Unstructured) (map[string]any, error) {
	s, found, err := unstructured.NestedMap(o.Object, "spec")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("no spec")
	}
	return s, nil
}

func convertCSIDriverConfig(o *unstructured.Unstructured, _ Options) (*unstructured.Unstructured, []string, error) {
	s, err := spec(o)
	if err != nil {
		return nil, nil, err
	}
	out := v1Object(o)
	return out, nil, unstructured.SetNestedMap(out.Object, s, "spec")
}

func convertSandboxConfig(o *unstructured.Unstructured, opts Options) (*unstructured.Unstructured, []string, error) {
	s, err := spec(o)
	if err != nil {
		return nil, nil, err
	}
	if opts.SandboxVersion == "" {
		return nil, nil, errors.New("a SandboxConfig needs a version name for its assets")
	}
	var notes []string
	out := v1Object(o)
	// The 1.x assets are a gVisor that does not restore 2.x snapshots, so
	// the object must not become the default of its class over the 2.x
	// installer's.
	if ann := out.GetAnnotations(); ann[classDefaultAnnotation] != "" {
		delete(ann, classDefaultAnnotation)
		if len(ann) == 0 {
			ann = nil
		}
		out.SetAnnotations(ann)
		notes = append(notes, classDefaultAnnotation+" removed")
	}
	v1Spec := map[string]any{"defaultVersion": opts.SandboxVersion}
	if class, ok := s["sandboxClass"]; ok {
		v1Spec["sandboxClass"] = class
	}
	version := map[string]any{"name": opts.SandboxVersion, "state": "Enabled"}
	if assets, ok := s["assets"]; ok {
		version["assets"] = assets
	}
	v1Spec["versions"] = []any{version}
	if _, ok := s["pauseImage"]; ok {
		notes = append(notes, "spec.pauseImage dropped: 2.x sandboxes have no pause container")
	}
	return out, notes, unstructured.SetNestedMap(out.Object, v1Spec, "spec")
}

func convertWorkerPool(o *unstructured.Unstructured, opts Options) (*unstructured.Unstructured, []string, error) {
	s, err := spec(o)
	if err != nil {
		return nil, nil, err
	}
	if opts.WorkerImage == "" {
		return nil, nil, errors.New("a WorkerPool needs the 2.x worker image")
	}
	out := v1Object(o)
	class, _ := s["sandboxClass"].(string)
	if class == "" {
		class = "gvisor"
	}
	delete(s, "sandboxClass")
	s["sandboxClasses"] = []any{map[string]any{"name": class}}
	s["workerImage"] = opts.WorkerImage
	return out, nil, unstructured.SetNestedMap(out.Object, s, "spec")
}

// Clients are the API clients the migration uses.
type Clients struct {
	CRDs    apiextensions.Interface
	Dynamic dynamic.Interface
}

// export is what Prepare saves of one kind, so a rerun after the objects are
// deleted still has them.
type export struct {
	V1alpha1 []map[string]any `json:"v1alpha1"`
	V1       []map[string]any `json:"v1"`
}

func exportPath(dir string, k Kind) string { return filepath.Join(dir, k.Plural+".json") }

// Prepare exports and converts every object, and unless dryRun deletes the
// objects and sets each CRD's storedVersions to [v1]. A kind whose CRD
// stores v1 only already is skipped. The export it writes to stateDir is
// reused by a later run, never overwritten; a dry run writes none.
func Prepare(ctx context.Context, c Clients, stateDir string, opts Options, dryRun bool, log io.Writer) error {
	for _, k := range Kinds {
		crd, err := c.CRDs.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, k.crdName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if slices.Equal(crd.Status.StoredVersions, []string{v1}) {
			fmt.Fprintf(log, "%s: stores v1 only\n", k.Plural)
			continue
		}
		exp, err := loadOrExport(ctx, c, stateDir, k, opts, !dryRun, log)
		if err != nil {
			return fmt.Errorf("%s: %w", k.Plural, err)
		}
		fmt.Fprintf(log, "%s: %d objects exported to %s\n", k.Plural, len(exp.V1alpha1), exportPath(stateDir, k))
		if dryRun {
			continue
		}
		if err := deleteAll(ctx, c, k, exp); err != nil {
			return fmt.Errorf("%s: %w", k.Plural, err)
		}
		if err := storeV1(ctx, c, k); err != nil {
			return fmt.Errorf("%s: %w", k.Plural, err)
		}
		fmt.Fprintf(log, "%s: objects deleted, storedVersions [v1]\n", k.Plural)
	}
	return nil
}

func loadOrExport(ctx context.Context, c Clients, stateDir string, k Kind, opts Options, save bool, log io.Writer) (*export, error) {
	path := exportPath(stateDir, k)
	if raw, err := os.ReadFile(path); err == nil {
		exp := &export{}
		return exp, json.Unmarshal(raw, exp)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	list, err := c.Dynamic.Resource(k.gvr(v1alpha1)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	exp := &export{}
	for i := range list.Items {
		o := &list.Items[i]
		conv, notes, err := k.convert(o, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", objectName(o), err)
		}
		for _, n := range notes {
			fmt.Fprintf(log, "%s %s: %s\n", k.Plural, objectName(o), n)
		}
		exp.V1alpha1 = append(exp.V1alpha1, o.Object)
		exp.V1 = append(exp.V1, conv.Object)
	}
	if !save {
		return exp, nil
	}
	raw, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	return exp, os.WriteFile(path, raw, 0o600)
}

func objectName(o *unstructured.Unstructured) string {
	if o.GetNamespace() == "" {
		return o.GetName()
	}
	return o.GetNamespace() + "/" + o.GetName()
}

// deleteAll deletes the exported objects and waits until they are gone.
// Their finalizers are removed first: the controllers that would act on
// them are stopped.
func deleteAll(ctx context.Context, c Clients, k Kind, exp *export) error {
	for _, raw := range exp.V1alpha1 {
		o := &unstructured.Unstructured{Object: raw}
		r := c.Dynamic.Resource(k.gvr(v1alpha1)).Namespace(o.GetNamespace())
		cur, err := r.Get(ctx, o.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if len(cur.GetFinalizers()) > 0 {
			cur.SetFinalizers(nil)
			if _, err := r.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
		if err := r.Delete(ctx, o.GetName(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			_, err := r.Get(ctx, o.GetName(), metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}); err != nil {
			return fmt.Errorf("waiting for %s to go: %w", objectName(o), err)
		}
	}
	return nil
}

// storeV1 makes v1 the CRD's storage version with an open schema next to
// the served v1alpha1, then sets storedVersions to [v1]. No object exists
// any more, so none is stored at v1alpha1. Applying the v1 CRDs then drops
// v1alpha1.
func storeV1(ctx context.Context, c Clients, k Kind) error {
	crds := c.CRDs.ApiextensionsV1().CustomResourceDefinitions()
	crd, err := crds.Get(ctx, k.crdName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	crd.Spec.Versions = transitionalVersions(crd.Spec.Versions)
	if crd, err = crds.Update(ctx, crd, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating the CRD to store v1: %w", err)
	}
	crd.Status.StoredVersions = []string{v1}
	if _, err := crds.UpdateStatus(ctx, crd, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("setting storedVersions: %w", err)
	}
	return nil
}

// transitionalVersions keeps v1alpha1 served but not stored, and adds a
// stored v1 that accepts any object if the CRD has none.
func transitionalVersions(versions []apiextensionsv1.CustomResourceDefinitionVersion) []apiextensionsv1.CustomResourceDefinitionVersion {
	out := slices.Clone(versions)
	hasV1 := false
	for i := range out {
		out[i].Storage = out[i].Name == v1
		hasV1 = hasV1 || out[i].Name == v1
	}
	if !hasV1 {
		out = append(out, apiextensionsv1.CustomResourceDefinitionVersion{
			Name:    v1,
			Served:  true,
			Storage: true,
			Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
				Type:                   "object",
				XPreserveUnknownFields: ptr.To(true),
			}},
			Subresources: &apiextensionsv1.CustomResourceSubresources{Status: &apiextensionsv1.CustomResourceSubresourceStatus{}},
		})
	}
	return out
}

// Restore creates the converted objects once the v1 CRDs are applied. An
// object that exists already, such as one a Helm upgrade created, is left
// as it is.
func Restore(ctx context.Context, c Clients, stateDir string, dryRun bool, log io.Writer) error {
	for _, k := range Kinds {
		crd, err := c.CRDs.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, k.crdName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(crd.Spec.Versions) != 1 || crd.Spec.Versions[0].Name != v1 {
			return fmt.Errorf("%s: apply the v1 CRD first", k.crdName())
		}
		raw, err := os.ReadFile(exportPath(stateDir, k))
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(log, "%s: nothing exported\n", k.Plural)
			continue
		}
		if err != nil {
			return err
		}
		exp := &export{}
		if err := json.Unmarshal(raw, exp); err != nil {
			return err
		}
		for _, obj := range exp.V1 {
			o := &unstructured.Unstructured{Object: obj}
			if dryRun {
				fmt.Fprintf(log, "%s %s: would create\n", k.Plural, objectName(o))
				continue
			}
			_, err := c.Dynamic.Resource(k.gvr(v1)).Namespace(o.GetNamespace()).Create(ctx, o, metav1.CreateOptions{})
			switch {
			case apierrors.IsAlreadyExists(err):
				fmt.Fprintf(log, "%s %s: exists, left as it is\n", k.Plural, objectName(o))
			case err != nil:
				return fmt.Errorf("%s %s: %w", k.Plural, objectName(o), err)
			default:
				fmt.Fprintf(log, "%s %s: created\n", k.Plural, objectName(o))
			}
		}
	}
	return nil
}
