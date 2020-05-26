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

package v1

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mattbaird/jsonpatch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/pkg/apis/duck"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	serviceresourcenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"

	_ "knative.dev/pkg/metrics/testing"
)

func validateCreatedServiceStatus(clients *test.Clients, names *test.ResourceNames) error {
	return CheckServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
		if s.Status.URL == nil || s.Status.URL.Host == "" {
			return false, fmt.Errorf("url is not present in Service status: %v", s)
		}
		names.URL = s.Status.URL.URL()
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
	routeObject, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	serviceObject, err := clients.ServingClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	configObject, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	revisionObject, err := clients.ServingClient.Revisions.Get(names.Revision, metav1.GetOptions{})
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
func CreateServiceReady(t pkgTest.T, clients *test.Clients, names *test.ResourceNames, fopt ...rtesting.ServiceOption) (*ResourceObjects, error) {
	if names.Image == "" {
		return nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Log("Creating a new Service", "service", names.Service)
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

	t.Log("Waiting for Service to transition to Ready.", "service", names.Service)
	if err := WaitForServiceState(clients.ServingClient, names.Service, IsServiceReady, "ServiceIsReady"); err != nil {
		return nil, err
	}

	t.Log("Checking to ensure Service Status is populated for Ready service")
	err = validateCreatedServiceStatus(clients, names)
	if err != nil {
		return nil, err
	}

	t.Log("Getting latest objects Created by Service")
	resources, err := GetResourceObjects(clients, *names)
	if err == nil {
		t.Log("Successfully created Service", names.Service)
	}
	return resources, err
}

// CreateService creates a service in namespace with the name names.Service and names.Image
func CreateService(t pkgTest.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ServiceOption) (*v1.Service, error) {
	service := Service(names, fopt...)
	test.AddTestAnnotation(t, service.ObjectMeta)
	LogResourceObject(t, ResourceObjects{Service: service})
	return clients.ServingClient.Services.Create(service)
}

// PatchService patches the existing service passed in with the applied mutations.
// Returns the latest service object
func PatchService(t pkgTest.T, clients *test.Clients, svc *v1.Service, fopt ...rtesting.ServiceOption) (*v1.Service, error) {
	newSvc := svc.DeepCopy()
	for _, opt := range fopt {
		opt(newSvc)
	}
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := duck.CreateBytePatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// UpdateServiceRouteSpec updates a service to use the route name in names.
func UpdateServiceRouteSpec(t pkgTest.T, clients *test.Clients, names test.ResourceNames, rs v1.RouteSpec) (*v1.Service, error) {
	patches := []jsonpatch.JsonPatchOperation{{
		Operation: "replace",
		Path:      "/spec/traffic",
		Value:     rs.Traffic,
	}}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Services.Patch(names.Service, types.JSONPatchType, patchBytes, "")
}

// WaitForServiceLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Service.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForServiceLatestRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	if err := WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			// We also check that the revision is pinned, meaning it's not a stale revision.
			// Without this it might happen that the latest created revision is later overridden by a newer one
			// and the following check for LatestReadyRevisionName would fail.
			if revErr := CheckRevisionState(clients.ServingClient, revisionName, IsRevisionPinned); revErr != nil {
				return false, nil
			}
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision"); err != nil {
		return "", fmt.Errorf("LatestCreatedRevisionName not updated: %w", err)
	}
	if err := WaitForServiceState(clients.ServingClient, names.Service, func(s *v1.Service) (bool, error) {
		return (s.Status.LatestReadyRevisionName == revisionName), nil
	}, "ServiceReadyWithRevision"); err != nil {
		return "", fmt.Errorf("LatestReadyRevisionName not updated with %s: %w", revisionName, err)
	}

	return revisionName, nil
}

// Service returns a Service object in namespace with the name names.Service
// that uses the image specified by names.Image.
func Service(names test.ResourceNames, fopt ...rtesting.ServiceOption) *v1.Service {
	a := append([]rtesting.ServiceOption{
		rtesting.WithConfigSpec(*ConfigurationSpec(pkgTest.ImagePath(names.Image))),
	}, fopt...)
	return rtesting.ServiceWithoutNamespace(names.Service, a...)
}

// WaitForServiceState polls the status of the Service called name
// from client every `PollInterval` until `inState` returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForServiceState(client *test.ServingClients, name string, inState func(s *v1.Service) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForServiceState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1.Service
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Services.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("service %q is not in desired state, got: %#v: %w", name, lastState, waitErr)
	}
	return nil
}

// CheckServiceState verifies the status of the Service called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForServiceState.
func CheckServiceState(client *test.ServingClients, name string, inState func(s *v1.Service) (bool, error)) error {
	s, err := client.Services.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if done, err := inState(s); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("service %q is not in desired state, got: %#v", name, s)
	}
	return nil
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1.Service) (bool, error) {
	return s.Generation == s.Status.ObservedGeneration && s.Status.IsReady(), nil
}

// IsServiceNotReady will check the status conditions of the service and return true if the service is
// not ready.
func IsServiceNotReady(s *v1.Service) (bool, error) {
	result := s.Status.GetCondition(v1.ServiceConditionReady)
	return s.Generation == s.Status.ObservedGeneration && result != nil && result.Status == corev1.ConditionFalse, nil
}

// IsServiceRoutesNotReady checks the RoutesReady status of the service and returns true only if RoutesReady is set to False.
func IsServiceRoutesNotReady(s *v1.Service) (bool, error) {
	result := s.Status.GetCondition(v1.ServiceConditionRoutesReady)
	return s.Generation == s.Status.ObservedGeneration && result != nil && result.Status == corev1.ConditionFalse, nil
}
