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
package v1new

import (
	"context"
	"fmt"
	"net/url"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclientset "knative.dev/serving/pkg/client/clientset/versioned"
	servingv1 "knative.dev/serving/pkg/client/clientset/versioned/typed/serving/v1"
)

// Normally we'd import things from knative.dev/serving/test but that pulls in global
// flags which we wish to avoid. So I'm coping common code here

type ResourceNames struct {
	Config        string
	Route         string
	Revision      string
	Service       string
	TrafficTarget string
	URL           *url.URL
	Image         string
}

type ServingClients struct {
	Services servingv1.ServiceInterface
}

// newServingClients instantiates and returns the serving clientset required to make requests to the
// knative serving cluster.
func newServingClients(cfg *rest.Config, namespace string) (*ServingClients, error) {
	cs, err := servingclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &ServingClients{
		Services: cs.ServingV1().Services(namespace),
	}, nil
}

func (clients *ServingClients) Delete(routes, configs, services []string) []error {
	deletions := []struct {
		client interface {
			Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
		}
		items []string
	}{
		// Delete services first, since we otherwise might delete a route/configuration
		// out from under the ksvc
		{clients.Services, services},
	}

	propPolicy := metav1.DeletePropagationForeground
	dopt := metav1.DeleteOptions{
		PropagationPolicy: &propPolicy,
	}

	var errs []error
	for _, deletion := range deletions {
		if deletion.client == nil {
			continue
		}

		for _, item := range deletion.items {
			if item == "" {
				continue
			}

			if err := deletion.client.Delete(context.Background(), item, dopt); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1.Service) (bool, error) {
	return s.IsReady(), nil
}

const PollInterval = 1 * time.Second
const PollTimeout = 10 * time.Minute

// WaitForServiceState polls the status of the Service called name
// from client every `PollInterval` until `inState` returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForReadyService(client *ServingClients, name string) error {
	var lastState *v1.Service
	waitErr := wait.PollImmediate(PollInterval, PollTimeout, func() (bool, error) {
		err := reconciler.RetryTestErrors(func(int) (err error) {
			lastState, err = client.Services.Get(context.Background(), name, metav1.GetOptions{})
			return err
		})
		if err != nil {
			return true, err
		}
		return IsServiceReady(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("service %q is not in desired state, got: %#v: %w", name, lastState, waitErr)
	}
	return nil
}
