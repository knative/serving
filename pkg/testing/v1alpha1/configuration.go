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

package v1alpha1

import (
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigOption enables further configuration of a Configuration.
type ConfigOption func(*v1alpha1.Configuration)

// WithConfigDeletionTimestamp will set the DeletionTimestamp on the Config.
func WithConfigDeletionTimestamp(r *v1alpha1.Configuration) {
	t := metav1.NewTime(time.Unix(1e9, 0))
	r.ObjectMeta.SetDeletionTimestamp(&t)
}

// WithConfigOwnersRemoved clears the owner references of this Configuration.
func WithConfigOwnersRemoved(cfg *v1alpha1.Configuration) {
	cfg.OwnerReferences = nil
}

// WithConfigContainerConcurrency sets the given Configuration's concurrency.
func WithConfigContainerConcurrency(cc v1beta1.RevisionContainerConcurrencyType) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Spec.GetTemplate().Spec.ContainerConcurrency = cc
	}
}

// WithGeneration sets the generation of the Configuration.
func WithGeneration(gen int64) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Generation = gen
	}
}

// WithObservedGen sets the observed generation of the Configuration.
func WithObservedGen(cfg *v1alpha1.Configuration) {
	cfg.Status.ObservedGeneration = cfg.Generation
}

// WithCreatedAndReady sets the latest{Created,Ready}RevisionName on the Configuration.
func WithCreatedAndReady(created, ready string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.SetLatestCreatedRevisionName(created)
		cfg.Status.SetLatestReadyRevisionName(ready)
	}
}

// WithLatestCreated initializes the .status.latestCreatedRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestCreated(name string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.SetLatestCreatedRevisionName(name)
	}
}

// WithLatestReady initializes the .status.latestReadyRevisionName to be the name
// of the latest revision that the Configuration would have created.
func WithLatestReady(name string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.SetLatestReadyRevisionName(name)
	}
}

// MarkRevisionCreationFailed calls .Status.MarkRevisionCreationFailed.
func MarkRevisionCreationFailed(msg string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.MarkRevisionCreationFailed(msg)
	}
}

// MarkLatestCreatedFailed calls .Status.MarkLatestCreatedFailed.
func MarkLatestCreatedFailed(msg string) ConfigOption {
	return func(cfg *v1alpha1.Configuration) {
		cfg.Status.MarkLatestCreatedFailed(cfg.Status.LatestCreatedRevisionName, msg)
	}
}

// WithConfigLabel attaches a particular label to the configuration.
func WithConfigLabel(key, value string) ConfigOption {
	return func(config *v1alpha1.Configuration) {
		if config.Labels == nil {
			config.Labels = make(map[string]string)
		}
		config.Labels[key] = value
	}
}
