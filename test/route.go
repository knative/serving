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
	"testing"
	"time"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/pkg/test/spoof"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(logger *logging.BaseLogger, clients *Clients, names ResourceNames) (*v1alpha1.Route, error) {
	route := Route(ServingNamespace, names)
	LogResourceObject(logger, ResourceObjects{Route: route})
	return clients.ServingClient.Routes.Create(route)
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
	newRoute.ObjectMeta = route.ObjectMeta
	LogResourceObject(logger, ResourceObjects{Route: newRoute})
	patchBytes, err := createPatch(route, newRoute)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Routes.Patch(names.Route, types.JSONPatchType, patchBytes, "")
}

// Prober is the interface for a prober, which checks the result of the probes when stopped.
type Prober interface {
	// Stop terminates the prober, and checks for errors, surfacing anything
	// error-worthy as test errors.
	Stop(t *testing.T)
}

type prober struct {
	logger *logging.BaseLogger
	errCh  chan error
}

// prober implements Prober
var _ Prober = (*prober)(nil)

// Stop implements Prober
func (p *prober) Stop(t *testing.T) {
	defer close(p.errCh)
	select {
	case err := <-p.errCh:
		t.Errorf("Prober encountered an error: %v", err)
	default:
		p.logger.Info("No error happens in the Route prober.")
	}
}

// RunRouteProber creates and runs a prober as background goroutine to keep polling Route.
// It stops when getting an error response from Route.
func RunRouteProber(logger *logging.BaseLogger, clients *Clients, domain string) Prober {
	logger.Infof("Starting Route prober for route domain %s.", domain)
	p := &prober{logger: logger, errCh: make(chan error, 1)}
	go func() {
		client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, ServingFlags.ResolvableDomain)
		if err != nil {
			p.errCh <- err
			return
		}
		// ResquestTimeout is set to 0 to make the polling infinite.
		client.RequestTimeout = 0 * time.Minute
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", domain), nil)
		if err != nil {
			p.errCh <- err
			return
		}

		// We keep polling Route if the response status is OK.
		// If the response status is not OK, we stop the prober and
		// generate error based on the response.
		_, err = client.Poll(req, pkgTest.Retrying(disallowsAny, http.StatusOK))
		if err != nil {
			p.errCh <- err
			return
		}
	}()
	return p
}

func disallowsAny(response *spoof.Response) (bool, error) {
	return true, fmt.Errorf("Get unexpected response %v", response)
}
