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

	"github.com/knative/serving/test/logging"
	"github.com/knative/serving/test/spoof"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *logging.BaseLogger, clients *Clients, names ResourceNames) error {
	route := Route(Flags.Namespace, names)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.ServingClient.Routes.Create(route)
	return err
}

// CreateBlueGreenRoute creates a route in the given namespace using the route name in names.
// Traffic is evenly split between the two routes specified by blue and green.
func CreateBlueGreenRoute(logger *logging.BaseLogger, clients *Clients, names, blue, green ResourceNames) error {
	route := BlueGreenRoute(Flags.Namespace, names, blue, green)
	LogResourceObject(logger, ResourceObjects{Route: route})
	_, err := clients.ServingClient.Routes.Create(route)
	return err
}

// RunRouteProber creates and runs a prober as background goroutine to keep polling Route.
// It stops when getting an error response from Route.
func RunRouteProber(logger *logging.BaseLogger, clients *Clients, domain string) <-chan error {
	logger.Infof("Starting Route prober for route domain %s.", domain)
	errorChan := make(chan error, 1)
	go func() {
		client, err := spoof.New(clients.KubeClient.Kube, logger, domain, ServingFlags.ResolvableDomain)
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
		_, err = client.Poll(req, Retrying(disallowsAny, http.StatusOK))
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
	return true, fmt.Errorf("Get unexpected response %v.", response)
}
