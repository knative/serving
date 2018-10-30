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

	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	cachinginformers "github.com/knative/caching/pkg/client/informers/externalversions/caching/v1alpha1"
	cachinglisters "github.com/knative/caching/pkg/client/listers/caching/v1alpha1"
	"github.com/knative/pkg/apis/duck"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	commonlogging "github.com/knative/pkg/logging"
	"github.com/knative/pkg/tracker"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	kpainformers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	kpalisters "github.com/knative/serving/pkg/client/listers/autoscaling/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/revision/config"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Resolve(string, k8schain.Options, map[string]struct{}) (string, error)
}

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
	Load() *config.Config
}

// Reconciler implements controller.Reconciler for Revision resources.
type Reconciler struct {
	*reconciler.Base

	// lister indexes properties about Revision
	revisionLister   listers.RevisionLister
	kpaLister        kpalisters.PodAutoscalerLister
	imageLister      cachinglisters.ImageLister
	deploymentLister appsv1listers.DeploymentLister
	serviceLister    corev1listers.ServiceLister
	endpointsLister  corev1listers.EndpointsLister
	configMapLister  corev1listers.ConfigMapLister

	buildInformerFactory duck.InformerFactory

	tracker     tracker.Interface
	resolver    resolver
	configStore configStore
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
	revisionInformer servinginformers.RevisionInformer,
	kpaInformer kpainformers.PodAutoscalerInformer,
	imageInformer cachinginformers.ImageInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	buildInformerFactory duck.InformerFactory,
) *controller.Impl {

	c := &Reconciler{
		Base:             reconciler.NewBase(opt, controllerAgentName),
		revisionLister:   revisionInformer.Lister(),
		kpaLister:        kpaInformer.Lister(),
		imageLister:      imageInformer.Lister(),
		deploymentLister: deploymentInformer.Lister(),
		serviceLister:    serviceInformer.Lister(),
		endpointsLister:  endpointsInformer.Lister(),
		configMapLister:  configMapInformer.Lister(),
		resolver: &digestResolver{
			client:    opt.KubeClientSet,
			transport: http.DefaultTransport,
		},
	}
	impl := controller.NewImpl(c, c.Logger, "Revisions", reconciler.MustNewStatsReporter("Revisions", c.Logger))

	// Set up an event handler for when the resource types of interest change
	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
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

	kpaInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Revision")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		},
	})

	c.tracker = tracker.New(impl.EnqueueKey, opt.GetTrackerLease())

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

	c.buildInformerFactory = newDuckInformerFactory(c.tracker, buildInformerFactory)

	// TODO(mattmoor): When we support reconciling Deployment differences,
	// we should consider triggering a global reconciliation here to the
	// logging configuration changes are rolled out to active revisions.
	c.configStore = config.NewStore(c.Logger.Named("config-store"))
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}

func KResourceTypedInformerFactory(opt reconciler.Options) duck.InformerFactory {
	return &duck.TypedInformerFactory{
		Client:       opt.DynamicClientSet,
		Type:         &duckv1alpha1.KResource{},
		ResyncPeriod: opt.ResyncPeriod,
		StopChannel:  opt.StopChannel,
	}
}

func newDuckInformerFactory(t tracker.Interface, delegate duck.InformerFactory) duck.InformerFactory {
	return &duck.CachedInformerFactory{
		Delegate: &duck.EnqueueInformerFactory{
			Delegate: delegate,
			EventHandler: cache.ResourceEventHandlerFuncs{
				AddFunc:    t.OnChanged,
				UpdateFunc: controller.PassNew(t.OnChanged),
			},
		},
	}
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

func (c *Reconciler) reconcileBuild(ctx context.Context, rev *v1alpha1.Revision) error {
	buildRef := rev.BuildRef()
	if buildRef == nil {
		return nil
	}

	logger := commonlogging.FromContext(ctx)

	if err := c.tracker.Track(*buildRef, rev); err != nil {
		logger.Errorf("Error tracking build '%+v' for Revision %q: %+v", buildRef, rev.Name, err)
		return err
	}
	rev.Status.InitializeBuildCondition()

	gvr, _ := meta.UnsafeGuessKindToResource(buildRef.GroupVersionKind())
	_, lister, err := c.buildInformerFactory.Get(gvr)
	if err != nil {
		logger.Errorf("Error getting a lister for a builds resource '%+v': %+v", gvr, err)
		return err
	}

	buildObj, err := lister.ByNamespace(rev.Namespace).Get(buildRef.Name)
	if err != nil {
		logger.Errorf("Error fetching Build %q for Revision %q: %v", buildRef.Name, rev.Name, err)
		return err
	}
	build := buildObj.(*duckv1alpha1.KResource)

	before := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	rev.Status.PropagateBuildStatus(build.Status)
	after := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	if before.Status != after.Status {
		// Create events when the Build result is in.
		if after.Status == corev1.ConditionTrue {
			c.Recorder.Event(rev, corev1.EventTypeNormal, "BuildSucceeded", after.Message)
		} else if after.Status == corev1.ConditionFalse {
			c.Recorder.Event(rev, corev1.EventTypeWarning, "BuildFailed", after.Message)
		}
	}

	return nil
}

func (c *Reconciler) reconcileDigest(ctx context.Context, rev *v1alpha1.Revision) error {
	// The image digest has already been resolved.
	if rev.Status.ImageDigest != "" {
		return nil
	}

	cfgs := config.FromContext(ctx)
	opt := k8schain.Options{
		Namespace:          rev.Namespace,
		ServiceAccountName: rev.Spec.ServiceAccountName,
		// ImagePullSecrets: Not possible via RevisionSpec, since we
		// don't expose such a field.
	}
	digest, err := c.resolver.Resolve(rev.Spec.Container.Image, opt, cfgs.Controller.RegistriesSkippingTagResolving)
	if err != nil {
		rev.Status.MarkContainerMissing(err.Error())
		return err
	}

	rev.Status.ImageDigest = digest

	return nil
}

func (c *Reconciler) reconcile(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := commonlogging.FromContext(ctx)

	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(ctx, rev)

	if err := c.reconcileBuild(ctx, rev); err != nil {
		return err
	}

	bc := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	if bc == nil || bc.Status == corev1.ConditionTrue {
		// There is no build, or the build completed successfully.

		phases := []struct {
			name string
			f    func(context.Context, *v1alpha1.Revision) error
		}{{
			name: "image digest",
			f:    c.reconcileDigest,
		}, {
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

func (c *Reconciler) updateStatus(desired *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	rev, err := c.revisionLister.Revisions(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(rev.Status, desired.Status) {
		// Don't modify the informers copy
		existing := rev.DeepCopy()
		existing.Status = desired.Status

		// TODO: for CRD there's no updatestatus, so use normal update
		return c.ServingClientSet.ServingV1alpha1().Revisions(desired.Namespace).Update(existing)
		//	return prClient.UpdateStatus(newRev)
	}
	return rev, nil
}
