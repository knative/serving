/*
Copyright 2019 The Knative Authors

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

package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mattbaird/jsonpatch"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ptest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1beta1"
	"knative.dev/serving/test"
)

func validateCreatedServiceStatus(clients *test.Clients, names *test.ResourceNames) error {
	return CheckServiceState(clients.ServingBetaClient, names.Service, func(s *v1beta1.Service) (bool, error) {
		if s.Status.URL == nil || s.Status.URL.Host == "" {
			return false, fmt.Errorf("url is not present in Service status: %v", s)
		}
		names.Domain = s.Status.URL.Host
		if s.Status.LatestCreatedRevisionName == "" {
			return false, fmt.Errorf("lastCreatedRevision is not present in Service status: %v", s)
		}
		names.Revision = s.Status.LatestCreatedRevisionName
		if s.Status.LatestReadyRevisionName == "" {
			return false, fmt.Errorf("lastReadyRevision is not present in Service status: %v", s)
		}
		if s.Status.ObservedGeneration != 1 {
			return false, fmt.Errorf("observedGeneration is not 1 in Service status: %v", s)
		}
		return true, nil
	})
}

// GetResourceObjects obtains the services resources from the k8s API server.
func GetResourceObjects(clients *test.Clients, names test.ResourceNames) (*ResourceObjects, error) {
	routeObject, err := clients.ServingBetaClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	serviceObject, err := clients.ServingBetaClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	configObject, err := clients.ServingBetaClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	revisionObject, err := clients.ServingBetaClient.Revisions.Get(names.Revision, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &ResourceObjects{
		Route:    routeObject,
		Service:  serviceObject,
		Config:   configObject,
		Revision: revisionObject,
	}, nil
}

// CreateServiceReady creates a new Service in state 'Ready'. This function expects Service and Image name
// passed in through 'names'.  Names is updated with the Route and Configuration created by the Service
// and ResourceObjects is returned with the Service, Route, and Configuration objects.
// Returns error if the service does not come up correctly.
func CreateServiceReady(t *testing.T, clients *test.Clients, names *test.ResourceNames, fopt ...rtesting.ServiceOption) (*ResourceObjects, error) {
	if names.Image == "" {
		return nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Logf("Creating a new Service %s.", names.Service)
	svc, err := CreateService(t, clients, *names, fopt...)
	if err != nil {
		return nil, err
	}

	// Populate Route and Configuration Objects with name
	names.Route = serviceresourcenames.Route(svc)
	names.Config = serviceresourcenames.Configuration(svc)

	// If the Service name was not specified, populate it
	if names.Service == "" {
		names.Service = svc.Name
	}

	t.Logf("Waiting for Service %q to transition to Ready.", names.Service)
	if err := WaitForServiceState(clients.ServingBetaClient, names.Service, IsServiceReady, "ServiceIsReady"); err != nil {
		return nil, err
	}

	t.Log("Checking to ensure Service Status is populated for Ready service", names.Service)
	err = validateCreatedServiceStatus(clients, names)
	if err != nil {
		return nil, err
	}

	t.Log("Getting latest objects Created by Service", names.Service)
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, err
}

// CreateService creates a service in namespace with the name names.Service and names.Image
func CreateService(t *testing.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ServiceOption) (*v1beta1.Service, error) {
	service := Service(names, fopt...)
	LogResourceObject(t, ResourceObjects{Service: service})
	svc, err := clients.ServingBetaClient.Services.Create(service)
	return svc, err
}

// PatchService patches the existing service passed in with the applied mutations.
// Returns the latest service object
func PatchService(t *testing.T, clients *test.Clients, svc *v1beta1.Service, fopt ...rtesting.ServiceOption) (*v1beta1.Service, error) {
	newSvc := svc.DeepCopy()
	for _, opt := range fopt {
		opt(newSvc)
	}
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := createPatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingBetaClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// UpdateServiceRouteSpec updates a service to use the route name in names.
func UpdateServiceRouteSpec(t *testing.T, clients *test.Clients, names test.ResourceNames, rs v1.RouteSpec) (*v1beta1.Service, error) {
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/traffic",
		Value:     rs.Traffic,
	}}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	return clients.ServingBetaClient.Services.Patch(names.Service, types.JSONPatchType, patchBytes, "")
}

// WaitForServiceLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Service.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForServiceLatestRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	err := WaitForServiceState(clients.ServingBetaClient, names.Service, func(s *v1beta1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	if err != nil {
		return "", err
	}
	err = WaitForServiceState(clients.ServingBetaClient, names.Service, func(s *v1beta1.Service) (bool, error) {
		return (s.Status.LatestReadyRevisionName == revisionName), nil
	}, "ServiceReadyWithRevision")

	return revisionName, err
}

// Service returns a Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func Service(names test.ResourceNames, fopt ...rtesting.ServiceOption) *v1beta1.Service {
	a := append([]rtesting.ServiceOption{
		rtesting.WithInlineConfigSpec(*ConfigurationSpec(ptest.ImagePath(names.Image))),
	}, fopt...)
	return rtesting.ServiceWithoutNamespace(names.Service, a...)
}

// WaitForServiceState polls the status of the Service called name
// from client every `interval` until `inState` returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForServiceState(client *test.ServingBetaClients, name string, inState func(s *v1beta1.Service) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForServiceState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1beta1.Service
	waitErr := wait.PollImmediate(interval, timeout, func() (bool, error) {
		var err error
		lastState, err = client.Services.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return errors.Wrapf(waitErr, "service %q is not in desired state, got: %+v", name, lastState)
	}
	return nil
}

// CheckServiceState verifies the status of the Service called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForServiceState.
func CheckServiceState(client *test.ServingBetaClients, name string, inState func(s *v1beta1.Service) (bool, error)) error {
	s, err := client.Services.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if done, err := inState(s); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("service %q is not in desired state, got: %+v", name, s)
	}
	return nil
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1beta1.Service) (bool, error) {
	return s.Generation == s.Status.ObservedGeneration && s.Status.IsReady(), nil
}

// IsServiceNotReady will check the status conditions of the service and return true if the service is
// not ready.
func IsServiceNotReady(s *v1beta1.Service) (bool, error) {
	return s.Generation == s.Status.ObservedGeneration && !s.Status.IsReady(), nil
}
