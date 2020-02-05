/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package performance

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/prometheus"
	"knative.dev/serving/test"

	"knative.dev/pkg/test/spoof"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	monitoringNS = "knative-monitoring"
	// Property name used by testgrid.
	perfLatency = "perf_latency"
	duration    = 1 * time.Minute
)

// Enable monitoring components
const (
	EnablePrometheus = iota
)

// Client is the client used in the performance tests.
type Client struct {
	E2EClients *test.Clients
	PromClient *prometheus.PromProxy
}

// Setup creates all the clients that we need to interact with in our tests
func Setup(t *testing.T, monitoring ...int) (*Client, error) {
	pkgTest.SetupLoggingFlags()
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		return nil, err
	}

	var p *prometheus.PromProxy
	for _, m := range monitoring {
		switch m {
		case EnablePrometheus:
			t.Log("Creating prometheus proxy client")
			p = &prometheus.PromProxy{Namespace: monitoringNS}
			p.Setup(clients.KubeClient.Kube, t.Logf)
		default:
			t.Log("No monitoring components enabled")
		}
	}

	return &Client{E2EClients: clients, PromClient: p}, nil
}

// TearDown cleans up resources used
func TearDown(client *Client, names test.ResourceNames, logf logging.FormatLogger) {
	test.TearDown(client.E2EClients, names)

	// Teardown prometheus client
	if client.PromClient != nil {
		client.PromClient.Teardown(logf)
	}
}

// ProbeTargetTillReady will probe the target once per second for the given duration, until it's ready or error happens
func ProbeTargetTillReady(target string, duration time.Duration) error {
	// Make sure the target is ready before sending the large amount of requests.
	spoofingClient := spoof.SpoofingClient{
		Client:          &http.Client{},
		RequestInterval: 1 * time.Second,
		RequestTimeout:  duration,
		Logf: func(fmt string, args ...interface{}) {
			log.Printf(fmt, args)
		},
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("target %q is invalid, cannot probe: %w", target, err)
	}
	if _, err = spoofingClient.Poll(req, func(resp *spoof.Response) (done bool, err error) {
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to get target %q ready: %w", target, err)
	}
	return nil
}

// WaitForScaleToZero will wait for the deployments in the indexer to scale to 0
func WaitForScaleToZero(ctx context.Context, namespace string, selector labels.Selector, duration time.Duration) error {
	pl := podinformer.Get(ctx).Lister()
	begin := time.Now()
	return wait.PollImmediate(1*time.Second, duration, func() (bool, error) {
		pods, err := pl.Pods(namespace).List(selector)
		if err != nil {
			return false, err
		}
		for _, pod := range pods {
			// Pending or Running w/o deletion timestamp (i.e. terminating).
			if pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning && pod.ObjectMeta.DeletionTimestamp == nil {
				return false, nil
			}
		}
		log.Printf("All pods are done or terminating after %v", time.Since(begin))
		return true, nil
	})
}

// resolvedHeaders returns headers for the request.
func resolvedHeaders(domain string, resolvableDomain bool) map[string][]string {
	headers := make(map[string][]string)
	if !resolvableDomain {
		headers["Host"] = []string{domain}
	}
	return headers
}
