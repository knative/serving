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
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	configns "github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources/names"
)

const controllerAgentName = "configuration-controller"

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// Reconciler implements controller.Reconciler for Configuration resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister

	configStore configStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController creates a new Configuration controller
func NewController(
	opt reconciler.Options,
	configurationInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		configurationLister: configurationInformer.Lister(),
		revisionLister:      revisionInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "Configurations", reconciler.MustNewStatsReporter("Configurations", c.Logger))

	c.Logger.Info("Setting up event handlers")
	configurationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	revisionInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Configuration")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
			DeleteFunc: impl.EnqueueControllerOf,
		},
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	c.configStore = configns.NewStore(c.Logger.Named("config-store"))
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Configuration
// resource with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	ctx = c.configStore.ToContext(ctx)

	// Get the Configuration resource with this namespace/name
	original, err := c.configurationLister.Configurations(namespace).Get(name)
	if errors.IsNotFound(err) {
		// The resource no longer exists, in which case we stop processing.
		logger.Errorf("configuration %q in work queue no longer exists", key)
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

func (c *Reconciler) reconcile(ctx context.Context, config *v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)
	config.Status.InitializeConditions()

	// First, fetch the revision that should exist for the current generation
	revName := resourcenames.Revision(config)
	latestCreatedRevision, err := c.revisionLister.Revisions(config.Namespace).Get(revName)
	if errors.IsNotFound(err) {
		latestCreatedRevision, err = c.createRevision(ctx, config, revName)
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
		// Update the LatestReadyRevisionName and surface an event for the transition.
		config.Status.SetLatestReadyRevisionName(latestCreatedRevision.Name)
		if created != ready {
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

	if err := c.gcRevisions(ctx, config); err != nil {
		return err
	}

	return nil
}

func (c *Reconciler) createRevision(ctx context.Context, config *v1alpha1.Configuration, revName string) (*v1alpha1.Revision, error) {
	logger := logging.FromContext(ctx)

	if config.Spec.Build != nil {
		// TODO(mattmoor): Determine whether we reuse the previous build.
		build := resources.MakeBuild(config)
		gvr, _ := meta.UnsafeGuessKindToResource(build.GroupVersionKind())
		created, err := c.DynamicClientSet.Resource(gvr).Namespace(build.GetNamespace()).Create(build)
		if err != nil {
			return nil, err
		}
		logger.Infof("Created Build:\n%+v", created.GetName())
		c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Build %q", created.GetName())
	}

	rev := resources.MakeRevision(config)
	created, err := c.ServingClientSet.ServingV1alpha1().Revisions(config.Namespace).Create(rev)
	if err != nil {
		return nil, err
	}
	c.Recorder.Eventf(config, corev1.EventTypeNormal, "Created", "Created Revision %q", rev.Name)
	logger.Infof("Created Revision:\n%+v", created)

	return created, nil
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Configuration) (*v1alpha1.Configuration, error) {
	u, err := c.configurationLister.Configurations(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(u.Status, desired.Status) {
		// Don't modify the informers copy
		existing := u.DeepCopy()
		existing.Status = desired.Status
		// TODO: for CRD there's no updatestatus, so use normal update
		return c.ServingClientSet.ServingV1alpha1().Configurations(desired.Namespace).Update(existing)
		//	return configClient.UpdateStatus(newu)
	}
	return u, nil
}

func (c *Reconciler) gcRevisions(ctx context.Context, config *v1alpha1.Configuration) error {
	logger := logging.FromContext(ctx)

	selector := labels.Set{serving.ConfigurationLabelKey: config.Name}.AsSelector()
	revs, err := c.revisionLister.Revisions(config.Namespace).List(selector)
	if err != nil {
		return err
	}

	for _, rev := range revs {
		if isRevisionStale(ctx, rev, config) {
			err := c.ServingClientSet.ServingV1alpha1().Revisions(rev.Namespace).Delete(rev.Name, &metav1.DeleteOptions{})
			if err != nil {
				logger.Errorf("Failed to delete stale revision: %v", err)
				return err
			}
		}
	}
	return nil
}

func isRevisionStale(ctx context.Context, rev *v1alpha1.Revision, config *v1alpha1.Configuration) bool {
	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)

	// maxGen is the maximum generation number we consider for GC
	maxGen := config.Spec.Generation - cfg.StaleRevisionMinimumGenerations

	if config.Status.LatestReadyRevisionName == rev.Name {
		return false
	}

	// Check if rev is within "MinimumGenerations" of latest
	if gen, err := rev.GetConfigurationGeneration(); err != nil {
		logger.Errorf("Failed to determine revision configuration generation: %v", err)
		return false
	} else if gen > maxGen {
		return false
	}

	curTime := time.Now()
	if rev.ObjectMeta.CreationTimestamp.Add(cfg.StaleRevisionCreateDelay).After(curTime) {
		// Revision was created sooner than staleRevisionCreateDelay. Ignore it
		return false
	}

	lastPin, err := rev.GetLastPinned()
	if err != nil {
		if err.(v1alpha1.LastPinnedParseError).Type != v1alpha1.AnnotationParseErrorTypeMissing {
			logger.Errorf("Failed to determine revision last pinned: %v", err)
		}
		return false
	}

	ret := lastPin.Add(cfg.StaleRevisionTimeout).Before(curTime)
	if ret {
		logger.Infof("Detected stale revision %v with creation time %v and lastPinned time %v.", rev.ObjectMeta.Name, rev.ObjectMeta.CreationTimestamp, lastPin)
	}
	return ret
}
