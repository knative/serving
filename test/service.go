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
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/mattbaird/jsonpatch"
	"k8s.io/apimachinery/pkg/types"
)

// CreateLatestService creates a service in namespace with the name names.Service
// that uses the image specified by imagePath
func CreateLatestService(logger *logging.BaseLogger, clients *Clients, names ResourceNames, imagePath string) (*v1alpha1.Service, error) {
	service := LatestService(ServingNamespace, names, imagePath)
	LogResourceObject(logger, ResourceObjects{Service: service})
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}

// UpdateReleaseService updates an existing service in namespace with the name names.Service
func UpdateReleaseService(logger *logging.BaseLogger, clients *Clients, svc *v1alpha1.Service, revisions []string, rolloutPercent int) (*v1alpha1.Service, error) {
	newSvc := ReleaseService(svc, revisions, rolloutPercent)
	LogResourceObject(logger, ResourceObjects{Service: newSvc})
	patchBytes, err := createPatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	patchedSvc, err := clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
	return patchedSvc, err
}

// UpdateManualService updates an existing service in namespace with the name names.Service
func UpdateManualService(logger *logging.BaseLogger, clients *Clients, svc *v1alpha1.Service) (*v1alpha1.Service, error) {
	newSvc := ManualService(svc)
	LogResourceObject(logger, ResourceObjects{Service: newSvc})
	patchBytes, err := createPatch(svc, newSvc)
	if err != nil {
		return nil, err
	}
	patchedSvc, err := clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
	return patchedSvc, err
}

func createPatch(cur, desired *v1alpha1.Service) ([]byte, error) {
	curJson, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}
	desiredJson, err := json.Marshal(desired)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.CreatePatch(curJson, desiredJson)
	if err != nil {
		return nil, err
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return patchBytes, nil
}

func UpdateServiceImage(clients *Clients, svc *v1alpha1.Service, imagePath string) error {
	var serviceType string
	if svc.Spec.RunLatest != nil {
		serviceType = "runLatest"
	} else if svc.Spec.Release != nil {
		serviceType = "release"
	} else if svc.Spec.Pinned != nil {
		serviceType = "pinned"
	} else {
		return fmt.Errorf("UpdateImageService(%v): unable to determine service type", svc)
	}
	patches := []jsonpatch.JsonPatchOperation{
		{
			Operation: "replace",
			Path:      fmt.Sprintf("/spec/%s/configuration/revisionTemplate/spec/container/image", serviceType),
			Value:     imagePath,
		},
	}
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return err
	}
	_, err = clients.ServingClient.Services.Patch(svc.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
	if err != nil {
		return err
	}
	return nil
}
