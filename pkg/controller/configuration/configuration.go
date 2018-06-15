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
	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

// Controller implements the controller for Configuration resources
type Controller struct {
	*controller.Base

	buildClientSet buildclientset.Interface

	// listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
}

// NewController creates a new Configuration controller
func NewController(
	opt controller.Options,
	buildClientSet buildclientset.Interface,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config) controller.Interface {

	// obtain references to a shared index informer for the Configuration
	// and Revision type.
	configurationInformer := elaInformerFactory.Serving().V1alpha1().Configurations()
	revisionInformer := elaInformerFactory.Serving().V1alpha1().Revisions()

	informers := []cache.SharedIndexInformer{
		configurationInformer.Informer(),
		revisionInformer.Informer(),
	}

	c := &Controller{
		Base:                controller.NewBase(opt, controllerAgentName, "Configurations", informers),
		buildClientSet:      buildClientSet,
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
			UpdateFunc: controller.PassNew(c.Enqueue),
		},
	})
	return c
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
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

	// Get the Configuration resource with this namespace/name
	config, err := c.configurationLister.Configurations(namespace).Get(name)
	if errors.IsNotFound(err) {
		// The resource no longer exists, in which case we stop processing.
		runtime.HandleError(fmt.Errorf("configuration %q in work queue no longer exists", key))
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informer's copy.
	config = config.DeepCopy()
	config.Status.InitializeConditions()

	// First, fetch the revision that should exist for the current generation
	revName := generateRevisionName(config)
	latestCreatedRevision, err := c.revisionLister.Revisions(config.Namespace).Get(revName)
	if errors.IsNotFound(err) {
		latestCreatedRevision, err = c.createRevision(config, revName)
		if err != nil {
			logger.Errorf("Failed to create Revision %q: %v", revName, err)
			c.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v", revName, err)
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

	// Reflect any status changes that occurred during reconciliation.
	if _, err := c.updateStatus(config); err != nil {
		logger.Error("Error updating configuration", zap.Error(err))
		return err
	}

	return nil
}

func (c *Controller) createRevision(config *v1alpha1.Configuration, revName string) (*v1alpha1.Revision, error) {
	logger := loggerWithConfigInfo(c.Logger, config.Namespace, config.Name)
	spec := config.Spec.RevisionTemplate.Spec
	controllerRef := controller.NewConfigurationControllerRef(config)

	if config.Spec.Build != nil {
		// TODO(mattmoor): Determine whether we reuse the previous build.
		build := &buildv1alpha1.Build{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       config.Namespace,
				GenerateName:    fmt.Sprintf("%s-", config.Name),
				OwnerReferences: []metav1.OwnerReference{*controllerRef},
			},
			Spec: *config.Spec.Build,
		}
		created, err := c.buildClientSet.BuildV1alpha1().Builds(build.Namespace).Create(build)
		if err != nil {
			return nil, err
		}
		logger.Infof("Created Build:\n%+v", created.Name)
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Build %q", created.Name)
		spec.BuildName = created.Name
	}

	rev := &v1alpha1.Revision{
		ObjectMeta: config.Spec.RevisionTemplate.ObjectMeta,
		Spec:       spec,
	}
	rev.Namespace = config.Namespace
	rev.Name = revName
	if rev.Labels == nil {
		rev.Labels = make(map[string]string)
	}
	rev.Labels[serving.ConfigurationLabelKey] = config.Name
	if rev.Annotations == nil {
		rev.Annotations = make(map[string]string)
	}
	rev.Annotations[serving.ConfigurationGenerationAnnotationKey] = fmt.Sprintf("%v", config.Spec.Generation)

	// Delete revisions when the parent Configuration is deleted.
	rev.OwnerReferences = append(rev.OwnerReferences, *controllerRef)

	created, err := c.ElaClientSet.ServingV1alpha1().Revisions(config.Namespace).Create(rev)
	if err != nil {
		return nil, err
	}
	c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", rev.Name)
	logger.Infof("Created Revision:\n%+v", created)

	return created, nil
}

func generateRevisionName(u *v1alpha1.Configuration) string {
	return fmt.Sprintf("%s-%05d", u.Name, u.Spec.Generation)
}

func (c *Controller) updateStatus(u *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	newu, err := c.configurationLister.Configurations(u.Namespace).Get(u.Name)
	if err != nil {
		return nil, err
	}
	newu.Status = u.Status
	// TODO: for CRD there's no updatestatus, so use normal update
	return c.ElaClientSet.ServingV1alpha1().Configurations(u.Namespace).Update(newu)
	//	return configClient.UpdateStatus(newu)
}
