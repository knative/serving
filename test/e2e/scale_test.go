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
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
)

func testScaleToWithin(t *testing.T, logger *logging.BaseLogger, scale int, duration time.Duration) {
	clients := Setup(t)

	deployGrp, _ := errgroup.WithContext(context.Background())

	domainCh := make(chan string, scale)
	cleanupCh := make(chan test.ResourceNames, scale)
	errCh := make(chan error, 1)
	defer close(cleanupCh)

	logger.Info("Creating new Services")
	for i := 0; i < scale; i++ {

		// https://golang.org/doc/faq#closures_and_goroutines
		i := i

		deployGrp.Go(func() error {
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

			svc, err := test.CreateLatestServiceWithResources(logger, clients, names, options)
			if err != nil {
				return errors.Wrapf(err, "failed to create service %s", names.Service)
			}
			names.Route = serviceresourcenames.Route(svc)
			names.Config = serviceresourcenames.Configuration(svc)

			// Send it to our cleanup logic (below)
			cleanupCh <- names

			logger.Infof("Wait for %s to become ready.", names.Service)
			if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
				return err
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
				return errors.Wrapf(err, "service %s was not updated with a domain", names.Service)
			}

			_, err = pkgTest.WaitForEndpointState(
				clients.KubeClient,
				logger,
				domain,
				pkgTest.Retrying(pkgTest.EventuallyMatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
				"WaitForEndpointToServeText",
				test.ServingFlags.ResolvableDomain)
			if err != nil {
				return errors.Wrapf(err, "the endpoint for Service %s at domain %s didn't serve the expected text %q", names.Service, domain, helloWorldExpectedOutput)
			}
			domainCh <- domain

			logger.Infof("%s is ready.", names.Service)
			return nil
		})
	}

	go func() {
		if err := deployGrp.Wait(); err != nil {
			logger.Errorf("An error occurred during service creation: %v", err)
			errCh <- errors.Wrap(err, "error waiting for endpoints to become ready")
		} else {
			logger.Info("Service creation was successful.")
			// Succeeds the test
			close(domainCh)
		}
	}()

	timeoutCh := time.After(duration)
	for {
		select {
		case names := <-cleanupCh:
			logger.Infof("Added %v to cleanup routine.", names)
			test.CleanupOnInterrupt(func() { TearDown(clients, names, logger) }, logger)
			defer TearDown(clients, names, logger)

		case domain, ok := <-domainCh:
			if !ok {
				logger.Info("All services were created successfully.")
				return
			}
			// Start probing the domain until the test is complete.
			probeCh := test.RunRouteProber(logger, clients, domain)
			defer func(probeCh <-chan error) {
				if err := test.GetRouteProberError(probeCh, logger); err != nil {
					t.Fatalf("Route %q prober failed with error: %v", domain, err)
				}
			}(probeCh)

		case err := <-errCh:
			t.Fatalf("An error occured during the test: %v", err)

		case <-timeoutCh:
			logger.Error("Timeout.")
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
	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestScaleTo10")

	testScaleToWithin(t, logger, 10, 90*time.Second)
}

func TestScaleTo50(t *testing.T) {
	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestScaleTo50")

	testScaleToWithin(t, logger, 50, 5*time.Minute)
}

// A version to customize for more extreme scale testing.
// This should only be checked in commented out.
// func TestScaleToN(t *testing.T) {
// 	//add test case specific name to its own logger
// 	logger := logging.GetContextLogger("TestScaleToN")
//
// 	testScaleToWithin(t, logger, 100, 4*time.Minute)
// }
