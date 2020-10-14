// +build e2e

/*
Copyright 2020 The Knative Authors

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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/system"
	pkgtest "knative.dev/pkg/test"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

const template = `{"httpRequest": {"requestMethod": "{{.Request.Method}}", "requestUrl": "{{js .Request.RequestURI}}", "requestSize": "{{.Request.ContentLength}}", "status": {{.Response.Code}}, "responseSize": "{{.Response.Size}}", "userAgent": "{{js .Request.UserAgent}}", "remoteIp": "{{js .Request.RemoteAddr}}", "serverIp": "{{.Revision.PodIP}}", "referer": "{{js .Request.Referer}}", "latency": "{{.Response.Latency}}s", "protocol": "{{.Request.Proto}}"}, "traceId": "{{index .Request.Header "X-B3-Traceid"}}"}`

func TestRequestLogs(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	cm, err := clients.KubeClient.GetConfigMap(system.Namespace()).Get(context.Background(), "config-observability", metav1.GetOptions{})
	if err != nil {
		t.Fatal("Fail to get ConfigMap config-observability:", err)
	}

	requestLogEnabled := strings.EqualFold(cm.Data[metrics.EnableReqLogKey], "true")
	probeLogEnabled := strings.EqualFold(cm.Data["logging.enable-probe-request-log"], "true")

	if !requestLogEnabled && !probeLogEnabled {
		t.Skip("Skipping verifying request logs because both request and probe logging is disabled")
	}

	if got, want := cm.Data[metrics.ReqLogTemplateKey], template; got != want {
		t.Skipf("Skipping verifing request logs because the template doesn't match:\n%s", cmp.Diff(want, got))
	}

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.EnsureTearDown(t, clients, &names)

	t.Log("Creating a new Service")

	resources, err := v1test.CreateServiceReady(t, clients, &names, []rtesting.ServiceOption{
		rtesting.WithConfigAnnotations(map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
			autoscaling.MaxScaleAnnotationKey: "1",
		})}...)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	_, err = pkgtest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		resources.Route.Status.URL.URL(),
		v1test.RetryingRouteInconsistency(pkgtest.MatchesAllOf(pkgtest.IsStatusOK, pkgtest.MatchesBody(test.HelloWorldText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))
	if err != nil {
		t.Fatalf("The endpoint didn't serve the expected text %q: %v", test.HelloWorldText, err)
	}

	pod, err := theOnlyPod(clients, resources.Revision.Namespace, resources.Revision.Name)
	if err != nil {
		t.Fatal("Fail to fetch the pod:", err)
	}

	// Only check request logs if the feature is enabled in config-observability.
	if requestLogEnabled {
		// A request was sent to / in WaitForEndpointState.
		if err := waitForLog(t, clients, pod.Namespace, pod.Name, "queue-proxy", func(log logLine) bool {
			return log.HTTPRequest.RequestURL == "/" &&
				log.HTTPRequest.UserAgent != network.QueueProxyUserAgent
		}); err != nil {
			t.Fatal("Got error waiting for normal request logs:", err)
		}
	} else {
		t.Log("Skipping verifing request logs because they are not enabled")
	}

	// Only check probe request logs if the feature is enabled in config-observability.
	if probeLogEnabled {
		// Health check requests are sent to / with a specific userAgent value periodically.
		if err := waitForLog(t, clients, pod.Namespace, pod.Name, "queue-proxy", func(log logLine) bool {
			return log.HTTPRequest.RequestURL == "/" &&
				log.HTTPRequest.UserAgent == network.QueueProxyUserAgent
		}); err != nil {
			t.Fatal("Got error waiting for health check log:", err)
		}
	} else {
		t.Log("Skipping verifing probe request logs because they are not enabled")
	}
}

func theOnlyPod(clients *test.Clients, ns, rev string) (corev1.Pod, error) {
	pods, err := clients.KubeClient.Kube.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{"app": rev}.String(),
	})

	if err != nil {
		return corev1.Pod{}, err
	}

	if len(pods.Items) == 0 {
		return corev1.Pod{}, fmt.Errorf("Expect 1 pod, but got %d for %s:%s", len(pods.Items), system.Namespace(), rev)
	}

	return pods.Items[0], nil
}

// waitForLog fetches the logs from a container of a pod decided by the given parameters
// until the given condition is meet or timeout. Most of knative logs are in json format.
func waitForLog(t *testing.T, clients *test.Clients, ns, podName, container string, condition func(log logLine) bool) error {
	return wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
		req := clients.KubeClient.Kube.CoreV1().Pods(ns).GetLogs(podName, &corev1.PodLogOptions{
			Container: container,
		})
		podLogs, err := req.Stream(context.Background())
		if err != nil {
			return false, err
		}
		defer podLogs.Close()

		scanner := bufio.NewScanner(podLogs)
		for scanner.Scan() {
			t.Logf("%s/%s log: %s", podName, container, scanner.Text())
			if len(scanner.Bytes()) == 0 {
				continue
			}
			var result logLine
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				t.Logf("Failed to parse log `%s` into json: %v", scanner.Text(), err)
				continue
			}
			if condition(result) {
				return true, nil
			}
		}
		return false, scanner.Err()
	})
}

type logLine struct {
	HTTPRequest httpRequest `json:"httpRequest"`
}

type httpRequest struct {
	RequestURL string `json:"requestUrl"`
	UserAgent  string `json:"userAgent"`
}
