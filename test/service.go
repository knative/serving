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
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// CreateLatestService creates a service in namespace with the name names.Service
// that uses the image specified by imagePath
func CreateLatestService(logger *logging.BaseLogger, clients *Clients, names ResourceNames, imagePath string) (*v1alpha1.Service, error) {
	service := LatestService(Flags.Namespace, names, imagePath)
	LogResourceObject(logger, ResourceObjects{Service: service})
	svc, err := clients.ServingClient.Services.Create(service)
	return svc, err
}
