/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package deployment

import (
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// ConfigName is the name of config map for the deployment.
	ConfigName = "config-deployment"

	// QueueSidecarImageKey is the config map key for queue sidecar image
	QueueSidecarImageKey = "queueSidecarImage"

	// ProgressDeadlineDefault is the default value for the config's
	// ProgressDeadlineSeconds. This does not match the K8s default value of 600s.
	ProgressDeadlineDefault = 120 * time.Second

	registriesSkippingTagResolvingKey = "registriesSkippingTagResolving"

	// ProgressDeadlineKey is the key to configure deployment progress deadline.
	ProgressDeadlineKey = "progressDeadline"
)

func defaultConfig() *Config {
	return &Config{
		ProgressDeadline:               ProgressDeadlineDefault,
		RegistriesSkippingTagResolving: sets.NewString("ko.local", "dev.local"),
	}
}

// NewConfigFromMap creates a DeploymentConfig from the supplied Map
func NewConfigFromMap(configMap map[string]string) (*Config, error) {
	nc := defaultConfig()
	qsideCarImage, ok := configMap[QueueSidecarImageKey]
	if !ok {
		return nil, errors.New("queue sidecar image is missing")
	}
	nc.QueueSidecarImage = qsideCarImage

	if pd, ok := configMap[ProgressDeadlineKey]; ok {
		v, err := time.ParseDuration(pd)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s=%s as duration, %w", ProgressDeadlineKey, pd, err)
		} else if v <= 0 {
			return nil, fmt.Errorf("%s cannot be non-positive duration, was %v", ProgressDeadlineKey, v)
		}
		nc.ProgressDeadline = v
	}

	if registries, ok := configMap[registriesSkippingTagResolvingKey]; ok {
		nc.RegistriesSkippingTagResolving = sets.NewString(strings.Split(registries, ",")...)
	}
	return nc, nil
}

// NewConfigFromConfigMap creates a DeploymentConfig from the supplied configMap
func NewConfigFromConfigMap(config *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(config.Data)
}

// Config includes the configurations for the controller.
type Config struct {
	// QueueSidecarImage is the name of the image used for the queue sidecar
	// injected into the revision pod
	QueueSidecarImage string

	// Repositories for which tag to digest resolving should be skipped
	RegistriesSkippingTagResolving sets.String

	// ProgressDeadline is the time in seconds we wait for the deployment to
	// be ready before considering it failed.
	ProgressDeadline time.Duration
}
