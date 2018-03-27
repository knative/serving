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
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/ghodss/yaml"
	"github.com/mattbaird/jsonpatch"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
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
	testNamespace         = "test-namespace"
	testConfigurationName = "test-configuration"
	imageName             = "test-container-image"
	envVarName            = "envname"
	envVarValue           = "envvalue"
	testGeneration        = 1
	testRouteName         = "test-route-name"
	testRevisionName      = "test-revision"
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

	expectFailsWith(t, ac.admit(&req), "unhandled kind")
}

func TestInvalidNewConfigurationFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	new := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
	expectFailsWith(t, ac.admit(new), errInvalidConfigurationInput.Error())
}

func TestInvalidNewConfigurationNameFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
	invalidName := "configuration.example"
	config := createConfiguration(0, invalidName)
	marshaled, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(req), "Invalid resource name")

	invalidName = strings.Repeat("a", 64)
	config = createConfiguration(0, invalidName)
	marshaled, err = yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(req), "Invalid resource name")
}

func TestValidNewConfigurationObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(createValidCreateConfiguration())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidConfigurationNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(testGeneration, testConfigurationName)
	new := createConfiguration(testGeneration, testConfigurationName)
	resp := ac.admit(createUpdateConfiguration(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidConfigurationEnvChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(testGeneration, testConfigurationName)
	new := createConfiguration(testGeneration, testConfigurationName)
	new.Spec.RevisionTemplate.Spec.Container.Env = []corev1.EnvVar{
		corev1.EnvVar{
			Name:  envVarName,
			Value: "different",
		},
	}
	resp := ac.admit(createUpdateConfiguration(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/generation",
			Value:     2,
		},
	})
}

func TestInvalidNewRouteNameFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
	invalidName := "configuration.example"
	config := createRoute(0, invalidName)
	marshaled, err := yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(req), "Invalid resource name")

	invalidName = strings.Repeat("a", 64)
	config = createRoute(0, invalidName)
	marshaled, err = yaml.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(req), "Invalid resource name")
}

func TestValidNewRouteObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(createValidCreateRoute())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidRouteNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1, testRouteName)
	new := createRoute(1, testRouteName)
	resp := ac.admit(createUpdateRoute(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidRouteChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1, testRouteName)
	new := createRoute(1, testRouteName)
	new.Spec.Traffic = []v1alpha1.TrafficTarget{
		v1alpha1.TrafficTarget{
			RevisionName: testRevisionName,
			Percent:      100,
		},
	}
	resp := ac.admit(createUpdateRoute(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{
		jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/spec/generation",
			Value:     2,
		},
	})
}

func TestValidWebhook(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	createDeployment(ac)
	ac.register(ac.client.Admissionregistration().MutatingWebhookConfigurations(), []byte{})
	_, err := ac.client.Admissionregistration().MutatingWebhookConfigurations().Get(ac.options.WebhookName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to create webhook: %s", err)
	}
}

func TestUpdatingWebhook(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	webhook := &admissionregistrationv1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: ac.options.WebhookName,
		},
		Webhooks: []admissionregistrationv1beta1.Webhook{
			{
				Name:         ac.options.WebhookName,
				Rules:        []admissionregistrationv1beta1.RuleWithOperations{{}},
				ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
			},
		},
	}

	createDeployment(ac)
	createWebhook(ac, webhook)
	ac.register(ac.client.Admissionregistration().MutatingWebhookConfigurations(), []byte{})
	currentWebhook, _ := ac.client.Admissionregistration().MutatingWebhookConfigurations().Get(ac.options.WebhookName, metav1.GetOptions{})
	if reflect.DeepEqual(currentWebhook.Webhooks, webhook.Webhooks) {
		t.Fatalf("Expected webhook to be updated")
	}
}

func createUpdateConfiguration(old, new *v1alpha1.Configuration) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateConfiguration()
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

func createValidCreateConfiguration() *admissionv1beta1.AdmissionRequest {
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
	config := createConfiguration(0, testConfigurationName)
	marshaled, err := yaml.Marshal(config)
	if err != nil {
		panic("failed to marshal configuration")
	}
	req.Object.Raw = marshaled
	return req
}

func createBaseUpdateConfiguration() *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Update,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
}

func createValidCreateRoute() *admissionv1beta1.AdmissionRequest {
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
	route := createRoute(0, testRouteName)
	marshaled, err := yaml.Marshal(route)
	if err != nil {
		panic("failed to marshal route")
	}
	req.Object.Raw = marshaled
	return req
}

func createBaseUpdateRoute() *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Update,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
}

func createUpdateRoute(old, new *v1alpha1.Route) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateRoute()
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

func createDeployment(ac *AdmissionController) {
	deployment := &v1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      elaWebhookDeployment,
			Namespace: elaSystemNamespace,
		},
	}
	ac.client.ExtensionsV1beta1().Deployments(elaSystemNamespace).Create(deployment)
}

func createWebhook(ac *AdmissionController, webhook *admissionregistrationv1beta1.MutatingWebhookConfiguration) {
	client := ac.client.Admissionregistration().MutatingWebhookConfigurations()
	_, err := client.Create(webhook)
	if err != nil {
		panic(fmt.Sprintf("failed to create test webhook: %s", err))
	}
}

func expectAllowed(t *testing.T, resp *admissionv1beta1.AdmissionResponse) {
	if !resp.Allowed {
		t.Errorf("Expected allowed, but failed with %+v", resp.Result)
	}
}

func expectFailsWith(t *testing.T, resp *admissionv1beta1.AdmissionResponse, contains string) {
	if resp.Allowed {
		t.Errorf("expected denial, got allowed")
		return
	}
	if !strings.Contains(resp.Result.Message, contains) {
		t.Errorf("expected failure containing %q got %q", contains, resp.Result.Message)
	}
}

func expectPatches(t *testing.T, a []byte, e []jsonpatch.JsonPatchOperation) {
	var actual []jsonpatch.JsonPatchOperation
	// Keep track of the patches we've found
	foundExpected := make([]bool, len(e))
	foundActual := make([]bool, len(e))

	err := json.Unmarshal(a, &actual)
	if err != nil {
		t.Errorf("failed to unmarshal patches: %s", err)
		return
	}
	if len(actual) != len(e) {
		t.Errorf("unexpected number of patches %d expected %d\n%+v\n%+v", len(actual), len(e), actual, e)
	}
	// Make sure all the expected patches are found
	for i, expectedPatch := range e {
		for j, actualPatch := range actual {
			if actualPatch.Json() == expectedPatch.Json() {
				foundExpected[i] = true
				foundActual[j] = true
			} else {
				t.Errorf("Values don't match: %+v vs %+v", actualPatch.Value, expectedPatch.Value)
			}
		}
	}
	for i, f := range foundExpected {
		if !f {
			t.Errorf("did not find %+v in actual patches: %q", e[i], actual)
		}
	}
	for i, f := range foundActual {
		if !f {
			t.Errorf("Extra patch found %+v in expected patches: %q", a[i], e)
		}
	}
}

func createConfiguration(generation int64, configurationName string) v1alpha1.Configuration {
	return v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      configurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: generation,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: &corev1.Container{
						Image: imageName,
						Env: []corev1.EnvVar{{
							Name:  envVarName,
							Value: envVarValue,
						}},
					},
				},
			},
		},
	}
}

func createRoute(generation int64, routeName string) v1alpha1.Route {
	return v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      routeName,
		},
		Spec: v1alpha1.RouteSpec{
			Generation: generation,
			Traffic: []v1alpha1.TrafficTarget{
				v1alpha1.TrafficTarget{
					Name:         "test-traffic-target",
					RevisionName: testRevisionName,
					Percent:      100,
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
