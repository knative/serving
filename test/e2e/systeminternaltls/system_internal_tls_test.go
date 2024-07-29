//go:build e2e
// +build e2e

/*
Copyright 2023 The Knative Authors

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

package systeminternaltls

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/certificates"
	"knative.dev/networking/pkg/config"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/autoscaling"
	pkgNetworking "knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue/certificate"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

// TestSystemInternalTLS tests the TLS connections between system components.
func TestSystemInternalTLS(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !strings.Contains(test.ServingFlags.IngressClass, "kourier") &&
		!strings.Contains(test.ServingFlags.IngressClass, "contour") {
		t.Skip("Skip this test for non-kourier/contour ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")
	resources, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	//The request made here should be enough to trigger some request logs on the Activator and QueueProxy
	t.Log("Checking Endpoint state")
	url := resources.Route.Status.URL.URL()
	checkEndpointState(t, clients, url)

	t.Log("Checking Activator logs")
	pods, err := clients.KubeClient.CoreV1().Pods(system.Namespace()).List(context.TODO(), v1.ListOptions{
		LabelSelector: "app=activator",
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	if len(pods.Items) == 0 {
		t.Fatalf("No pods detected for activator: %v", err)
	}
	activatorPod := pods.Items[0]

	const numMatches = 1
	if err := e2e.WaitForLog(t, clients, activatorPod.Namespace, activatorPod.Name, "activator", matchTLSLog, numMatches); err != nil {
		t.Fatal("TLS not used on requests to activator:", err)
	}

	t.Log("Checking Queue-Proxy logs")
	pods, err = clients.KubeClient.CoreV1().Pods("serving-tests").List(context.TODO(), v1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/configuration=%s", names.Config),
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	if len(pods.Items) == 0 {
		t.Fatalf("No pods detected for test app: %v", err)
	}
	helloWorldPod := pods.Items[0]

	if err := e2e.WaitForLog(t, clients, helloWorldPod.Namespace, helloWorldPod.Name, "queue-proxy", matchTLSLog, numMatches); err != nil {
		t.Fatal("TLS not used on requests to queue-proxy:", err)
	}
}

// TestTLSCertificateRotation tests certificate rotation and automatic reloading of certs.
func TestTLSCertificateRotation(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !strings.Contains(test.ServingFlags.IngressClass, "kourier") &&
		!strings.Contains(test.ServingFlags.IngressClass, "contour") {
		t.Skip("Skip this test for non-kourier/contour ingress.")
	}

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	t.Log("Creating Service:", names.Service)
	resources1, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithConfigAnnotations(map[string]string{
			// Make sure we don't scale to zero during the test as we're waiting for logs.
			autoscaling.MinScaleAnnotationKey: "1",
		}))
	if err != nil {
		t.Fatalf("Failed to create Service: %v: %v", names.Service, err)
	}

	t.Log("Checking Endpoint state")
	url := resources1.Route.Status.URL.URL()
	checkEndpointState(t, clients, url)

	// Read the old (default) secret.
	secret, err := e2e.GetCASecret(clients)
	if err != nil {
		t.Fatal(err)
	}

	if err := clients.KubeClient.CoreV1().Secrets(secret.Namespace).
		Delete(context.Background(), secret.Name, v1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete Secret %s: %v", secret.Name, err)
	}

	// Wait for the secret to be reloaded.
	secretRenewed, err := e2e.GetCASecret(clients)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Creating ConfigMap with old and new CA certs")
	systemNS := os.Getenv(system.NamespaceEnvKey)

	// Create ConfigMap with networking.knative.dev/trust-bundle label in required namespaces
	cm := &corev1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name: "knative-bundle",
			Labels: map[string]string{
				networking.TrustBundleLabelKey: "true",
			},
		},
		Data: map[string]string{
			"cert.pem":         string(secret.Data[certificates.CertName]),
			"cert_renewed.pem": string(secretRenewed.Data[certificates.CertName]),
		},
	}
	_, err = clients.KubeClient.CoreV1().ConfigMaps(systemNS).
		Create(context.Background(), cm, v1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create configmap:", err)
	}

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().ConfigMaps(systemNS).
			Delete(context.Background(), cm.Name, v1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			t.Fatal("Failed to delete configmap:", err)
		}
	})

	t.Log("Deleting Secret in user namespace that is mounted by queue-proxy")
	if err := clients.KubeClient.CoreV1().Secrets(test.ServingFlags.TestNamespace).Delete(context.Background(), pkgNetworking.ServingCertName, v1.DeleteOptions{}); err != nil {
		t.Error(err)
	}

	t.Log("Checking queue-proxy logs")
	pods, err := clients.KubeClient.CoreV1().Pods(test.ServingFlags.TestNamespace).List(context.TODO(), v1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/configuration=%s", names.Config),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods.Items) == 0 {
		t.Fatal("No pods detected for test app:", err)
	}
	// The Certs are loaded during startup and then after re-creating the secret.
	const numMatches = 2
	helloWorldPod := pods.Items[0]
	if err := e2e.WaitForLog(t, clients, helloWorldPod.Namespace, helloWorldPod.Name, "queue-proxy", matchCertReloadLog, numMatches); err != nil {
		t.Fatal("Certificate not reloaded in time by queue-proxy:", err)
	}
	checkEndpointState(t, clients, url)

	t.Log("Deleting secret in system namespace")
	if err := clients.KubeClient.CoreV1().Secrets(systemNS).Delete(context.Background(), config.ServingRoutingCertName, v1.DeleteOptions{}); err != nil {
		t.Fatalf("Failed to delete secret %s in system namespace: %v", config.ServingRoutingCertName, err)
	}
	checkEndpointState(t, clients, url)
}

func checkEndpointState(t *testing.T, clients *test.Clients, url *url.URL) {
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
		"HelloWorldText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS),
	); err != nil {
		t.Fatalf("The endpoint %s didn't serve the expected text %q: %v", url, test.HelloWorldText, err)
	}
}

func matchTLSLog(line string) bool {
	if strings.Contains(line, "TLS") {
		if strings.Contains(line, "TLS: <nil>") {
			return false
		} else if strings.Contains(line, "TLS: {") {
			return true
		}
	}
	return false
}

func matchCertReloadLog(line string) bool {
	return strings.Contains(line, certificate.CertReloadMessage)
}
