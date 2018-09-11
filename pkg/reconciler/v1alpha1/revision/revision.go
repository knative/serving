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

package revision

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildinformers "github.com/knative/build/pkg/client/informers/externalversions/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions/caching/v1alpha1"
	cachinglisters "github.com/knative/caching/pkg/client/listers/caching/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	commonlogging "github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	kpainformers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	kpalisters "github.com/knative/serving/pkg/client/listers/autoscaling/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	vpav1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/poc.autoscaling.k8s.io/v1alpha1"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "revision-controller"
)

var (
	foregroundDeletion = metav1.DeletePropagationForeground
	fgDeleteOptions    = &metav1.DeleteOptions{
		PropagationPolicy: &foregroundDeletion,
	}
)

type Changed bool

const (
	WasChanged Changed = true
	Unchanged  Changed = false
)

type resolver interface {
	Resolve(*appsv1.Deployment, map[string]struct{}) error
}

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// Reconciler implements controller.Reconciler for Revision resources.
type Reconciler struct {
	*reconciler.Base

	// VpaClientSet allows us to configure VPA objects
	vpaClient vpa.Interface

	// lister indexes properties about Revision
	revisionLister   listers.RevisionLister
	kpaLister        kpalisters.PodAutoscalerLister
	buildLister      buildlisters.BuildLister
	imageLister      cachinglisters.ImageLister
	deploymentLister appsv1listers.DeploymentLister
	serviceLister    corev1listers.ServiceLister
	endpointsLister  corev1listers.EndpointsLister
	configMapLister  corev1listers.ConfigMapLister

	buildtracker *buildTracker
	resolver     resolver
	configStore  configStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// queue - message queue for handling new events.  unique to this controller.
func NewController(
	opt reconciler.Options,
	vpaClient vpa.Interface,
	revisionInformer servinginformers.RevisionInformer,
	kpaInformer kpainformers.PodAutoscalerInformer,
	buildInformer buildinformers.BuildInformer,
	imageInformer cachinginformers.ImageInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	vpaInformer vpav1alpha1informers.VerticalPodAutoscalerInformer,
) *controller.Impl {

	configStore := &config.Store{}

	c := &Reconciler{
		Base:             reconciler.NewBase(opt, controllerAgentName),
		vpaClient:        vpaClient,
		revisionLister:   revisionInformer.Lister(),
		kpaLister:        kpaInformer.Lister(),
		buildLister:      buildInformer.Lister(),
		imageLister:      imageInformer.Lister(),
		deploymentLister: deploymentInformer.Lister(),
		serviceLister:    serviceInformer.Lister(),
		endpointsLister:  endpointsInformer.Lister(),
		configMapLister:  configMapInformer.Lister(),
		buildtracker:     &buildTracker{builds: map[key]set{}},
		configStore:      configStore,
		resolver: &digestResolver{
			client:    opt.KubeClientSet,
			transport: http.DefaultTransport,
		},
	}

	configStore.Logger = c.Logger.Named("config-store")

	impl := controller.NewImpl(c, c.Logger, "Revisions")

	// Set up an event handler for when the resource types of interest change
	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	buildInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueBuildTrackers(impl),
		UpdateFunc: controller.PassNew(c.EnqueueBuildTrackers(impl)),
	})

	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueEndpointsRevision(impl),
		UpdateFunc: controller.PassNew(c.EnqueueEndpointsRevision(impl)),
	})

	deploymentInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		},
	})

	// We don't watch for changes to Image because we don't incorporate any of its
	// properties into our own status and should work completely in the absence of
	// a functioning Image controller.

	configMapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		},
	})

	// TODO(mattmoor): When we support reconciling Deployment differences,
	// we should consider triggering a global reconciliation here to the
	// logging configuration changes are rolled out to active revisions.
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Revision resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := commonlogging.FromContext(ctx)
	logger.Info("Running reconcile Revision")

	ctx = c.configStore.ToContext(ctx)

	// Get the Revision resource with this namespace/name
	original, err := c.revisionLister.Revisions(namespace).Get(name)
	// The resource may no longer exist, in which case we stop processing.
	if apierrs.IsNotFound(err) {
		logger.Errorf("revision %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informer's copy.
	rev := original.DeepCopy()

	// Reconcile this copy of the revision and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, rev)
	if equality.Semantic.DeepEqual(original.Status, rev.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		// logger.Infof("Updating Status (-old, +new): %v", cmp.Diff(original, rev))
		if _, err := c.updateStatus(rev); err != nil {
			logger.Warn("Failed to update revision status", zap.Error(err))
			return err
		}
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := commonlogging.FromContext(ctx)

	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(ctx, rev)

	if rev.Spec.BuildName != "" {
		rev.Status.InitializeBuildCondition()
		build, err := c.buildLister.Builds(rev.Namespace).Get(rev.Spec.BuildName)
		if err != nil {
			logger.Errorf("Error fetching Build %q for Revision %q: %v", rev.Spec.BuildName, rev.Name, err)
			return err
		}
		before := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
		rev.Status.PropagateBuildStatus(build.Status)
		after := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
		if before.Status != after.Status {
			// Create events when the Build result is in.
			if after.Status == corev1.ConditionTrue {
				c.Recorder.Event(rev, corev1.EventTypeNormal, "BuildSucceeded", after.Message)
				c.buildtracker.Untrack(rev)
			} else if after.Status == corev1.ConditionFalse {
				c.Recorder.Event(rev, corev1.EventTypeWarning, "BuildFailed", after.Message)
				c.buildtracker.Untrack(rev)
			}
		}
		// If the Build isn't done, then add it to our tracker.
		// TODO(#1267): See the comment in SyncBuild about replacing this utility.
		if after.Status == corev1.ConditionUnknown {
			logger.Errorf("Tracking revision: %v", rev.Name)
			c.buildtracker.Track(rev)
		}
	}

	bc := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	if bc == nil || bc.Status == corev1.ConditionTrue {
		// There is no build, or the build completed successfully.

		phases := []struct {
			name string
			f    func(context.Context, *v1alpha1.Revision) error
		}{{
			name: "user deployment",
			f:    c.reconcileDeployment,
		}, {
			name: "user k8s service",
			f:    c.reconcileService,
		}, {
			// Ensures our namespace has the configuration for the fluentd sidecar.
			name: "fluentd configmap",
			f:    c.reconcileFluentdConfigMap,
		}, {
			name: "KPA",
			f:    c.reconcileKPA,
		}, {
			name: "vertical pod autoscaler",
			f:    c.reconcileVPA,
		}}

		for _, phase := range phases {
			if err := phase.f(ctx, rev); err != nil {
				logger.Errorf("Failed to reconcile %s: %v", phase.name, zap.Error(err))
				return err
			}
		}
	}

	return nil
}

func (c *Reconciler) updateRevisionLoggingURL(
	ctx context.Context,
	rev *v1alpha1.Revision,
) {

	config := config.FromContext(ctx)
	if config.Observability.LoggingURLTemplate == "" {
		return
	}

	uid := string(rev.UID)

	rev.Status.LogURL = strings.Replace(
		config.Observability.LoggingURLTemplate,
		"${REVISION_UID}", uid, -1)
}

func (c *Reconciler) EnqueueBuildTrackers(impl *controller.Impl) func(obj interface{}) {
	return func(obj interface{}) {
		build := obj.(*buildv1alpha1.Build)

		// TODO(#1267): We should consider alternatives to the buildtracker that
		// allow us to shed this indexed state.
		if bc := getBuildDoneCondition(build); bc != nil {
			// For each of the revisions watching this build, mark their build phase as complete.
			for k := range c.buildtracker.GetTrackers(build) {
				impl.EnqueueKey(string(k))
			}
		}
	}
}

func (c *Reconciler) EnqueueEndpointsRevision(impl *controller.Impl) func(obj interface{}) {
	return func(obj interface{}) {
		endpoints := obj.(*corev1.Endpoints)
		// Use the label on the Endpoints (from Service) to determine whether it is
		// owned by a Revision, and if so queue that Revision.
		if revisionName, ok := endpoints.Labels[serving.RevisionLabelKey]; ok {
			impl.EnqueueKey(endpoints.Namespace + "/" + revisionName)
		}
	}
}

func (c *Reconciler) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	newRev, err := c.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(newRev.Status, rev.Status) {
		newRev.Status = rev.Status

		// TODO: for CRD there's no updatestatus, so use normal update
		return c.ServingClientSet.ServingV1alpha1().Revisions(rev.Namespace).Update(newRev)
		//	return prClient.UpdateStatus(newRev)
	}
	return rev, nil
}
