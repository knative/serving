/*
Copyright 2017 Google Inc. All Rights Reserved.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/mattbaird/jsonpatch"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func newDefaultOptions() ControllerOptions {
	return ControllerOptions{
		ServiceName:      "ela-webhook",
		ServiceNamespace: "ela-system",
		Port:             443,
		SecretName:       "ela-webhook-certs",
		WebhookName:      "webhook.elafros.dev",
	}
}

const (
	testNamespace    = "test-namespace"
	rtName           = "test-revision-template"
	imageName        = "test-container-image"
	envVarName       = "envname"
	envVarValue      = "envvalue"
	testDomain       = "example.com"
	testGeneration   = 1
	esName           = "test-route-name"
	testRevisionName = "test-revision"
)

func newRunningTestAdmissionController(t *testing.T, options ControllerOptions) (
	kubeClient *fakekubeclientset.Clientset,
	ac *AdmissionController,
	stopCh chan struct{}) {
	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()

	ac, err := NewAdmissionController(kubeClient, options)
	if err != nil {
		t.Fatalf("Failed to create new admission controller: %s", err)
	}
	stopCh = make(chan struct{})
	go func() {
		if err := ac.Run(stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()
	ac.Run(stopCh)
	return
}

func newNonRunningTestAdmissionController(t *testing.T, options ControllerOptions) (
	kubeClient *fakekubeclientset.Clientset,
	ac *AdmissionController) {
	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()

	ac, err := NewAdmissionController(kubeClient, options)
	if err != nil {
		t.Fatalf("Failed to create new admission controller: %s", err)
	}
	return
}

func TestDeleteAllowed(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())

	req := admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Delete,
	}

	resp := ac.admit(&req)
	if !resp.Allowed {
		t.Fatalf("unexpected denial of delete")
	}
}

func TestConnectAllowed(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())

	req := admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Connect,
	}

	resp := ac.admit(&req)
	if !resp.Allowed {
		t.Fatalf("unexpected denial of connect")
	}
}

func TestUnknownKindFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())

	req := admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Garbage"},
	}

	assertFailsWith(t, ac.admit(&req), "unhandled kind")
}

func TestValidNewRTObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(createValidCreateRT())
	assertAllowed(t, resp)
	p := incrementGenerationPatch(0)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidRTNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(1)
	new := createConfiguration(1)
	resp := ac.admit(createUpdateRT(&old, &new))
	assertAllowed(t, resp)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidRTEnvChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(1)
	new := createConfiguration(1)
	new.Spec.Template.Spec.Env = []corev1.EnvVar{
		corev1.EnvVar{
			Name:  envVarName,
			Value: "different",
		},
	}
	resp := ac.admit(createUpdateRT(&old, &new))
	assertAllowed(t, resp)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/generation",
			Value:     2,
		},
	})
}

func TestValidNewESObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(createValidCreateES())
	assertAllowed(t, resp)
	p := incrementGenerationPatch(0)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidESNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1)
	new := createRoute(1)
	resp := ac.admit(createUpdateES(&old, &new))
	assertAllowed(t, resp)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidESChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1)
	new := createRoute(1)
	new.Spec.Traffic = []v1alpha1.TrafficTarget{
		v1alpha1.TrafficTarget{
			Revision: testRevisionName,
			Percent:  100,
		},
	}
	resp := ac.admit(createUpdateES(&old, &new))
	assertAllowed(t, resp)
	assertPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/generation",
			Value:     2,
		},
	})
}

func createUpdateRT(old, new *v1alpha1.Configuration) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateRT()
	marshaled, err := yaml.Marshal(old)
	if err != nil {
		panic("failed to marshal configuration")
	}
	req.Object.Raw = marshaled
	marshaledOld, err := yaml.Marshal(new)
	if err != nil {
		panic("failed to marshal configuration")
	}
	req.OldObject.Raw = marshaledOld
	return req
}

func createValidCreateRT() *admissionv1beta1.AdmissionRequest {
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
	rt := createConfiguration(0)
	marshaled, err := yaml.Marshal(rt)
	if err != nil {
		panic("failed to marshal configuration")
	}
	req.Object.Raw = marshaled
	return req
}

func createBaseUpdateRT() *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Update,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
}

func createValidCreateES() *admissionv1beta1.AdmissionRequest {
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
	rt := createRoute(0)
	marshaled, err := yaml.Marshal(rt)
	if err != nil {
		panic("failed to marshal route")
	}
	req.Object.Raw = marshaled
	return req
}

func createBaseUpdateES() *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Update,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
}

func createUpdateES(old, new *v1alpha1.Route) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateES()
	marshaled, err := yaml.Marshal(old)
	if err != nil {
		panic("failed to marshal route")
	}
	req.Object.Raw = marshaled
	marshaledOld, err := yaml.Marshal(new)
	if err != nil {
		panic("failed to marshal route")
	}
	req.OldObject.Raw = marshaledOld
	return req
}

func assertAllowed(t *testing.T, resp *admissionv1beta1.AdmissionResponse) {
	if !resp.Allowed {
		t.Fatalf("Expected allowed, but failed with %+v", resp.Result)
	}
}

func assertFailsWith(t *testing.T, resp *admissionv1beta1.AdmissionResponse, contains string) {
	if resp.Allowed {
		t.Fatalf("expected denial, got allowed")
	}
	if !strings.Contains(resp.Result.Message, contains) {
		t.Fatalf("expected failure containing %q got %q", contains, resp.Result.Message)
	}
}

func assertPatches(t *testing.T, a []byte, e []jsonpatch.JsonPatchOperation) {
	var actual []jsonpatch.JsonPatchOperation
	// Keep track of the patches we've found
	foundExpected := make([]bool, len(e))
	foundActual := make([]bool, len(e))

	err := json.Unmarshal(a, &actual)
	if err != nil {
		t.Fatalf("failed to unmarshal patches: %s", err)
	}
	if len(actual) != len(e) {
		t.Fatalf("unexpected number of patches %d expected %d\n%+v\n%+v", len(actual), len(e), actual, e)
	}
	// Make sure all the expected patches are found
	for i, expectedPatch := range e {
		for j, actualPatch := range actual {
			if actualPatch.Json() == expectedPatch.Json() {
				foundExpected[i] = true
				foundActual[j] = true
			} else {
				t.Fatalf("Values don't match: %+v vs %+v", actualPatch.Value, expectedPatch.Value)
			}
		}
	}
	for i, f := range foundExpected {
		if !f {
			t.Fatalf("did not find %+v in actual patches: %q", e[i], actual)
		}
	}
	for i, f := range foundActual {
		if !f {
			t.Fatalf("Extra patch found %+v in expected patches: %q", a[i], e)
		}
	}
}

func createConfiguration(generation int64) v1alpha1.Configuration {
	return v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      rtName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: generation,
			Template: v1alpha1.Revision{
				Spec: v1alpha1.RevisionSpec{
					ContainerSpec: &v1alpha1.ContainerSpec{
						Image: imageName,
					},
					Env: []corev1.EnvVar{
						corev1.EnvVar{
							Name:  envVarName,
							Value: envVarValue,
						},
					},
				},
			},
		},
	}
}

func createRoute(generation int64) v1alpha1.Route {
	return v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      rtName,
		},
		Spec: v1alpha1.RouteSpec{
			Generation:   generation,
			DomainSuffix: testDomain,
			Traffic: []v1alpha1.TrafficTarget{
				v1alpha1.TrafficTarget{
					Name:     esName,
					Revision: testRevisionName,
					Percent:  100,
				},
			},
		},
	}
}

func incrementGenerationPatch(old int64) jsonpatch.JsonPatchOperation {
	return jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/spec/generation",
		Value:     old + 1,
	}
}
