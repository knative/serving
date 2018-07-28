/*
Copyright 2018 Google Inc. All Rights Reserved.
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

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"go.uber.org/zap"
)

// CreateLatestService creates a service in namespace with the name names.Service
// that uses the image specified by imagePath
func CreateLatestService(logger *zap.SugaredLogger, clients *Clients, names ResourceNames, imagePath string) (*v1alpha1.Service, error) {
	service := LatestService(Flags.Namespace, names, imagePath)
	// Log the service object
	if serviceJSON, err := json.Marshal(service); err != nil {
		logger.Infof("Failed to create json from service object: %v", err)
	} else {
		logger.Infow("Created resource object", "SERVICE", string(serviceJSON))
	}

	svc, err := clients.Services.Create(service)
	return svc, err
}
