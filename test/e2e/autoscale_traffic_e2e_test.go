//go:build e2e
// +build e2e

/*
Copyright 2026 The Knative Authors

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
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	vegeta "github.com/tsenart/vegeta/v12/lib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	nv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/network"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// TestAutoscaleTrafficStopsWithPendingRequest ensures that stopping load also
// releases requests that have reached the service but have not finished. Test
// both the direct and activator paths, including their ingress/mesh proxies.
func TestAutoscaleTrafficStopsWithPendingRequest(t *testing.T) {
	for _, path := range []struct{ name, burstCapacity string }{{"direct", "0"}, {"activator", "-1"}} {
		t.Run(path.name, func(t *testing.T) {
			clients := Setup(t)
			names := test.ResourceNames{Service: test.ObjectNameForTest(t), Image: test.Timeout}
			test.EnsureTearDown(t, clients, &names)
			resources, err := v1test.CreateServiceReady(t, clients, &names, rtesting.WithConfigAnnotations(map[string]string{
				autoscaling.MinScaleAnnotationKey:  "1",
				autoscaling.TargetBurstCapacityKey: path.burstCapacity,
			}))
			if err != nil {
				t.Fatal(err)
			}
			mode := nv1alpha1.SKSOperationModeServe
			if path.name == "activator" {
				mode = nv1alpha1.SKSOperationModeProxy
			}
			if err := wait.PollUntilContextTimeout(t.Context(), 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
				sks, err := clients.NetworkingClient.ServerlessServices.Get(ctx, resources.Revision.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return sks.Spec.Mode == mode && sks.IsReady() && sks.Status.ObservedGeneration == sks.Generation, nil
			}); err != nil {
				t.Fatalf("service did not enter %s mode: %v", mode, err)
			}
			t.Logf("Service is ready in %s mode", mode)

			stop := make(chan struct{})
			var stopOnce sync.Once
			stopTraffic := func() { stopOnce.Do(func() { close(stop) }) }
			defer stopTraffic()
			ctx := &TestContext{t: t, clients: clients}
			client := newVegetaHTTPClient(ctx, resources.Route.Status.URL.URL(), stop)
			transport := client.Transport
			headers := make(chan *http.Response, 1)
			bodyClosed := make(chan struct{})
			var bodyCloseOnce sync.Once
			client.Transport = network.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				resp, err := transport.RoundTrip(r)
				if err == nil {
					resp.Body = &trafficResponseBody{ReadCloser: resp.Body, closed: bodyClosed, once: &bodyCloseOnce}
					headers <- resp
				}
				return resp, err
			})
			attacker := vegeta.NewAttacker(vegeta.Client(client), vegeta.Timeout(0), vegeta.Workers(1), vegeta.MaxWorkers(1))
			// The timeout image flushes response headers, then waits a minute
			// before writing the body. Receiving headers proves the request
			// traversed the real data path, rather than being canceled pre-dial.
			target := getVegetaTarget(resources.Route.Status.URL.Host, "timeout", 60000, test.ServingFlags.HTTPS)
			done := make(chan error, 1)
			go func() {
				done <- generateTraffic(ctx, attacker, vegeta.ConstantPacer{}, stop, target)
			}()
			select {
			case resp := <-headers:
				t.Cleanup(func() { resp.Body.Close() })
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("response status = %d, want 200", resp.StatusCode)
				}
				t.Log("Received response headers; body is still pending for 60 seconds")
			case <-time.After(30 * time.Second):
				t.Fatal("did not receive response headers")
			}

			started := time.Now()
			stopTraffic()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("traffic did not stop with a pending response body")
			}
			select {
			case <-bodyClosed:
				t.Logf("Stopped traffic and closed pending response in %v", time.Since(started))
			default:
				t.Fatal("traffic returned but left the response body and worker running")
			}
		})
	}
}

// trafficResponseBody records when Vegeta releases its pending response.
type trafficResponseBody struct {
	io.ReadCloser
	closed chan struct{}
	once   *sync.Once
}

func (b *trafficResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { close(b.closed) })
	return err
}
