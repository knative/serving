/*
Copyright 2018 Google LLC.

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

package configuration

import (
	"fmt"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SyncConfiguration implements configuration.Receiver
func (c *Receiver) SyncConfiguration(config *v1alpha1.Configuration) error {
	logger := loggerWithConfigInfo(c.Logger, config.Namespace, config.Name)

	// Configuration business logic
	if config.GetGeneration() == config.Status.ObservedGeneration {
		// TODO(vaikas): Check to see if Status.LatestCreatedRevisionName is ready and update Status.LatestReady
		logger.Infof("Skipping reconcile since already reconciled %d == %d",
			config.Spec.Generation, config.Status.ObservedGeneration)
		return nil
	}

	logger.Infof("Running reconcile Configuration for %s\n%+v\n%+v\n",
		config.Name, config, config.Spec.RevisionTemplate)
	spec := config.Spec.RevisionTemplate.Spec
	controllerRef := controller.NewConfigurationControllerRef(config)

	if config.Spec.Build != nil {
		// TODO(mattmoor): Determine whether we reuse the previous build.
		build := &buildv1alpha1.Build{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    config.Namespace,
				GenerateName: fmt.Sprintf("%s-", config.Name),
			},
			Spec: *config.Spec.Build,
		}
		build.OwnerReferences = append(build.OwnerReferences, *controllerRef)
		created, err := c.buildClientSet.BuildV1alpha1().Builds(build.Namespace).Create(build)
		if err != nil {
			logger.Errorf("Failed to create Build:\n%+v\n%s", build, err)
			c.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Build %q: %v", build.Name, err)
			return err
		}
		logger.Infof("Created Build:\n%+v", created.Name)
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Build %q", created.Name)
		spec.BuildName = created.Name
	}

	revName, err := generateRevisionName(config)
	if err != nil {
		return err
	}

	revClient := c.ElaClientSet.ServingV1alpha1().Revisions(config.Namespace)
	created, err := revClient.Get(revName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			logger.Error("Revisions Get failed", zap.Error(err), zap.String(logkey.Revision, revName))
			return err
		}

		rev := &v1alpha1.Revision{
			ObjectMeta: config.Spec.RevisionTemplate.ObjectMeta,
			Spec:       spec,
		}
		// TODO: Should this just use rev.ObjectMeta.GenerateName =
		rev.ObjectMeta.Name = revName
		// Can't generate objects in a different namespace from what the call is made against,
		// so use the namespace of the configuration that's being updated for the Revision being
		// created.
		rev.ObjectMeta.Namespace = config.Namespace

		if rev.ObjectMeta.Labels == nil {
			rev.ObjectMeta.Labels = make(map[string]string)
		}
		rev.ObjectMeta.Labels[serving.ConfigurationLabelKey] = config.Name

		if rev.ObjectMeta.Annotations == nil {
			rev.ObjectMeta.Annotations = make(map[string]string)
		}
		rev.ObjectMeta.Annotations[serving.ConfigurationGenerationAnnotationKey] = fmt.Sprintf("%v", config.Spec.Generation)

		// Delete revisions when the parent Configuration is deleted.
		rev.OwnerReferences = append(rev.OwnerReferences, *controllerRef)

		created, err = revClient.Create(rev)
		if err != nil {
			logger.Errorf("Failed to create Revision:\n%+v\n%s", rev, err)
			c.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v", rev.Name, err)
			return err
		}
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", rev.Name)
		logger.Infof("Created Revision:\n%+v", created)
	} else {
		logger.Infof("Revision already created %s: %s", created.ObjectMeta.Name, err)
	}
	// Update the Status of the configuration with the latest generation that
	// we just reconciled against so we don't keep generating revisions.
	// Also update the LatestCreatedRevisionName so that we'll know revision to check
	// for ready state so that when ready, we can make it Latest.
	config.Status.LatestCreatedRevisionName = created.ObjectMeta.Name
	config.Status.ObservedGeneration = config.Spec.Generation

	logger.Infof("Updating the configuration status:\n%+v", config)

	if _, err = c.updateStatus(config); err != nil {
		logger.Error("Failed to update the configuration", zap.Error(err))
		return err
	}

	return nil
}
