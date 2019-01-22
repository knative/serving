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
	"fmt"
	"net/http"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/knative/serving/pkg/pool"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func testScaleToWithin(t *testing.T, logger *logging.BaseLogger, scale int, duration time.Duration) {
	clients := Setup(t)

	cleanupCh := make(chan test.ResourceNames, scale)
	defer close(cleanupCh)

	fopt := []ServiceOption{
		// We set a small resource alloc so that we can pack more pods into the cluster.
		func(svc *v1alpha1.Service) {
			svc.Spec.RunLatest.Configuration.RevisionTemplate.Spec.Container.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("10m"),
					corev1.ResourceMemory: resource.MustParse("20Mi"),
				},
			}
		},
		// See #2946 for why we do this.
		func(svc *v1alpha1.Service) {
			svc.Spec.RunLatest.Configuration.RevisionTemplate.ObjectMeta.Annotations = map[string]string{
				"autoscaling.knative.dev/minScale": "1",
				"autoscaling.knative.dev/maxScale": "1",
			}
		},
	}
	// These are the local (per-probe) and global (all probes) targets for the scale test.
	// 95 = 19/20, so allow a single failure with the minimum number of probes, but expect
	// us to have 3.5 9s overall.
	const (
		localSLO  = 0.95
		globalSLO = 0.9995
		minProbes = 20
	)
	pm := test.NewProberManager(logger, clients, minProbes)

	timeoutCh := time.After(duration)

	logger.Info("Creating new Services")
	wg := pool.NewWithCapacity(50 /* maximum in-flight creates */, scale /* capacity */)
	for i := 0; i < scale; i++ {
		// https://golang.org/doc/faq#closures_and_goroutines
		i := i

		wg.Go(func() error {
			names := test.ResourceNames{
				Service: test.AppendRandomString(fmt.Sprintf("scale-%05d-%03d-", scale, i), logger),
				Image:   "helloworld",
			}

			options := &test.Options{
				// Give each request 10 seconds to respond.
				// This is mostly to work around #2897
				RevisionTimeoutSeconds: 10,
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "/",
						},
					},
				},
			}

			svc, err := test.CreateLatestService(logger, clients, names, options, fopt...)
			if err != nil {
				t.Errorf("CreateLatestService() = %v", err)
				return nil
			}
			names.Route = serviceresourcenames.Route(svc)
			names.Config = serviceresourcenames.Configuration(svc)

			// Send it to our cleanup logic (below)
			cleanupCh <- names

			logger.Infof("Wait for %s to become ready.", names.Service)
			if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
				t.Errorf("WaitForServiceState(IsReady) = %v", err)
				return nil
			}

			var domain string
			err = test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
				if s.Status.Domain != "" {
					domain = s.Status.Domain
					return true, nil
				}
				return false, nil
			}, "ServiceUpdatedWithDomain")
			if err != nil {
				t.Errorf("WaitForServiceState(w/ Domain) = %v", err)
				return nil
			}

			_, err = pkgTest.WaitForEndpointState(
				clients.KubeClient,
				logger,
				domain,
				pkgTest.Retrying(pkgTest.EventuallyMatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
				"WaitForEndpointToServeText",
				test.ServingFlags.ResolvableDomain)
			if err != nil {
				t.Errorf("WaitForEndpointState(expected text) = %v", err)
				return nil
			}
			// Start probing the domain until the test is complete.
			pm.Spawn(domain)

			logger.Infof("%s is ready.", names.Service)
			return nil
		})
	}

	// Wait for all of the service creations to complete (possibly in failure),
	// and signal the done channel.
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		if err := wg.Wait(); err != nil {
			t.Fatalf("hmm, this go routine never returns errors: %v", err)
		}
	}()

	for {
		// As services get created, add logic to clean them up.
		// When all of the creations have finished, then stop all of the active probers
		// and check our SLIs against our SLOs.
		// All of this has to finish within the configured timeout.
		select {
		case names := <-cleanupCh:
			logger.Infof("Added %v to cleanup routine.", names)
			test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
			defer TearDown(clients, names, logger)

		case <-doneCh:
			// This ProberManager implementation waits for minProbes before actually stopping.
			if err := pm.Stop(); err != nil {
				t.Fatalf("Stop() = %v", err)
			}
			// Check each of the local SLOs
			pm.Foreach(func(domain string, p test.Prober) {
				if err := test.CheckSLO(localSLO, domain, p); err != nil {
					t.Errorf("CheckSLO() = %v", err)
				}
			})
			// Check the global SLO
			if err := test.CheckSLO(globalSLO, "aggregate", pm); err != nil {
				t.Errorf("CheckSLO() = %v", err)
			}
			return

		case <-timeoutCh:
			// If we don't do this first, then we'll see tons of 503s from the ongoing probes
			// as we tear down the things they are probing.
			defer pm.Stop()
			t.Fatalf("Timed out waiting for %d services to become ready", scale)
		}
	}
}

// While redundant, we run two versions of this by default:
// 1. TestScaleTo10: a developer smoke test that's useful when changing this to assess whether
//   things have gone horribly wrong.  This should take about 12-20 seconds total.
// 2. TestScaleTo50: a more proper execution of the test, which verifies a slightly more
//   interesting burst of deployments, but low enough to complete in a reasonable window.

func TestScaleTo10(t *testing.T) {
	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestScaleTo10")

	testScaleToWithin(t, logger, 10, 60*time.Second)
}

func TestScaleTo50(t *testing.T) {
	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger("TestScaleTo50")

	testScaleToWithin(t, logger, 50, 5*time.Minute)
}

// A version to customize for more extreme scale testing.
// This should only be checked in commented out.
// func TestScaleToN(t *testing.T) {
// 	// Add test case specific name to its own logger.
// 	logger := logging.GetContextLogger("TestScaleToN")
//
// 	testScaleToWithin(t, logger, 200, 4*time.Minute)
// }
