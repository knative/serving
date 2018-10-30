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

// route.go provides methods to perform actions on the route resource.

package test

import (
	"fmt"
	"net/http"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *logging.BaseLogger, clients *Clients, names ResourceNames) error {
	route := Route(ServingNamespace, names)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.ServingClient.Routes.Create(route)
	return err
}

// CreateBlueGreenRoute creates a route in the given namespace using the route name in names.
// Traffic is evenly split between the two routes specified by blue and green.
func CreateBlueGreenRoute(logger *logging.BaseLogger, clients *Clients, names, blue, green ResourceNames) error {
	route := BlueGreenRoute(ServingNamespace, names, blue, green)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.ServingClient.Routes.Create(route)
	return err
}

// UpdateRoute updates a route in the given namespace using the route name in names
func UpdateBlueGreenRoute(logger *logging.BaseLogger, clients *Clients, names, blue, green ResourceNames) (*v1alpha1.Route, error) {
	route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newRoute := BlueGreenRoute(ServingNamespace, names, blue, green)
	newRoute.ObjectMeta.ResourceVersion = route.ObjectMeta.ResourceVersion
	LogResourceObject(logger, ResourceObjects{Route: newRoute})
	patchBytes, err := createPatch(route, newRoute)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Routes.Patch(names.Route, types.JSONPatchType, patchBytes, "")
}

// ProbeDomain sends requests to a domain until we get a successful
// response. This ensures the domain is routable before we send it a
// bunch of traffic.
func ProbeDomain(logger *logging.BaseLogger, clients *Clients, domain string) error {
	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, ServingFlags.ResolvableDomain)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
	if err != nil {
		return err
	}
	// TODO(tcnghia): Replace this probing with Status check when we have them.
	_, err = client.Poll(req, pkgTest.Retrying(pkgTest.MatchesAny, http.StatusNotFound, http.StatusServiceUnavailable))
	return err
}

// RunRouteProber creates and runs a prober as background goroutine to keep polling Route.
// It stops when getting an error response from Route.
func RunRouteProber(logger *logging.BaseLogger, clients *Clients, domain string) <-chan error {
	logger.Infof("Starting Route prober for route domain %s.", domain)
	errorChan := make(chan error, 1)
	go func() {
		client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, ServingFlags.ResolvableDomain)
		if err != nil {
			errorChan <- err
			close(errorChan)
			return
		}
		// ResquestTimeout is set to 0 to make the polling infinite.
		client.RequestTimeout = 0 * time.Minute
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
		if err != nil {
			errorChan <- err
			close(errorChan)
			return
		}

		// We keep polling Route if the response status is OK.
		// If the response status is not OK, we stop the prober and
		// generate error based on the response.
		_, err = client.Poll(req, pkgTest.Retrying(disallowsAny, http.StatusOK))
		if err != nil {
			errorChan <- err
			close(errorChan)
			return
		}
	}()
	return errorChan
}

// GetRouteProberError gets the error of route prober.
func GetRouteProberError(errorChan <-chan error, logger *logging.BaseLogger) error {
	select {
	case err := <-errorChan:
		return err
	default:
		logger.Info("No error happens in the Route prober.")
		return nil
	}
}

func disallowsAny(response *spoof.Response) (bool, error) {
	return true, fmt.Errorf("Get unexpected response %v", response)
}
