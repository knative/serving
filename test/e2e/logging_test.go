// +build e2e

/*
Copyright 2018 The Knative Authors

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

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/network"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const request_log_template = `
{"requestMethod": "{{.Request.Method}}", "requestUrl": "{{js .Request.RequestURI}}", "userAgent": "{{js .Request.UserAgent}}"}
`

func TestRequestLogs(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	// Patch config-observability to enable request logs
	obcm, err := rawCM(clients, "config-observability")
	if err != nil {
		t.Errorf("Error retrieving observability configmap: %v", err)
	}

	patchedObcm := obcm.DeepCopy()
	patchedObcm.Data["logging.request-log-template"] = request_log_template
	patchedObcm.Data["logging.enable-probe-request-log"] = "true"
	patchCM(clients, patchedObcm)
	defer patchCM(clients, obcm)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, []rtesting.ServiceOption{
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
			autoscaling.MaxScaleAnnotationKey: "1",
		})}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	url := resources.Route.Status.URL.URL()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText))),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(t.Logf, clients, test.ServingFlags.Https),
	); err != nil {
		t.Fatalf("The endpoint %s for Route %s didn't serve the expected text %q: %v", url, names.Route, test.HelloWorldText, err)
	}

	pod, err := theOnlyPod(clients, resources.Revision.Namespace, resources.Revision.Name)
	if err != nil {
		t.Fatalf("Fail to fetch the pod: %v", err)
	}

	// A request was sent to / in WaitForEndpointState.
	if err := waitForLog(clients, pod.Namespace, pod.Name, "queue-proxy", func(log map[string]interface{}) bool {
		if v, ok := log["requestUrl"]; !ok {
			return false
		} else if v.(string) != "/" {
			return false
		}

		if v, ok := log["userAgent"]; !ok {
			return false
		} else {
			return v.(string) != network.QueueProxyUserAgent
		}
	}); err != nil {
		t.Fatalf("Got error waiting for normal request logs: %v", err)
	}

	// Health check requests are sent to / with a specific userAgent value periodically.
	if err := waitForLog(clients, pod.Namespace, pod.Name, "queue-proxy", func(log map[string]interface{}) bool {
		if v, ok := log["requestUrl"]; !ok {
			return false
		} else if v.(string) != "/" {
			return false
		}

		if v, ok := log["userAgent"]; !ok {
			return false
		} else {
			return v.(string) == network.QueueProxyUserAgent
		}
	}); err != nil {
		t.Fatalf("Got error waiting for health check log: %v", err)
	}
}

func theOnlyPod(clients *test.Clients, ns, rev string) (corev1.Pod, error) {
	pods, err := clients.KubeClient.Kube.CoreV1().Pods(ns).List(metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", rev)})

	if err != nil {
		return corev1.Pod{}, err
	}

	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("Expect 1 pod, but got %d for %s:%s", len(pods.Items), system.Namespace(), rev)
	}

	return pods.Items[0], nil
}

// waitForLog fetches the logs from a container of a pod decided by the given parameters
// until the given condition is meet or timeout. Most of knative logs are in json format.
func waitForLog(clients *test.Clients, ns, podName, container string, condition func(log map[string]interface{}) bool) error {
	return wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
		podLogOpts := corev1.PodLogOptions{Container: container}

		req := clients.KubeClient.Kube.CoreV1().Pods(ns).GetLogs(podName, &podLogOpts)
		podLogs, err := req.Stream()
		if err != nil {
			return false, err
		}
		defer podLogs.Close()

		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, podLogs)
		if err != nil {
			return false, err
		}

		for _, log := range strings.Split(buf.String(), "\n") {
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(log), &result); err != nil {
				continue
			}

			if condition(result) {
				return true, nil
			}
		}
		return false, nil
	})
}
