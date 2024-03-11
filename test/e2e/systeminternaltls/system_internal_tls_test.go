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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestSystemInternalTLS tests the TLS connections between system components.
func TestSystemInternalTLS(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	if !(strings.Contains(test.ServingFlags.IngressClass, "kourier") ||
		strings.Contains(test.ServingFlags.IngressClass, "istio")) {
		t.Skip("Skip this test for non-kourier or non-istio ingress.")
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
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

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

	req := clients.KubeClient.CoreV1().Pods(activatorPod.Namespace).GetLogs(activatorPod.Name, &corev1.PodLogOptions{})
	activatorTLSCount, err := scanPodLogs(req, matchTLSLog)

	if err != nil {
		t.Fatalf("Failed checking activator logs: %s", err)
	} else if activatorTLSCount == 0 {
		t.Fatal("TLS not used on requests to activator")
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
	req = clients.KubeClient.CoreV1().Pods(helloWorldPod.Namespace).GetLogs(helloWorldPod.Name, &corev1.PodLogOptions{Container: "queue-proxy"})
	queueTLSCount, err := scanPodLogs(req, matchTLSLog)

	if err != nil {
		t.Fatalf("Failed checking queue-proxy logs: %s", err)
	} else if queueTLSCount == 0 {
		t.Fatal("TLS not used on requests to queue-proxy")
	}
}

func scanPodLogs(req *rest.Request, matcher func(string) bool) (matchCount int, err error) {

	podLogs, err := req.Stream(context.Background())
	if err != nil {
		err = fmt.Errorf("failed to stream activator logs: %w", err)
		return
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	podLogs.Close()
	if err != nil {
		err = fmt.Errorf("failed to read activator logs from buffer: %w", err)
		return
	}

	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		if matcher(scanner.Text()) {
			matchCount++
		}
	}

	if err = scanner.Err(); err != nil {
		err = fmt.Errorf("failed scanning activator logs: %w", err)
		return
	}

	return
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
