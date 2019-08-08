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
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	deploymentinformer "knative.dev/pkg/injection/informers/kubeinformers/appsv1/deployment"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/zipkin"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	sksinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"
	"knative.dev/serving/test"
	"knative.dev/test-infra/shared/common"
	"knative.dev/test-infra/shared/prometheus"
	"knative.dev/test-infra/shared/prow"

	// Mysteriously required to support GCP auth (required by k8s libs). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	monitoringNS = "knative-monitoring"
	// Property name used by testgrid.
	perfLatency = "perf_latency"
	duration    = 1 * time.Minute
	traceSuffix = "-Trace.json"
	httpPrefix  = "http://"
)

// Enable monitoring components
const (
	EnablePrometheus = iota
	EnableZipkinTracing
)

// Client is the client used in the performance tests.
type Client struct {
	E2EClients *test.Clients
	PromClient *prometheus.PromProxy
}

// traceFile is the name of the
var traceFile *os.File

// Setup creates all the clients that we need to interact with in our tests
func Setup(t *testing.T, monitoring ...int) (*Client, error) {
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
		case EnableZipkinTracing:
			// Enable zipkin tracing
			zipkin.SetupZipkinTracing(clients.KubeClient.Kube, t.Logf)

			// Create file to store traces
			dir := prow.GetLocalArtifactsDir()
			if err := common.CreateDir(dir); nil != err {
				t.Log("Cannot create the artifacts dir. Will not log tracing.")
			} else {
				name := path.Join(dir, t.Name()+traceSuffix)
				t.Logf("Storing traces in %s", name)
				traceFile, err = os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					t.Log("Unable to create tracing file.")
				}
			}
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

	// disable zipkin
	if traceFile != nil {
		zipkin.CleanupZipkinTracingSetup(logf)
		traceFile.Close()
	}
}

// AddTrace gets the JSON zipkin trace for the traceId and stores it.
// https://github.com/openzipkin/zipkin-go/blob/master/model/span.go defines the struct for the JSON
func AddTrace(logf logging.FormatLogger, tName string, traceID string) {
	if traceFile == nil {
		logf("Trace file is not setup correctly. Exiting without adding trace")
		return
	}

	// Sleep to get traces
	time.Sleep(5 * time.Second)

	trace, err := zipkin.JSONTrace(traceID)
	if err != nil {
		logf("Skipping trace %s due to error: %v", traceID, err)
		return
	}

	if _, err := traceFile.WriteString(fmt.Sprintf("%s,\n", trace)); err != nil {
		logf("Cannot write to trace file: %v", err)
	}
}

// resolvedHeaders returns headers for the request.
func resolvedHeaders(domain string, resolvableDomain bool) map[string][]string {
	headers := make(map[string][]string)
	if !resolvableDomain {
		headers["Host"] = []string{domain}
	}
	return headers
}

// sanitizedURL returns a URL that is guaranteed to have an httpPrefix.
func sanitizedURL(endpoint string) string {
	if !strings.HasPrefix(endpoint, httpPrefix) {
		return httpPrefix + endpoint
	}
	return endpoint
}

// DeploymentStatus is a struct that wraps the status of a deployment.
type DeploymentStatus struct {
	DesiredReplicas int32
	ReadyReplicas   int32
}

// FetchDeploymentStatus creates a channel that can return the up-to-date DeploymentStatus periodically.
func FetchDeploymentStatus(
	ctx context.Context, namespace string, selector labels.Selector, ticker *time.Ticker,
) <-chan []DeploymentStatus {
	dl := deploymentinformer.Get(ctx).Lister()
	ch := make(chan []DeploymentStatus)
	go func() {
		for {
			select {
			case <-ticker.C:
				// Overlay the desired and ready pod counts.
				deployments, err := dl.Deployments(namespace).List(selector)
				if err != nil {
					log.Printf("Error listing deployments: %v", err)
					break
				}

				ds := make([]DeploymentStatus, len(deployments))
				for _, d := range deployments {
					ds = append(ds, DeploymentStatus{
						DesiredReplicas: *d.Spec.Replicas,
						ReadyReplicas:   d.Status.ReadyReplicas,
					})
				}
				ch <- ds
			}
		}
	}()

	return ch
}

// FetchSKSMode creates a channel that can return the up-to-date ServerlessServiceOperationMode periodically.
func FetchSKSMode(
	ctx context.Context, namespace string, selector labels.Selector, ticker *time.Ticker,
) <-chan []netv1alpha1.ServerlessServiceOperationMode {
	sksl := sksinformer.Get(ctx).Lister()
	ch := make(chan []netv1alpha1.ServerlessServiceOperationMode)
	go func() {
		for {
			select {
			case <-ticker.C:
				// Overlay the SKS "mode".
				skses, err := sksl.ServerlessServices("default").List(selector)
				if err != nil {
					log.Printf("Error listing serverless services: %v", err)
					break
				}
				sksms := make([]netv1alpha1.ServerlessServiceOperationMode, len(skses))
				for _, sks := range skses {
					sksms = append(sksms, sks.Spec.Mode)
				}
				ch <- sksms
			}
		}
	}()

	return ch
}
