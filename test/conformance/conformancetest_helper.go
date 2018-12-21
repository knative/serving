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

//runtime_conformance_helper.go contains helper methods used by conformance tests that verify runtime-contract.

package conformance

import (
	"fmt"
	"net/http"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/service/resources/names"
	"github.com/knative/serving/test"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fetchEnvInfo creates the service using test_images/environment and fetches environment info defined inside the container dictated by urlPath.
func fetchEnvInfo(clients *test.Clients, logger *logging.BaseLogger, urlPath string, options *test.Options) ([]byte, *test.ResourceNames, error) {
	logger.Info("Creating a new Service")
	var names test.ResourceNames
	names.Service = test.AppendRandomString("yashiki", logger)
	names.Image = "environment"
	svc, err := test.CreateLatestService(logger, clients, names, options)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("Failed to create Service: %v", err))
	}
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)
	defer tearDown(clients, names)

	var revisionName string
	logger.Info("The Service will be updated with the name of the Revision once it is created")
	err = test.WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("Service %s was not updated with the new revision: %v", names.Service, err))
	}
	names.Revision = revisionName

	logger.Info("When the Service reports as Ready, everything should be ready.")
	if err := test.WaitForServiceState(clients.ServingClient, names.Service, test.IsServiceReady, "ServiceIsReady"); err != nil {
		return nil, nil, errors.New(fmt.Sprintf("The Service %s was not marked as Ready to serve traffic to Revision %s: %v", names.Service, names.Revision, err))
	}

	logger.Infof("When the Revision can have traffic routed to it, the Route is marked as Ready.")
	if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		return nil, nil, errors.New(fmt.Sprintf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err))
	}

	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("Error fetching Route %s: %v", names.Route, err))
	}

	url := route.Status.Domain + urlPath
	resp, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		url,
		pkgTest.Retrying(func(resp *spoof.Response) (bool, error) {
			if resp.StatusCode == http.StatusOK {
				return true, nil
			}

			return true, errors.New(string(resp.Body))
		}, http.StatusNotFound),
		"EnvVarsServesText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		return nil, nil, errors.New(fmt.Sprintf("Failed before reaching desired state : %v", err))
	}

	return resp.Body, &names, nil
}
