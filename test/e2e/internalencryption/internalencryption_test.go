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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	netcfg "knative.dev/networking/pkg/config"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"

	//. "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/test_images/metricsreader/helpers"
	v1test "knative.dev/serving/test/v1"
)

var (
	ExpectedSecurityMode = netcfg.TrustEnabled
)

// TestInitContainers tests init containers support.
func TestInternalEncryption(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.MetricsReader,
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
		spoof.MatchesAllOf(spoof.IsStatusOK, spoof.MatchesBody(test.MetricsReaderText)),
		"MetricsReaderText",
		test.ServingFlags.ResolvableDomain,
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.MetricsReaderText, err)
	}

	pods, err := clients.KubeClient.CoreV1().Pods("serving-tests").List(context.TODO(), v1.ListOptions{
		LabelSelector: fmt.Sprintf("serving.knative.dev/configuration=%s", names.Config),
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	var postData helpers.PostData

	if len(pods.Items) > 0 {
		postData.QueueIP = pods.Items[0].Status.PodIP
	}

	pods, err = clients.KubeClient.CoreV1().Pods("knative-serving").List(context.TODO(), v1.ListOptions{
		LabelSelector: "app=activator",
	})
	if err != nil {
		t.Fatalf("Failed to get pods: %v", err)
	}
	if len(pods.Items) > 0 {
		postData.ActivatorIP = pods.Items[0].Status.PodIP
	}

	initialCounts, err := getTLSCounts(url.String(), &postData)
	if err != nil {
		t.Fatalf("Failed to get initial TLS Connection Counts: %v", err)
	}
	t.Logf("Initial Counts: %#v", initialCounts)

	updatedCounts, err := getTLSCounts(url.String(), &postData)
	if err != nil {
		t.Fatalf("Failed to get updated TLS Connection Counts: %v", err)
	}
	t.Logf("Updated Counts: %#v", updatedCounts)

	if updatedCounts.Activator[ExpectedSecurityMode] <= initialCounts.Activator[ExpectedSecurityMode] {
		t.Fatalf("Connection Count with SecurityMode (%s) at Activator pod failed to increase", ExpectedSecurityMode)
	}

	if updatedCounts.Queue[ExpectedSecurityMode] <= initialCounts.Queue[ExpectedSecurityMode] {
		t.Fatalf("Connection Count with SecurityMode (%s) at QueueProxy pod failed to increase", ExpectedSecurityMode)
	}

}

func getTLSCounts(url string, d *helpers.PostData) (*helpers.ResponseData, error) {
	counts := &helpers.ResponseData{}

	jsonPostData, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal post data request to JSON:\n  %#v\n  %w", *d, err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonPostData))
	if err != nil {
		return nil, fmt.Errorf("failed to make POST request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Failed to read response body from %s: %w", url, err)
	}

	err = json.Unmarshal(body, &counts)
	if err != nil {
		return nil, fmt.Errorf("Failed to unmarshal response body:\n  body: %s\n  err: %w", string(body), err)
	}

	return counts, nil
}
