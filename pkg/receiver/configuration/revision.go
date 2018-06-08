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
	"context"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

// SyncRevision implements revsion.Receiver
func (c *Receiver) SyncRevision(revision *v1alpha1.Revision) error {
	revisionName := revision.Name
	namespace := revision.Namespace
	// Lookup and see if this Revision corresponds to a Configuration that
	// we own and hence the Configuration that created this Revision.
	configName := controller.LookupOwningConfigurationName(revision.OwnerReferences)
	if configName == "" {
		return nil
	}

	logger := loggerWithConfigInfo(c.Logger, namespace, configName).With(zap.String(logkey.Revision, revisionName))
	ctx := logging.WithLogger(context.TODO(), logger)

	config, err := c.lister.Configurations(namespace).Get(configName)
	if err != nil {
		logger.Error("Error fetching configuration upon revision becoming ready", zap.Error(err))
		return err
	}

	if revision.Name != config.Status.LatestCreatedRevisionName {
		// The revision isn't the latest created one, so ignore this event.
		logger.Info("Revision is not the latest created one")
		return nil
	}

	// Don't modify the informer's copy.
	config = config.DeepCopy()

	if !revision.Status.IsReady() {
		logger.Infof("Revision %q of configuration %q is not ready", revisionName, config.Name)

		//add LatestRevision condition to be false with the status from the revision
		c.markConfigurationLatestRevisionStatus(ctx, config, revision)

		if _, err := c.updateStatus(config); err != nil {
			logger.Error("Error updating configuration", zap.Error(err))
		}
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestRevisionUpdate",
			"Latest revision of configuration is not ready")
	} else {
		logger.Info("Revision is ready")

		alreadyReady := config.Status.IsReady()
		if !alreadyReady {
			c.markConfigurationReady(ctx, config, revision)
		}
		logger.Infof("Setting LatestReadyRevisionName of Configuration %q to revision %q",
			config.Name, revision.Name)
		config.Status.LatestReadyRevisionName = revision.Name

		if _, err := c.updateStatus(config); err != nil {
			logger.Error("Error updating configuration", zap.Error(err))
		}
		if !alreadyReady {
			c.Recorder.Eventf(config, corev1.EventTypeNormal, "ConfigurationReady",
				"Configuration becomes ready")
		}
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestReadyUpdate",
			"LatestReadyRevisionName updated to %q", revision.Name)
	}
	return nil
}
