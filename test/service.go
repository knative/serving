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

// service.go provides methods to perform actions on the service resource.

package test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/knative/pkg/apis/duck"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	serviceresourcenames "github.com/knative/serving/pkg/reconciler/service/resources/names"
	"github.com/mattbaird/jsonpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	rtesting "github.com/knative/serving/pkg/reconciler/testing"
)

// TODO(dangerd): Move function to duck.CreateBytePatch
func createPatch(cur, desired interface{}) ([]byte, error) {
	patch, err := duck.CreatePatch(cur, desired)
	if err != nil {
		return nil, err
	}
	return patch.MarshalJSON()
}

func validateCreatedServiceStatus(clients *Clients, names *ResourceNames) error {
	return CheckServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
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
func GetResourceObjects(clients *Clients, names ResourceNames) (*ResourceObjects, error) {
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

// CreateRunLatestServiceReady creates a new Service in state 'Ready'. This function expects Service and Image name passed in through 'names'.
// Names is updated with the Route and Configuration created by the Service and ResourceObjects is returned with the Service, Route, and Configuration objects.
// Returns error if the service does not come up correctly.
func CreateRunLatestServiceReady(t *testing.T, clients *Clients, names *ResourceNames, options *Options, fopt ...rtesting.ServiceOption) (*ResourceObjects, error) {
	if names.Image == "" {
		return nil, fmt.Errorf("expected non-empty Image name; got Image=%v", names.Image)
	}

	t.Logf("Creating a new Service %s.", names.Service)
	svc, err := CreateLatestService(t, clients, *names, options, fopt...)
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
	if err := WaitForServiceState(clients.ServingClient, names.Service, IsServiceReady, "ServiceIsReady"); err != nil {
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

// CreateLatestService creates a service in namespace with the name names.Service and names.Image
func CreateLatestService(t *testing.T, clients *Clients, names ResourceNames, options *Options, fopt ...rtesting.ServiceOption) (*v1alpha1.Service, error) {
	service := LatestService(names, options, fopt...)
	LogResourceObject(t, ResourceObjects{Service: service})
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}

// CreateLatestServiceLegacy creates a service in namespace with the name names.Service and names.Image
func CreateLatestServiceLegacy(t *testing.T, clients *Clients, names ResourceNames, options *Options, fopt ...rtesting.ServiceOption) (*v1alpha1.Service, error) {
	service := LatestServiceLegacy(names, options, fopt...)
	LogResourceObject(t, ResourceObjects{Service: service})
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}

// PatchServiceImage patches the existing service passed in with a new imagePath. Returns the latest service object
func PatchServiceImage(t *testing.T, clients *Clients, svc *v1alpha1.Service, imagePath string) (*v1alpha1.Service, error) {
	newSvc := svc.DeepCopy()
	if svc.Spec.DeprecatedRunLatest != nil {
		newSvc.Spec.DeprecatedRunLatest.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else if svc.Spec.DeprecatedRelease != nil {
		newSvc.Spec.DeprecatedRelease.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else if svc.Spec.DeprecatedPinned != nil {
		newSvc.Spec.DeprecatedPinned.Configuration.GetTemplate().Spec.GetContainer().Image = imagePath
	} else if svc.Spec.DeprecatedManual != nil {
		return nil, fmt.Errorf("UpdateImageService(%v): manual is not supported", svc)
	} else {
		newSvc.Spec.ConfigurationSpec.GetTemplate().Spec.GetContainer().Image = imagePath
	}
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := createPatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// PatchService creates and applies a patch from the diff between curSvc and desiredSvc. Returns the latest service object.
func PatchService(t *testing.T, clients *Clients, curSvc *v1alpha1.Service, desiredSvc *v1alpha1.Service) (*v1alpha1.Service, error) {
	LogResourceObject(t, ResourceObjects{Service: desiredSvc})
	patchBytes, err := createPatch(curSvc, desiredSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Services.Patch(curSvc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// UpdateServiceRouteSpec updates a service to use the route name in names.
func UpdateServiceRouteSpec(t *testing.T, clients *Clients, names ResourceNames, rs v1alpha1.RouteSpec) (*v1alpha1.Service, error) {
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

// PatchServiceTemplateMetadata patches an existing service by adding metadata to the service's RevisionTemplateSpec.
func PatchServiceTemplateMetadata(t *testing.T, clients *Clients, svc *v1alpha1.Service, metadata metav1.ObjectMeta) (*v1alpha1.Service, error) {
	newSvc := svc.DeepCopy()
	newSvc.Spec.ConfigurationSpec.Template.ObjectMeta = metadata
	LogResourceObject(t, ResourceObjects{Service: newSvc})
	patchBytes, err := createPatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	return clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// WaitForServiceLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Service.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForServiceLatestRevision(clients *Clients, names ResourceNames) (string, error) {
	var revisionName string
	err := WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		if s.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = s.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ServiceUpdatedWithRevision")
	if err != nil {
		return "", err
	}
	err = WaitForServiceState(clients.ServingClient, names.Service, func(s *v1alpha1.Service) (bool, error) {
		return (s.Status.LatestReadyRevisionName == revisionName), nil
	}, "ServiceReadyWithRevision")

	return revisionName, err
}
