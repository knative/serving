/*
Copyright 2017 The Knative Authors
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
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/serving/pkg/system"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/mattbaird/jsonpatch"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/logging/testing"
)

func newDefaultOptions() ControllerOptions {
	return ControllerOptions{
		ServiceName:      "webhook",
		ServiceNamespace: system.Namespace,
		Port:             443,
		SecretName:       "webhook-certs",
		WebhookName:      "webhook.knative.dev",
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
	testServiceName       = "test-service-name"
)

func newRunningTestAdmissionController(t *testing.T, options ControllerOptions) (
	kubeClient *fakekubeclientset.Clientset,
	ac *AdmissionController,
	stopCh chan struct{}) {
	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()

	ac, err := NewAdmissionController(kubeClient, options, TestLogger(t))
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

	ac, err := NewAdmissionController(kubeClient, options, TestLogger(t))
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

	resp := ac.admit(TestContextWithLogger(t), &req)
	if !resp.Allowed {
		t.Fatalf("unexpected denial of delete")
	}
}

func TestConnectAllowed(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())

	req := admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Connect,
	}

	resp := ac.admit(TestContextWithLogger(t), &req)
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

	expectFailsWith(t, ac.admit(TestContextWithLogger(t), &req), "unhandled kind")
}

func TestInvalidNewConfigurationNameFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Configuration"},
	}
	invalidName := "configuration.example"
	config := createConfiguration(0, invalidName)
	marshaled, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")

	invalidName = strings.Repeat("a", 64)
	config = createConfiguration(0, invalidName)
	marshaled, err = json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal configuration: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")
}

func TestValidNewConfigurationObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(TestContextWithLogger(t), createValidCreateConfiguration())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidConfigurationNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(testGeneration, testConfigurationName)
	new := createConfiguration(testGeneration, testConfigurationName)
	resp := ac.admit(TestContextWithLogger(t), createUpdateConfiguration(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidConfigurationEnvChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createConfiguration(testGeneration, testConfigurationName)
	new := createConfiguration(testGeneration, testConfigurationName)
	new.Spec.RevisionTemplate.Spec.Container.Env = []corev1.EnvVar{{
		Name:  envVarName,
		Value: "different",
	}}
	resp := ac.admit(TestContextWithLogger(t), createUpdateConfiguration(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/generation",
		Value:     2.0,
	}})
}

func TestInvalidNewRouteNameFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Route"},
	}
	invalidName := "route.example"
	config := createRoute(0, invalidName)
	marshaled, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal route: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")

	invalidName = strings.Repeat("a", 64)
	config = createRoute(0, invalidName)
	marshaled, err = json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal route: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")
}

func TestValidNewRouteObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(TestContextWithLogger(t), createValidCreateRoute())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidRouteNoChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1, testRouteName)
	new := createRoute(1, testRouteName)
	resp := ac.admit(TestContextWithLogger(t), createUpdateRoute(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{})
}

func TestValidRouteChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createRoute(1, testRouteName)
	new := createRoute(1, testRouteName)
	new.Spec.Traffic = []v1alpha1.TrafficTarget{{
		RevisionName: testRevisionName,
		Percent:      100,
	}}
	resp := ac.admit(TestContextWithLogger(t), createUpdateRoute(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/generation",
		Value:     2.0,
	}})
}

func TestValidNewRevisionObject(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Revision"},
	}

	revision := createRevision(testRevisionName)
	marshaled, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.Object.Raw = marshaled
	resp := ac.admit(TestContextWithLogger(t), req)
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/generation",
		Value:     1.0,
	}, {
		Operation: "add",
		Path:      "/spec/servingState",
		Value:     "Active",
	}})
}

func TestValidRevisionUpdates(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Revision"},
	}

	revision := createRevision(testRevisionName)
	marshaled, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.OldObject.Raw = marshaled

	// Change fields we are allowed to change:
	revision.Spec.ServingState = v1alpha1.RevisionServingStateReserve

	marshaled, err = json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.Object.Raw = marshaled
	resp := ac.admit(TestContextWithLogger(t), req)
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{{
		Operation: "add",
		Path:      "/spec/generation",
		Value:     1.0,
	}})
}

func TestInvalidRevisionUpdate(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Revision"},
	}

	revision := createRevision(testRevisionName)
	marshaled, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.OldObject.Raw = marshaled

	// Change fields we are NOT allowed to change:
	revision.Spec.Container.Image = "yikes"

	marshaled, err = json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.Object.Raw = marshaled

	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Immutable fields changed")
}

func TestInvalidNewRevisionNameFails(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Revision"},
	}

	invalidName := "revision.example"
	revision := createRevision(invalidName)
	marshaled, err := json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")

	invalidName = strings.Repeat("a", 64)
	revision = createRevision(invalidName)

	marshaled, err = json.Marshal(revision)
	if err != nil {
		t.Fatalf("Failed to marshal revision: %s", err)
	}
	req.Object.Raw = marshaled
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), req), "Invalid resource name")
}

func TestValidNewServicePinned(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(TestContextWithLogger(t), createValidCreateServicePinned())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestValidNewServiceRunLatest(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	resp := ac.admit(TestContextWithLogger(t), createValidCreateServiceRunLatest())
	expectAllowed(t, resp)
	p := incrementGenerationPatch(0)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{p})
}

func TestInvalidNewServiceNoSpecs(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	svc := createServicePinned(0, testServiceName)
	svc.Spec.Pinned = nil
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), createCreateService(svc)), "Expected exactly one, got neither: spec.runLatest, spec.pinned")
}

func TestInvalidNewServiceNoRevisionNameInPinned(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	svc := createServicePinned(0, testServiceName)
	svc.Spec.Pinned.RevisionName = ""
	expectFailsWith(t, ac.admit(TestContextWithLogger(t), createCreateService(svc)), "spec.pinned.revisionName")
}

func TestValidServiceEnvChanges(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	old := createServicePinned(testGeneration, testServiceName)
	new := createServicePinned(testGeneration, testServiceName)
	new.Spec.Pinned.Configuration.RevisionTemplate.Spec.Container.Env = []corev1.EnvVar{{
		Name:  envVarName,
		Value: "different",
	}}
	resp := ac.admit(TestContextWithLogger(t), createUpdateService(&old, &new))
	expectAllowed(t, resp)
	expectPatches(t, resp.Patch, []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/generation",
		Value:     2.0,
	}})
}

func TestValidWebhook(t *testing.T) {
	_, ac := newNonRunningTestAdmissionController(t, newDefaultOptions())
	createDeployment(ac)
	ac.register(TestContextWithLogger(t), ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(), []byte{})
	_, err := ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(ac.options.WebhookName, metav1.GetOptions{})
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
		Webhooks: []admissionregistrationv1beta1.Webhook{{
			Name:         ac.options.WebhookName,
			Rules:        []admissionregistrationv1beta1.RuleWithOperations{{}},
			ClientConfig: admissionregistrationv1beta1.WebhookClientConfig{},
		}},
	}

	createDeployment(ac)
	createWebhook(ac, webhook)
	ac.register(TestContextWithLogger(t), ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(), []byte{})
	currentWebhook, _ := ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(ac.options.WebhookName, metav1.GetOptions{})
	if reflect.DeepEqual(currentWebhook.Webhooks, webhook.Webhooks) {
		t.Fatalf("Expected webhook to be updated")
	}
}

func TestRegistrationForAlreadyExistingWebhook(t *testing.T) {
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
	createWebhook(ac, webhook)

	ac.options.RegistrationDelay = 1 * time.Millisecond
	stopCh := make(chan struct{})
	errCh := make(chan error)

	go func() {
		errCh <- ac.Run(stopCh)
	}()

	err := <-errCh
	if err == nil {
		t.Fatal("Expected webhook controller to fail")
	}

	if !strings.Contains(err.Error(), "configmaps") {
		t.Fatal("Expected error msg to contain configmap key missing error")
	}
}

func TestCertConfigurationForAlreadyGeneratedSecret(t *testing.T) {
	secretName := "test-secret"
	ns := "test-namespace"
	opts := newDefaultOptions()
	opts.SecretName = secretName
	opts.ServiceNamespace = ns
	kubeClient, ac := newNonRunningTestAdmissionController(t, opts)

	ctx := context.TODO()
	newSecret, err := generateSecret(ctx, secretName, ns)
	if err != nil {
		t.Fatalf("Failed to generate secret: %v", err)
	}
	_, err = kubeClient.CoreV1().Secrets(ns).Create(newSecret)
	if err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	createNamespace(t, ac.client, metav1.NamespaceSystem)
	createTestConfigMap(t, ac.client)

	tlsConfig, caCert, err := configureCerts(ctx, kubeClient, &ac.options)
	if err != nil {
		t.Fatalf("Failed to configure secret: %v", err)
	}
	expectedCert, err := tls.X509KeyPair(newSecret.Data[secretServerCert], newSecret.Data[secretServerKey])
	if err != nil {
		t.Fatalf("Failed to create cert from x509 key pair: %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("Expected TLS config not to be nil")
	}
	if len(tlsConfig.Certificates) < 1 {
		t.Fatalf("Expected TLS Config Cert to be set")
	}

	if diff := cmp.Diff(expectedCert.Certificate, tlsConfig.Certificates[0].Certificate, cmp.AllowUnexported()); diff != "" {
		t.Fatalf("Unexpected cert diff (-want, +got) %v", diff)
	}
	if diff := cmp.Diff(newSecret.Data[secretCACert], caCert, cmp.AllowUnexported()); diff != "" {
		t.Fatalf("Unexpected CA cert diff (-want, +got) %v", diff)
	}
}

func TestCertConfigurationForGeneratedSecret(t *testing.T) {
	secretName := "test-secret"
	ns := "test-namespace"
	opts := newDefaultOptions()
	opts.SecretName = secretName
	opts.ServiceNamespace = ns
	kubeClient, ac := newNonRunningTestAdmissionController(t, opts)

	ctx := context.TODO()
	createNamespace(t, ac.client, metav1.NamespaceSystem)
	createTestConfigMap(t, ac.client)

	tlsConfig, caCert, err := configureCerts(ctx, kubeClient, &ac.options)
	if err != nil {
		t.Fatalf("Failed to configure certificates: %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("Expected TLS config not to be nil")
	}
	if len(tlsConfig.Certificates) < 1 {
		t.Fatalf("Expected TLS Certfificate to be set on webhook server")
	}

	p, _ := pem.Decode(caCert)
	if p == nil {
		t.Fatalf("Expected PEM encoded CA cert ")
	}
	if p.Type != "CERTIFICATE" {
		t.Fatalf("Expectet type to be CERTIFICATE but got %s", string(p.Type))
	}
}

func createUpdateConfiguration(old, new *v1alpha1.Configuration) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateConfiguration()
	marshaled, err := json.Marshal(old)
	if err != nil {
		panic("failed to marshal configuration")
	}
	req.Object.Raw = marshaled
	marshaledOld, err := json.Marshal(new)
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
	marshaled, err := json.Marshal(config)
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
	marshaled, err := json.Marshal(route)
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

func createUpdateRouteRaw(old, new []byte) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateRoute()
	req.Object.Raw = new
	req.OldObject.Raw = old
	return req
}

func createUpdateRoute(old, new *v1alpha1.Route) *admissionv1beta1.AdmissionRequest {
	marshaledOld, err := json.Marshal(old)
	if err != nil {
		panic("failed to marshal route")
	}
	marshaled, err := json.Marshal(new)
	if err != nil {
		panic("failed to marshal route")
	}
	return createUpdateRouteRaw(marshaledOld, marshaled)
}

func createDeployment(ac *AdmissionController) {
	deployment := &v1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      servingWebhookDeployment,
			Namespace: system.Namespace,
		},
	}
	ac.client.ExtensionsV1beta1().Deployments(system.Namespace).Create(deployment)
}

func createBaseUpdateService() *admissionv1beta1.AdmissionRequest {
	return &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Update,
		Kind:      metav1.GroupVersionKind{Kind: "Service"},
	}
}

func createUpdateService(old, new *v1alpha1.Service) *admissionv1beta1.AdmissionRequest {
	req := createBaseUpdateService()
	marshaled, err := json.Marshal(old)
	if err != nil {
		panic("failed to marshal service")
	}
	req.Object.Raw = marshaled
	marshaledOld, err := json.Marshal(new)
	if err != nil {
		panic("failed to marshal service")
	}
	req.OldObject.Raw = marshaledOld
	return req
}

func createCreateService(service v1alpha1.Service) *admissionv1beta1.AdmissionRequest {
	req := &admissionv1beta1.AdmissionRequest{
		Operation: admissionv1beta1.Create,
		Kind:      metav1.GroupVersionKind{Kind: "Service"},
	}
	marshaled, err := json.Marshal(service)
	if err != nil {
		panic("failed to marshal service")
	}
	req.Object.Raw = marshaled
	return req
}

func createValidCreateServicePinned() *admissionv1beta1.AdmissionRequest {
	return createCreateService(createServicePinned(0, testServiceName))
}

func createValidCreateServiceRunLatest() *admissionv1beta1.AdmissionRequest {
	return createCreateService(createServiceRunLatest(0, testServiceName))
}

func createWebhook(ac *AdmissionController, webhook *admissionregistrationv1beta1.MutatingWebhookConfiguration) {
	client := ac.client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	_, err := client.Create(webhook)
	if err != nil {
		panic(fmt.Sprintf("failed to create test webhook: %s", err))
	}
}

func expectAllowed(t *testing.T, resp *admissionv1beta1.AdmissionResponse) {
	t.Helper()
	if !resp.Allowed {
		t.Errorf("Expected allowed, but failed with %+v", resp.Result)
	}
}

func expectFailsWith(t *testing.T, resp *admissionv1beta1.AdmissionResponse, contains string) {
	t.Helper()
	if resp.Allowed {
		t.Errorf("expected denial, got allowed")
		return
	}
	if !strings.Contains(resp.Result.Message, contains) {
		t.Errorf("expected failure containing %q got %q", contains, resp.Result.Message)
	}
}

func expectPatches(t *testing.T, a []byte, e []jsonpatch.JsonPatchOperation) {
	t.Helper()
	var got []jsonpatch.JsonPatchOperation

	err := json.Unmarshal(a, &got)
	if err != nil {
		t.Errorf("failed to unmarshal patches: %s", err)
		return
	}

	if diff := cmp.Diff(e, got, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("expectPatches (-want, +got) = %v", diff)
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
					Container: corev1.Container{
						Image: imageName,
						Env: []corev1.EnvVar{{
							Name:  envVarName,
							Value: envVarValue,
						}},
					},
					ConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelMulti,
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
			Traffic: []v1alpha1.TrafficTarget{{
				Name:         "test-traffic-target",
				RevisionName: testRevisionName,
				Percent:      100,
			}},
		},
	}
}

func createRevision(revName string) v1alpha1.Revision {
	return v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      revName,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
				Image: "test-image",
			},
			ConcurrencyModel: v1alpha1.RevisionRequestConcurrencyModelMulti,
		},
	}
}

func createServicePinned(generation int64, serviceName string) v1alpha1.Service {
	return v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      serviceName,
		},
		Spec: v1alpha1.ServiceSpec{
			Generation: generation,
			Pinned: &v1alpha1.PinnedType{
				RevisionName:  testRevisionName,
				Configuration: createConfiguration(generation, "config").Spec,
			},
		},
	}
}

func createServiceRunLatest(generation int64, serviceName string) v1alpha1.Service {
	return v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      serviceName,
		},
		Spec: v1alpha1.ServiceSpec{
			Generation: generation,
			RunLatest: &v1alpha1.RunLatestType{
				Configuration: createConfiguration(generation, "config").Spec,
			},
		},
	}
}

func incrementGenerationPatch(old float64) jsonpatch.JsonPatchOperation {
	return jsonpatch.JsonPatchOperation{
		Operation: "add",
		Path:      "/spec/generation",
		Value:     old + 1.0,
	}
}
