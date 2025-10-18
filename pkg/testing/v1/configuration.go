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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// ConfigOption enables further configuration of a Configuration.
type ConfigOption func(*v1.Configuration)

// WithConfigReadinessProbe sets the provided probe to be the readiness
// probe on the configuration.
func WithConfigReadinessProbe(p *corev1.Probe) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.Containers[0].ReadinessProbe = p
	}
}

// WithConfigDeletionTimestamp sets the DeletionTimestamp on the Config.
func WithConfigDeletionTimestamp(r *v1.Configuration) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithConfigContainerConcurrency sets the given Configuration's concurrency.
func WithConfigContainerConcurrency(cc int64) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.ContainerConcurrency = &cc
	}
}

// WithConfigGeneration sets the generation of the Configuration.
func WithConfigGeneration(gen int64) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Generation = gen
	}
}

// WithConfigObservedGen sets the observed generation of the Configuration.
func WithConfigObservedGen(cfg *v1.Configuration) {
	cfg.Status.ObservedGeneration = cfg.Generation
}

// WithLatestCreated initializes the .status.latestCreatedRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestCreated(name string) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Status.SetLatestCreatedRevisionName(name)
	}
}

// WithLatestReady initializes the .status.latestReadyRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestReady(name string) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Status.SetLatestReadyRevisionName(name)
	}
}

// MarkRevisionCreationFailed calls .Status.MarkRevisionCreationFailed.
func MarkRevisionCreationFailed(msg string) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Status.MarkRevisionCreationFailed(msg)
	}
}

// MarkLatestCreatedFailed calls .Status.MarkLatestCreatedFailed.
func MarkLatestCreatedFailed(msg string) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Status.MarkLatestCreatedFailed(cfg.Status.LatestCreatedRevisionName, msg)
	}
}

// WithConfigLabel attaches a particular label to the configuration.
func WithConfigLabel(key, value string) ConfigOption {
	return func(config *v1.Configuration) {
		config.Labels = kmeta.UnionMaps(config.Labels, map[string]string{key: value})
	}
}

// WithConfigAnn attaches a particular label to the configuration.
func WithConfigAnn(key, value string) ConfigOption {
	return func(config *v1.Configuration) {
		config.Annotations = kmeta.UnionMaps(config.Annotations, map[string]string{key: value})
	}
}

// WithConfigOwnersRemoved clears the owner references of this Configuration.
func WithConfigOwnersRemoved(cfg *v1.Configuration) {
	cfg.OwnerReferences = nil
}

// WithConfigEnv configures the Service to use the provided environment variables.
func WithConfigEnv(evs ...corev1.EnvVar) ConfigOption {
	return func(c *v1.Configuration) {
		c.Spec.Template.Spec.Containers[0].Env = evs
	}
}

// WithConfigRevisionTimeoutSeconds sets revision timeout.
func WithConfigRevisionTimeoutSeconds(revisionTimeoutSeconds int64) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.TimeoutSeconds = ptr.Int64(revisionTimeoutSeconds)
	}
}

// WithConfigRevisionResponseStartTimeoutSeconds sets revision first byte timeout
func WithConfigRevisionResponseStartTimeoutSeconds(revisionResponseStartTimeoutSeconds int64) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.ResponseStartTimeoutSeconds = ptr.Int64(revisionResponseStartTimeoutSeconds)
	}
}

// WithConfigRevisionIdleTimeoutSeconds sets revision idle timeout
func WithConfigRevisionIdleTimeoutSeconds(revisionIdleTimeoutSeconds int64) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.IdleTimeoutSeconds = ptr.Int64(revisionIdleTimeoutSeconds)
	}
}

// WithVolume adds a volume to the service
func WithConfigVolume(name, mountPath string, volumeSource corev1.VolumeSource) ConfigOption {
	return func(c *v1.Configuration) {
		rt := &c.Spec.Template.Spec

		rt.Containers[0].VolumeMounts = append(rt.Containers[0].VolumeMounts,
			corev1.VolumeMount{
				Name:      name,
				MountPath: mountPath,
			})

		rt.Volumes = append(rt.Volumes, corev1.Volume{
			Name:         name,
			VolumeSource: volumeSource,
		})
	}
}
