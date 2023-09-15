//go:build e2e
// +build e2e

/*
Copyright 2021 The Knative Authors

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

package internalencryption

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/rest"
	netcfg "knative.dev/networking/pkg/config"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/serving"

	//. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

var (
	ExpectedSecurityMode = netcfg.TrustEnabled
)

type RequestLog struct {
	RequestURL string              `json:"requestUrl"`
	TLS        tls.ConnectionState `json:"tls"`
}

// TestInitContainers tests init containers support.
func TestInternalEncryption(t *testing.T) {
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

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.CheckEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		url,
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.HelloWorldText)),
		"HelloWorldText",
		test.ServingFlags.ResolvableDomain,
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

	revision := resources.Revision
	if val := revision.Labels[serving.ConfigurationLabelKey]; val != names.Config {
		t.Fatalf("Got revision label configuration=%q, want=%q ", names.Config, val)
	}
	if val := revision.Labels[serving.ServiceLabelKey]; val != names.Service {
		t.Fatalf("Got revision label service=%q, want=%q", val, names.Service)
	}

	// Check on the logs for the activator
	pods, err := clients.KubeClient.CoreV1().Pods("knative-serving").List(context.TODO(), v1.ListOptions{
		LabelSelector: "app=activator",
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	activatorPod := pods.Items[0]

	req := clients.KubeClient.CoreV1().Pods(activatorPod.Namespace).GetLogs(activatorPod.Name, &corev1.PodLogOptions{})
	_, nilCount, err := getPodLogs(req)

	if nilCount > 0 {
		t.Fatal("TLS not used on requests to activator")
	}

	// Check on the logs for the queue-proxy
	pods, err = clients.KubeClient.CoreV1().Pods("serving-tests").List(context.TODO(), v1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/configuration=%s", names.Config),
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	helloWorldPod := pods.Items[0]
	req = clients.KubeClient.CoreV1().Pods(helloWorldPod.Namespace).GetLogs(helloWorldPod.Name, &corev1.PodLogOptions{})
	_, nilCount, err = getPodLogs(req)

	if nilCount > 0 {
		t.Fatal("TLS not used on requests to queue-proxy")
	}
}

func getPodLogs(req *rest.Request) (tlsCount int, nilCount int, err error) {
	tlsCount, nilCount = 0, 0

	podLogs, err := req.Stream(context.Background())
	if err != nil {
		err = fmt.Errorf("Failed to stream activator logs: %v", err)
		return
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	podLogs.Close()
	if err != nil {
		err = fmt.Errorf("Failed to read activator logs from buffer: %v", err)
		return
	}

	scanner := bufio.NewScanner(buf)
	for scanner.Scan() {
		log.Printf("log: %s", scanner.Text())
		if strings.Contains(scanner.Text(), "TLS") {
			if strings.Contains(scanner.Text(), "TLS: <nil>") {
				nilCount += 1

			} else if strings.Contains(scanner.Text(), "TLS: [") {
				tlsCount += 1
			}
		}
	}

	if err = scanner.Err(); err != nil {
		err = fmt.Errorf("Failed to scan activator logs: %v", err)
		return
	}

	return
}
