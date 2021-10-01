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

	// QueueSidecarImageKey is the config map key for queue sidecar image.
	QueueSidecarImageKey = "queue-sidecar-image"

	// DeprecatedQueueSidecarImageKey is the config map key for queue sidecar image.
	DeprecatedQueueSidecarImageKey = "queueSidecarImage"

	// ProgressDeadlineDefault is the default value for the config's
	// ProgressDeadlineSeconds. This matches the K8s default value of 600s.
	ProgressDeadlineDefault = 600 * time.Second

	// ProgressDeadlineKey is the key to configure deployment progress deadline.
	ProgressDeadlineKey = "progress-deadline"

	// digestResolutionTimeoutKey is the key to configure the digest resolution timeout.
	digestResolutionTimeoutKey = "digest-resolution-timeout"

	// digestResolutionTimeoutDefault is the default digest resolution timeout.
	digestResolutionTimeoutDefault = 10 * time.Second

	// registriesSkippingTagResolvingKey is the config map key for the set of registries
	// (e.g. ko.local) where tags should not be resolved to digests.
	registriesSkippingTagResolvingKey = "registries-skipping-tag-resolving"

	// queueSidecar resource request keys.
	queueSidecarCPURequestKey              = "queue-sidecar-cpu-request"
	queueSidecarMemoryRequestKey           = "queue-sidecar-memory-request"
	queueSidecarEphemeralStorageRequestKey = "queue-sidecar-ephemeral-storage-request"

	// queueSidecar resource limit keys.
	queueSidecarCPULimitKey              = "queue-sidecar-cpu-limit"
	queueSidecarMemoryLimitKey           = "queue-sidecar-memory-limit"
	queueSidecarEphemeralStorageLimitKey = "queue-sidecar-ephemeral-storage-limit"

	// concurrencyStateEndpointKey is the key to configure the endpoint Queue Proxy will call when traffic drops to / increases from zero.
	concurrencyStateEndpointKey = "concurrency-state-endpoint"
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
		DigestResolutionTimeout:        digestResolutionTimeoutDefault,
		RegistriesSkippingTagResolving: sets.NewString("kind.local", "ko.local", "dev.local"),
		QueueSidecarCPURequest:         &QueueSidecarCPURequestDefault,
		ConcurrencyStateEndpoint:       "",
	}
}

// NewConfigFromMap creates a DeploymentConfig from the supplied Map.
func NewConfigFromMap(configMap map[string]string) (*Config, error) {
	nc := defaultConfig()

	if err := cm.Parse(configMap,
		// Legacy keys for backwards compatibility
		cm.AsString(DeprecatedQueueSidecarImageKey, &nc.QueueSidecarImage),
		cm.AsDuration("progressDeadline", &nc.ProgressDeadline),
		cm.AsDuration("digestResolutionTimeout", &nc.DigestResolutionTimeout),
		cm.AsStringSet("registriesSkippingTagResolving", &nc.RegistriesSkippingTagResolving),
		cm.AsQuantity("queueSidecarCPURequest", &nc.QueueSidecarCPURequest),
		cm.AsQuantity("queueSidecarMemoryRequest", &nc.QueueSidecarMemoryRequest),
		cm.AsQuantity("queueSidecarEphemeralStorageRequest", &nc.QueueSidecarEphemeralStorageRequest),
		cm.AsQuantity("queueSidecarCPULimit", &nc.QueueSidecarCPULimit),
		cm.AsQuantity("queueSidecarMemoryLimit", &nc.QueueSidecarMemoryLimit),
		cm.AsQuantity("queueSidecarEphemeralStorageLimit", &nc.QueueSidecarEphemeralStorageLimit),
		cm.AsString("concurrencyStateEndpoint", &nc.ConcurrencyStateEndpoint),

		cm.AsString(QueueSidecarImageKey, &nc.QueueSidecarImage),
		cm.AsDuration(ProgressDeadlineKey, &nc.ProgressDeadline),
		cm.AsDuration(digestResolutionTimeoutKey, &nc.DigestResolutionTimeout),
		cm.AsStringSet(registriesSkippingTagResolvingKey, &nc.RegistriesSkippingTagResolving),

		cm.AsQuantity(queueSidecarCPURequestKey, &nc.QueueSidecarCPURequest),
		cm.AsQuantity(queueSidecarMemoryRequestKey, &nc.QueueSidecarMemoryRequest),
		cm.AsQuantity(queueSidecarEphemeralStorageRequestKey, &nc.QueueSidecarEphemeralStorageRequest),
		cm.AsQuantity(queueSidecarCPULimitKey, &nc.QueueSidecarCPULimit),
		cm.AsQuantity(queueSidecarMemoryLimitKey, &nc.QueueSidecarMemoryLimit),
		cm.AsQuantity(queueSidecarEphemeralStorageLimitKey, &nc.QueueSidecarEphemeralStorageLimit),

		cm.AsString(concurrencyStateEndpointKey, &nc.ConcurrencyStateEndpoint),
	); err != nil {
		return nil, err
	}

	if nc.QueueSidecarImage == "" {
		return nil, errors.New("queue-sidecar-image cannot be empty or unset")
	}

	if nc.ProgressDeadline <= 0 {
		return nil, fmt.Errorf("progress-deadline cannot be a non-positive duration, was %v", nc.ProgressDeadline)
	}

	if nc.ProgressDeadline.Truncate(time.Second) != nc.ProgressDeadline {
		return nil, fmt.Errorf("progress-deadline must be rounded to a whole second, was: %v", nc.ProgressDeadline)
	}

	if nc.DigestResolutionTimeout <= 0 {
		return nil, fmt.Errorf("digest-resolution-timeout cannot be a non-positive duration, was %v", nc.DigestResolutionTimeout)
	}

	return nc, nil
}

// NewConfigFromConfigMap creates a DeploymentConfig from the supplied configMap.
func NewConfigFromConfigMap(config *corev1.ConfigMap) (*Config, error) {
	return NewConfigFromMap(config.Data)
}

// Config includes the configurations for the controller.
type Config struct {
	// QueueSidecarImage is the name of the image used for the queue sidecar
	// injected into the revision pod.
	QueueSidecarImage string

	// Repositories for which tag to digest resolving should be skipped.
	RegistriesSkippingTagResolving sets.String

	// DigestResolutionTimeout is the maximum time allowed for image digest resolution.
	DigestResolutionTimeout time.Duration

	// ProgressDeadline is the time in seconds we wait for the deployment to
	// be ready before considering it failed.
	ProgressDeadline time.Duration

	// QueueSidecarCPURequest is the CPU Request to set for the queue proxy sidecar container.
	QueueSidecarCPURequest *resource.Quantity

	// QueueSidecarCPULimit is the CPU Limit to set for the queue proxy sidecar container.
	QueueSidecarCPULimit *resource.Quantity

	// QueueSidecarMemoryRequest is the Memory Request to set for the queue proxy sidecar container.
	QueueSidecarMemoryRequest *resource.Quantity

	// QueueSidecarMemoryLimit is the Memory Limit to set for the queue proxy sidecar container.
	QueueSidecarMemoryLimit *resource.Quantity

	// QueueSidecarEphemeralStorageRequest is the Ephemeral Storage Request to
	// set for the queue proxy sidecar container.
	QueueSidecarEphemeralStorageRequest *resource.Quantity

	// QueueSidecarEphemeralStorageLimit is the Ephemeral Storage Limit to set
	// for the queue proxy sidecar container.
	QueueSidecarEphemeralStorageLimit *resource.Quantity

	// ConcurrencyStateEndpoint is the endpoint Queue Proxy will call when traffic drops to / increases from zero.
	ConcurrencyStateEndpoint string
}
