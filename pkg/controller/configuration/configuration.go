/*
Copyright 2018 The Knative Authors.

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
	"fmt"
	"reflect"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/controller/configuration/resources"
	resourcenames "github.com/knative/serving/pkg/controller/configuration/resources/names"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

// Controller implements the controller for Configuration resources
type Controller struct {
	*controller.Base

	// listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
}

// NewController creates a new Configuration controller
func NewController(
	opt controller.Options,
	configurationInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
) *Controller {

	c := &Controller{
		Base:                controller.NewBase(opt, controllerAgentName, "Configurations"),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
	}

	c.Logger.Info("Setting up event handlers")
	configurationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: controller.PassNew(c.Enqueue),
		DeleteFunc: c.Enqueue,
	})

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter("Configuration"),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(c.EnqueueControllerOf),
		},
	})
	return c
}

// Run starts the controller's worker threads, the number of which is threadiness. It then blocks until stopCh
// is closed, at which point it shuts down its internal work queue and waits for workers to finish processing their
// current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.Reconcile, "Configuration")
}

// loggerWithConfigInfo enriches the logs with configuration name and namespace.
func loggerWithConfigInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Configuration, name))
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Configuration
// resource with the current status of the resource.
func (c *Controller) Reconcile(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	// Wrap our logger with the additional context of the configuration that we are reconciling.
	logger := loggerWithConfigInfo(c.Logger, namespace, name)
	ctx := logging.WithLogger(context.TODO(), logger)

	// Get the Configuration resource with this namespace/name
	original, err := c.configurationLister.Configurations(namespace).Get(name)
	if errors.IsNotFound(err) {
		// The resource no longer exists, in which case we stop processing.
		runtime.HandleError(fmt.Errorf("configuration %q in work queue no longer exists", key))
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informer's copy.
	config := original.DeepCopy()

	// Reconcile this copy of the configuration and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, config)
	if equality.Semantic.DeepEqual(original.Status, config.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(config); err != nil {
		logger.Warn("Failed to update configuration status", zap.Error(err))
		return err
	}
	return err
}

func (c *Controller) reconcile(ctx context.Context, config *v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)
	config.Status.InitializeConditions()

	// First, fetch the revision that should exist for the current generation
	revName := resourcenames.Revision(config)
	latestCreatedRevision, err := c.revisionLister.Revisions(config.Namespace).Get(revName)
	if errors.IsNotFound(err) {
		latestCreatedRevision, err = c.createRevision(config, revName)
		if err != nil {
			logger.Errorf("Failed to create Revision %q: %v", revName, err)
			c.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v", revName, err)

			// Mark the Configuration as not-Ready since creating
			// its latest revision failed.
			config.Status.MarkRevisionCreationFailed(err.Error())

			return err
		}
	} else if err != nil {
		logger.Errorf("Failed to reconcile Configuration: %q failed to Get Revision: %q", config.Name, revName)
		return err
	}

	// Second, set this to be the latest revision that we have created.
	config.Status.SetLatestCreatedRevisionName(revName)
	config.Status.ObservedGeneration = config.Spec.Generation

	// Last, determine whether we should set LatestReadyRevisionName to our
	// LatestCreatedRevision based on its readiness.
	rc := latestCreatedRevision.Status.GetCondition(v1alpha1.RevisionConditionReady)
	switch {
	case rc == nil || rc.Status == corev1.ConditionUnknown:
		logger.Infof("Revision %q of configuration %q is not ready", revName, config.Name)

	case rc.Status == corev1.ConditionTrue:
		logger.Infof("Revision %q of configuration %q is ready", revName, config.Name)

		created, ready := config.Status.LatestCreatedRevisionName, config.Status.LatestReadyRevisionName
		if ready == "" {
			// Surface an event for the first revision becoming ready.
			c.Recorder.Eventf(config, corev1.EventTypeNormal, "ConfigurationReady",
				"Configuration becomes ready")
		}
		if created != ready {
			// Update the LatestReadyRevisionName and surface an event for the transition.
			config.Status.SetLatestReadyRevisionName(latestCreatedRevision.Name)
			c.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestReadyUpdate",
				"LatestReadyRevisionName updated to %q", latestCreatedRevision.Name)
		}

	case rc.Status == corev1.ConditionFalse:
		logger.Infof("Revision %q of configuration %q has failed", revName, config.Name)

		// TODO(mattmoor): Only emit the event the first time we see this.
		config.Status.MarkLatestCreatedFailed(latestCreatedRevision.Name, rc.Message)
		c.Recorder.Eventf(config, corev1.EventTypeWarning, "LatestCreatedFailed",
			"Latest created revision %q has failed", latestCreatedRevision.Name)

	default:
		err := fmt.Errorf("unrecognized condition status: %v on revision %q", rc.Status, revName)
		logger.Errorf("Error reconciling Configuration %q: %v", config.Name, err)
		return err
	}

	return nil
}

func (c *Controller) createRevision(config *v1alpha1.Configuration, revName string) (*v1alpha1.Revision, error) {
	logger := loggerWithConfigInfo(c.Logger, config.Namespace, config.Name)

	buildName := ""
	if config.Spec.Build != nil {
		// TODO(mattmoor): Determine whether we reuse the previous build.
		build := resources.MakeBuild(config)
		created, err := c.BuildClientSet.BuildV1alpha1().Builds(build.Namespace).Create(build)
		if err != nil {
			return nil, err
		}
		logger.Infof("Created Build:\n%+v", created.Name)
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Build %q", created.Name)
		buildName = created.Name
	}

	rev := resources.MakeRevision(config, buildName)
	created, err := c.ServingClientSet.ServingV1alpha1().Revisions(config.Namespace).Create(rev)
	if err != nil {
		return nil, err
	}
	c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", rev.Name)
	logger.Infof("Created Revision:\n%+v", created)

	return created, nil
}

func (c *Controller) updateStatus(u *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	newu, err := c.configurationLister.Configurations(u.Namespace).Get(u.Name)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(newu.Status, u.Status) {
		newu.Status = u.Status
		// TODO: for CRD there's no updatestatus, so use normal update
		return c.ServingClientSet.ServingV1alpha1().Configurations(u.Namespace).Update(newu)
		//	return configClient.UpdateStatus(newu)
	}
	return newu, nil
}
