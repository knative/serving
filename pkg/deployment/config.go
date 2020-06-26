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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	cm "knative.dev/pkg/configmap"
)

const (
	// ConfigName is the name of config map for the deployment.
	ConfigName = "config-deployment"

	// QueueSidecarImageKey is the config map key for queue sidecar image
	QueueSidecarImageKey = "queueSidecarImage"

	// ProgressDeadlineDefault is the default value for the config's
	// ProgressDeadlineSeconds. This does not match the K8s default value of 600s.
	ProgressDeadlineDefault = 120 * time.Second

	// ProgressDeadlineKey is the key to configure deployment progress deadline.
	ProgressDeadlineKey = "progressDeadline"

	// registriesSkippingTagResolvingKey is the config map key for the set of registries
	// (e.g. ko.local) where tags should not be resolved to digests.
	registriesSkippingTagResolvingKey = "registriesSkippingTagResolving"

	// queueSidecar resource request keys.
	queueSidecarCPURequestKey              = "queueSidecarCPURequest"
	queueSidecarMemoryRequestKey           = "queueSidecarMemoryRequest"
	queueSidecarEphemeralStorageRequestKey = "queueSidecarEphemeralStorageRequest"

	// queueSidecar resource limit keys.
	queueSidecarCPULimitKey              = "queueSidecarCPULimit"
	queueSidecarMemoryLimitKey           = "queueSidecarMemoryLimit"
	queueSidecarEphemeralStorageLimitKey = "queueSidecarEphemeralStorageLimit"
)

var (
	// QueueSidecarCPURequestDefault is the default request.cpu to set for the
	// queue sidecar. It is set at 25m for backwards-compatibility since this was
	// the historic default before the field was operator-settable.
	QueueSidecarCPURequestDefault = resource.MustParse("25m")
)

func defaultConfig() *Config {
	return &Config{
		ProgressDeadline:               ProgressDeadlineDefault,
		RegistriesSkippingTagResolving: sets.NewString("ko.local", "dev.local"),
		QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
	}
}

// NewConfigFromMap creates a DeploymentConfig from the supplied Map
func NewConfigFromMap(configMap map[string]string) (*Config, error) {
	nc := defaultConfig()

	if err := cm.Parse(configMap,
		cm.AsString(QueueSidecarImageKey, &nc.QueueSidecarImage),
		cm.AsDuration(ProgressDeadlineKey, &nc.ProgressDeadline),
		cm.AsStringSet(registriesSkippingTagResolvingKey, &nc.RegistriesSkippingTagResolving),

		cm.AsQuantity(queueSidecarCPURequestKey, &nc.QueueSidecarCPURequest),
		cm.AsQuantity(queueSidecarMemoryRequestKey, &nc.QueueSidecarMemoryRequest),
		cm.AsQuantity(queueSidecarEphemeralStorageRequestKey, &nc.QueueSidecarEphemeralStorageRequest),
		cm.AsQuantity(queueSidecarCPULimitKey, &nc.QueueSidecarCPULimit),
		cm.AsQuantity(queueSidecarMemoryLimitKey, &nc.QueueSidecarMemoryLimit),
		cm.AsQuantity(queueSidecarEphemeralStorageLimitKey, &nc.QueueSidecarEphemeralStorageLimit),
	); err != nil {
		return nil, err
	}

	if nc.QueueSidecarImage == "" {
		return nil, errors.New("queueSidecarImage cannot be empty or unset")
	}

	if nc.ProgressDeadline <= 0 {
		return nil, fmt.Errorf("progressDeadline cannot be a non-positive duration, was %v", nc.ProgressDeadline)
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

	// QueueSidecarCPURequest is the CPU Request to set for the queue proxy sidecar container
	QueueSidecarCPURequest *resource.Quantity

	// QueueSidecarCPULimit is the CPU Limit to set for the queue proxy sidecar container
	QueueSidecarCPULimit *resource.Quantity

	// QueueSidecarMemoryRequest is the Memory Request to set for the queue proxy sidecar container
	QueueSidecarMemoryRequest *resource.Quantity

	// QueueSidecarMemoryLimit is the Memory Limit to set for the queue proxy sidecar container
	QueueSidecarMemoryLimit *resource.Quantity

	// QueueSidecarEphemeralStorageRequest is the Ephemeral Storage Request to
	// set for the queue proxy sidecar container
	QueueSidecarEphemeralStorageRequest *resource.Quantity

	// QueueSidecarEphemeralStorageLimit is the Ephemeral Storage Limit to set
	// for the queue proxy sidecar container
	QueueSidecarEphemeralStorageLimit *resource.Quantity
}
