/*
Copyright 2018 Google LLC

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

package route

import (
	"context"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"
)

// SyncConfiguration implements configuration.Receiver
func (c *Receiver) SyncConfiguration(config *v1alpha1.Configuration) error {
	configName := config.Name
	ns := config.Namespace

	if config.Status.LatestReadyRevisionName == "" {
		c.Logger.Infof("Configuration %s is not ready", configName)
		return nil
	}

	routeName, ok := config.Labels[serving.RouteLabelKey]
	if !ok {
		c.Logger.Infof("Configuration %s does not have label %s", configName, serving.RouteLabelKey)
		return nil
	}

	logger := loggerWithRouteInfo(c.Logger, ns, routeName)
	ctx := logging.WithLogger(context.TODO(), logger)
	route, err := c.lister.Routes(ns).Get(routeName)
	if err != nil {
		logger.Error("Error fetching route upon configuration becoming ready", zap.Error(err))
		return err
	}

	// Don't modify the informers copy
	route = route.DeepCopy()
	if _, err := c.syncTrafficTargetsAndUpdateRouteStatus(ctx, route); err != nil {
		logger.Error("Error updating route upon configuration becoming ready", zap.Error(err))
		return err
	}
	return nil
}
