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

// configuration.go provides methods to perform actions on the configuration object.

package test

import (
	"github.com/knative/pkg/test/logging"
	corev1 "k8s.io/api/core/v1"
)

// Options are test setup parameters.
type Options struct {
	EnvVars              []corev1.EnvVar
	ContainerConcurrency int
}

// CreateConfiguration create a configuration resource in namespace with the name names.Config
// that uses the image specified by imagePath.
func CreateConfiguration(logger *logging.BaseLogger, clients *Clients, names ResourceNames, imagePath string, options *Options) error {
	config := Configuration(ServingNamespace, names, imagePath, options)

	LogResourceObject(logger, ResourceObjects{Configuration: config})
	_, err := clients.ServingClient.Configs.Create(config)
	return err
}
