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

	buildv1alpha1 "github.com/elafros/build/pkg/apis/build/v1alpha1"
	buildclientset "github.com/elafros/build/pkg/client/clientset/versioned"
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
	"github.com/golang/glog"
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
	base *controller.ControllerBase

	buildClientSet buildclientset.Interface

	// lister indexes properties about Configuration
	lister listers.ConfigurationLister
	synced cache.InformerSynced

	// don't start the workers until revisions cache have been synced
	revisionsSynced cache.InformerSynced
}

// NewController creates a new Configuration controller
func NewController(
	base *controller.ControllerBase,
	buildClientSet buildclientset.Interface,
	config *rest.Config,
	controllerConfig controller.Config) controller.Interface {

	// obtain references to a shared index informer for the Configuration
	// and Revision type.
	informer := base.ElaInformerFactory.Elafros().V1alpha1().Configurations()
	revisionInformer := base.ElaInformerFactory.Elafros().V1alpha1().Revisions()

	base.Init(controllerAgentName, "Configurations", informer.Informer())
	controller := &Controller{
		base:            base,
		buildClientSet:  buildClientSet,
		lister:          informer.Lister(),
		synced:          informer.Informer().HasSynced,
		revisionsSynced: revisionInformer.Informer().HasSynced,
	}

	glog.Info("Setting up event handlers")
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
	return c.base.Run(threadiness, stopCh, []cache.InformerSynced{c.synced, c.revisionsSynced},
		c.syncHandler, "Configuration")
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
		glog.Infof("Skipping reconcile since already reconciled %d == %d",
			config.Spec.Generation, config.Status.ObservedGeneration)
		return nil
	}

	glog.Infof("Running reconcile Configuration for %s\n%+v\n%+v\n",
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
			glog.Errorf("Failed to create Build:\n%+v\n%s", build, err)
			c.base.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Build %q: %v", build.Name, err)
			return err
		}
		glog.Infof("Created Build:\n%+v", created.Name)
		c.base.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Build %q", created.Name)
		spec.BuildName = created.Name
	}

	revName, err := generateRevisionName(config)
	if err != nil {
		return err
	}

	revClient := c.base.ElaClientSet.ElafrosV1alpha1().Revisions(config.Namespace)
	created, err := revClient.Get(revName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			glog.Errorf("Revisions Get for %q failed: %s", revName, err)
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
		rev.ObjectMeta.Labels[ela.ConfigurationLabelKey] = config.Name

		if rev.ObjectMeta.Annotations == nil {
			rev.ObjectMeta.Annotations = make(map[string]string)
		}
		rev.ObjectMeta.Annotations[ela.ConfigurationGenerationAnnotationKey] = fmt.Sprintf("%v", config.Spec.Generation)

		// Delete revisions when the parent Configuration is deleted.
		rev.OwnerReferences = append(rev.OwnerReferences, *controllerRef)

		created, err = revClient.Create(rev)
		if err != nil {
			glog.Errorf("Failed to create Revision:\n%+v\n%s", rev, err)
			c.base.Recorder.Eventf(config, corev1.EventTypeWarning, "CreationFailed", "Failed to create Revision %q: %v", rev.Name, err)
			return err
		}
		c.base.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", rev.Name)
		glog.Infof("Created Revision:\n%+v", created)
	} else {
		glog.Infof("Revision already created %s: %s", created.ObjectMeta.Name, err)
	}
	// Update the Status of the configuration with the latest generation that
	// we just reconciled against so we don't keep generating revisions.
	// Also update the LatestCreatedRevisionName so that we'll know revision to check
	// for ready state so that when ready, we can make it Latest.
	config.Status.LatestCreatedRevisionName = created.ObjectMeta.Name
	config.Status.ObservedGeneration = config.Spec.Generation

	glog.Infof("Updating the configuration status:\n%+v", config)

	if _, err = c.updateStatus(config); err != nil {
		glog.Errorf("Failed to update the configuration %s", err)
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
	configClient := c.base.ElaClientSet.ElafrosV1alpha1().Configurations(u.Namespace)
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

	config, err := c.lister.Configurations(namespace).Get(configName)
	if err != nil {
		glog.Errorf("Error fetching configuration '%s/%s' upon revision becoming ready: %v",
			namespace, configName, err)
		return
	}

	if revision.Name != config.Status.LatestCreatedRevisionName {
		// The revision isn't the latest created one, so ignore this event.
		glog.Infof("Revision %q is not the latest created one", revisionName)
		return
	}

	// Don't modify the informer's copy.
	config = config.DeepCopy()

	if !revision.Status.IsReady() {
		glog.Infof("Revision %q of configuration %q is not ready", revisionName, config.Name)

		//add LatestRevision condition to be false with the status from the revision
		c.markConfigurationLatestRevisionStatus(config, revision)

		if _, err := c.updateStatus(config); err != nil {
			glog.Errorf("Error updating configuration '%s/%s': %v",
				namespace, configName, err)
		}
		c.base.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestRevisionUpdate",
			"Latest revision of configuration is not ready")

	} else {
		glog.Infof("Revision %q is ready", revisionName)

		alreadyReady := config.Status.IsReady()
		if !alreadyReady {
			c.markConfigurationReady(config, revision)
		}
		glog.Infof("Setting LatestReadyRevisionName of Configuration %q to revision %q",
			config.Name, revision.Name)
		config.Status.LatestReadyRevisionName = revision.Name

		if _, err := c.updateStatus(config); err != nil {
			glog.Errorf("Error updating configuration '%s/%s': %v",
				namespace, configName, err)
		}
		if !alreadyReady {
			c.base.Recorder.Eventf(config, corev1.EventTypeNormal, "ConfigurationReady",
				"Configuration becomes ready")
		}
		c.base.Recorder.Eventf(config, corev1.EventTypeNormal, "LatestReadyUpdate",
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
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	glog.Infof("Marking Configuration %q ready", config.Name)
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
	config *v1alpha1.Configuration, revision *v1alpha1.Revision) {
	config.Status.RemoveCondition(v1alpha1.ConfigurationConditionReady)
	cond := getLatestRevisionStatusCondition(revision)
	if cond == nil {
		glog.Infof("Revision status is not updated yet")
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
