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
	"fmt"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "configuration-controller"

// Controller implements the controller for Configuration resources
type Controller struct {
	*controller.Base

	buildClientSet buildclientset.Interface

	// lister indexes properties about Configuration
	lister listers.ConfigurationLister
	synced cache.InformerSynced

	// don't start the workers until revisions cache have been synced
	revisionsSynced cache.InformerSynced
}

// NewController creates a new Configuration controller
func NewController(
	kubeClientSet kubernetes.Interface,
	elaClientSet clientset.Interface,
	buildClientSet buildclientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config,
	logger *zap.SugaredLogger) controller.Interface {

	// obtain references to a shared index informer for the Configuration
	// and Revision type.
	informer := elaInformerFactory.Serving().V1alpha1().Configurations()
	revisionInformer := elaInformerFactory.Serving().V1alpha1().Revisions()

	controller := &Controller{
		Base: controller.NewBase(kubeClientSet, elaClientSet, kubeInformerFactory,
			elaInformerFactory, informer.Informer(), controllerAgentName, "Configurations", logger),
		buildClientSet:  buildClientSet,
		lister:          informer.Lister(),
		synced:          informer.Informer().HasSynced,
		revisionsSynced: revisionInformer.Informer().HasSynced,
	}

	controller.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.addRevisionEvent,
		UpdateFunc: controller.updateRevisionEvent,
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh,
		[]cache.InformerSynced{c.synced, c.revisionsSynced}, c.syncHandler, "Configuration")
}

// loggerWithConfigInfo enriches the logs with configuration name and namespace.
func loggerWithConfigInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Configuration, name))
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Configuration
// resource with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	logger := loggerWithConfigInfo(c.Logger, namespace, name)

	// Get the Configuration resource with this namespace/name
	config, err := c.lister.Configurations(namespace).Get(name)
	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("configuration %q in work queue no longer exists", key))
			return nil
		}

		return err
	}
	// Don't modify the informer's copy.
	config = config.DeepCopy()

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

func generateRevisionName(u *v1alpha1.Configuration) (string, error) {
	// TODO: consider making sure the length of the
	// string will not cause problems down the stack
	if u.Spec.Generation == 0 {
		return "", fmt.Errorf("configuration generation cannot be 0")
	}
	return fmt.Sprintf("%s-%05d", u.Name, u.Spec.Generation), nil
}

func (c *Controller) updateStatus(u *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	configClient := c.ElaClientSet.ServingV1alpha1().Configurations(u.Namespace)
	newu, err := configClient.Get(u.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newu.Status = u.Status

	// TODO: for CRD there's no updatestatus, so use normal update
	return configClient.Update(newu)
	//	return configClient.UpdateStatus(newu)
}

func (c *Controller) addRevisionEvent(obj interface{}) {
	revision := obj.(*v1alpha1.Revision)
	revisionName := revision.Name
	namespace := revision.Namespace
	// Lookup and see if this Revision corresponds to a Configuration that
	// we own and hence the Configuration that created this Revision.
	configName := controller.LookupOwningConfigurationName(revision.OwnerReferences)
	if configName == "" {
		return
	}

	logger := loggerWithConfigInfo(c.Logger, namespace, configName).With(zap.String(logkey.Revision, revisionName))
	ctx := logging.WithLogger(context.TODO(), logger)

	config, err := c.lister.Configurations(namespace).Get(configName)
	if err != nil {
		logger.Error("Error fetching configuration upon revision becoming ready", zap.Error(err))
		return
	}

	if revision.Name != config.Status.LatestCreatedRevisionName {
		// The revision isn't the latest created one, so ignore this event.
		logger.Info("Revision is not the latest created one")
		return
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
	return
}

func (c *Controller) updateRevisionEvent(old, new interface{}) {
	c.addRevisionEvent(new)
}

func getLatestRevisionStatusCondition(revision *v1alpha1.Revision) *v1alpha1.RevisionCondition {
	for _, cond := range revision.Status.Conditions {
		if !(cond.Type == v1alpha1.RevisionConditionReady && cond.Status == corev1.ConditionTrue) {
			return &cond
		}
	}
	return nil
}

// Mark ConfigurationConditionReady of Configuration ready as the given latest
// created revision is ready.
func (c *Controller) markConfigurationReady(
	ctx context.Context,
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	logger := logging.FromContext(ctx)
	logger.Info("Marking Configuration ready")
	config.Status.RemoveCondition(v1alpha1.ConfigurationConditionLatestRevisionReady)
	config.Status.SetCondition(
		&v1alpha1.ConfigurationCondition{
			Type:   v1alpha1.ConfigurationConditionReady,
			Status: corev1.ConditionTrue,
			Reason: "LatestRevisionReady",
		})
}

// Mark ConfigurationConditionLatestRevisionReady of Configuration to false with the status
// from the revision
func (c *Controller) markConfigurationLatestRevisionStatus(
	ctx context.Context,
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	logger := logging.FromContext(ctx)
	config.Status.RemoveCondition(v1alpha1.ConfigurationConditionReady)
	cond := getLatestRevisionStatusCondition(revision)
	if cond == nil {
		logger.Info("Revision status is not updated yet")
		return
	}
	config.Status.SetCondition(
		&v1alpha1.ConfigurationCondition{
			Type:    v1alpha1.ConfigurationConditionLatestRevisionReady,
			Status:  corev1.ConditionFalse,
			Reason:  cond.Reason,
			Message: cond.Message,
		})
}
