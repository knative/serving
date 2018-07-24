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
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/controller/revision/config"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/equality"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	vpa "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"

	buildinformers "github.com/knative/build/pkg/client/informers/externalversions/build/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	vpav1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/poc.autoscaling.k8s.io/v1alpha1"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
)

const (
	controllerAgentName = "revision-controller"

	serviceTimeoutDuration = 5 * time.Minute
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
	Resolve(*appsv1.Deployment) error
}

// Controller implements the controller for Revision resources.
type Controller struct {
	*controller.Base

	// VpaClientSet allows us to configure VPA objects
	vpaClient vpa.Interface

	// lister indexes properties about Revision
	revisionLister   listers.RevisionLister
	buildLister      buildlisters.BuildLister
	deploymentLister appsv1listers.DeploymentLister
	serviceLister    corev1listers.ServiceLister
	endpointsLister  corev1listers.EndpointsLister
	configMapLister  corev1listers.ConfigMapLister

	buildtracker *buildTracker

	resolver      resolver
	resolverMutex sync.Mutex

	// controllerConfig could change over time and access to it
	// must go through controllerConfigMutex
	controllerConfig      *config.Controller
	controllerConfigMutex sync.Mutex

	// networkConfig could change over time and access to it
	// must go through networkConfigMutex
	networkConfig      *config.Network
	networkConfigMutex sync.Mutex

	// loggingConfig could change over time and access to it
	// must go through loggingConfigMutex
	loggingConfig      *logging.Config
	loggingConfigMutex sync.Mutex

	// observabilityConfig could change over time and access to it
	// must go through observabilityConfigMutex
	observabilityConfig      *config.Observability
	observabilityConfigMutex sync.Mutex

	// autoscalerConfig could change over time and access to it
	// must go through autoscalerConfigMutex
	autoscalerConfig      *autoscaler.Config
	autoscalerConfigMutex sync.Mutex
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// queue - message queue for handling new events.  unique to this controller.
func NewController(
	opt controller.Options,
	vpaClient vpa.Interface,
	revisionInformer servinginformers.RevisionInformer,
	buildInformer buildinformers.BuildInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	vpaInformer vpav1alpha1informers.VerticalPodAutoscalerInformer,
) *Controller {

	c := &Controller{
		Base:             controller.NewBase(opt, controllerAgentName, "Revisions"),
		vpaClient:        vpaClient,
		revisionLister:   revisionInformer.Lister(),
		buildLister:      buildInformer.Lister(),
		deploymentLister: deploymentInformer.Lister(),
		serviceLister:    serviceInformer.Lister(),
		endpointsLister:  endpointsInformer.Lister(),
		configMapLister:  configMapInformer.Lister(),
		buildtracker:     &buildTracker{builds: map[key]set{}},
	}

	// Set up an event handler for when the resource types of interest change
	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: controller.PassNew(c.Enqueue),
		DeleteFunc: c.Enqueue,
	})

	buildInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueBuildTrackers,
		UpdateFunc: controller.PassNew(c.EnqueueBuildTrackers),
	})

	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueEndpointsRevision,
		UpdateFunc: controller.PassNew(c.EnqueueEndpointsRevision),
	})

	deploymentInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter("Revision"),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(c.EnqueueControllerOf),
		},
	})

	configMapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter("Revision"),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    c.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(c.EnqueueControllerOf),
		},
	})

	opt.ConfigMapWatcher.Watch(config.NetworkConfigName, c.receiveNetworkConfig)
	opt.ConfigMapWatcher.Watch(logging.ConfigName, c.receiveLoggingConfig)
	opt.ConfigMapWatcher.Watch(config.ObservabilityConfigName, c.receiveObservabilityConfig)
	opt.ConfigMapWatcher.Watch(autoscaler.ConfigName, c.receiveAutoscalerConfig)
	opt.ConfigMapWatcher.Watch(config.ControllerConfigName, c.receiveControllerConfig)

	return c
}

// Run starts the controller's worker threads, the number of which is threadiness. It then blocks until stopCh
// is closed, at which point it shuts down its internal work queue and waits for workers to finish processing their
// current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	return c.RunController(threadiness, stopCh, c.Reconcile, "Revision")
}

// loggerWithRevisionInfo enriches the logs with revision name and namespace.
func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Revision, name))
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Revision resource
// with the current status of the resource.
func (c *Controller) Reconcile(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	logger := loggerWithRevisionInfo(c.Logger, namespace, name)
	ctx := logging.WithLogger(context.TODO(), logger)
	logger.Info("Running reconcile Revision")

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

func (c *Controller) reconcile(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)

	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(rev)

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
			name: "autoscaler deployment",
			f:    c.reconcileAutoscalerDeployment,
		}, {
			name: "autoscaler k8s service",
			f:    c.reconcileAutoscalerService,
		}, {
			name: "vertical pod autoscaler",
			f:    c.reconcileVPA,
		}}

		for _, phase := range phases {
			if err := phase.f(ctx, rev); err != nil {
				logger.Errorf("Failed to reconcile %s", phase.name, zap.Error(err))
				return err
			}
		}
	}

	return nil
}

func (c *Controller) updateRevisionLoggingURL(rev *v1alpha1.Revision) {
	logURLTmpl := c.getObservabilityConfig().LoggingURLTemplate
	if logURLTmpl != "" {
		rev.Status.LogURL = strings.Replace(logURLTmpl, "${REVISION_UID}", string(rev.UID), -1)
	}
}

func (c *Controller) EnqueueBuildTrackers(obj interface{}) {
	build := obj.(*buildv1alpha1.Build)

	// TODO(#1267): We should consider alternatives to the buildtracker that
	// allow us to shed this indexed state.
	if bc := getBuildDoneCondition(build); bc != nil {
		// For each of the revisions watching this build, mark their build phase as complete.
		for k := range c.buildtracker.GetTrackers(build) {
			c.EnqueueKey(string(k))
		}
	}
}

func (c *Controller) EnqueueEndpointsRevision(obj interface{}) {
	endpoints := obj.(*corev1.Endpoints)
	// Use the label on the Endpoints (from Service) to determine whether it is
	// owned by a Revision, and if so queue that Revision.
	if revisionName, ok := endpoints.Labels[serving.RevisionLabelKey]; ok {
		c.EnqueueKey(endpoints.Namespace + "/" + revisionName)
	}

}

func (c *Controller) receiveNetworkConfig(configMap *corev1.ConfigMap) {
	newNetworkConfig, err := config.NewNetworkFromConfigMap(configMap)
	c.networkConfigMutex.Lock()
	defer c.networkConfigMutex.Unlock()
	if err != nil {
		if c.networkConfig != nil {
			c.Logger.Errorf("Error updating Network ConfigMap: %v", err)
		} else {
			c.Logger.Fatalf("Error initializing Network ConfigMap: %v", err)
		}
		return
	}
	c.Logger.Infof("Network config map is added or updated: %v", configMap)
	c.networkConfig = newNetworkConfig
}

func (c *Controller) receiveLoggingConfig(configMap *corev1.ConfigMap) {
	newLoggingConfig, err := logging.NewConfigFromConfigMap(configMap)
	c.loggingConfigMutex.Lock()
	defer c.loggingConfigMutex.Unlock()
	if err != nil {
		if c.loggingConfig != nil {
			c.Logger.Error("Error updating Logging ConfigMap.", zap.Error(err))
		} else {
			c.Logger.Fatalf("Error initializing Logging ConfigMap: %v", err)
		}
		return
	}

	// TODO(mattmoor): When we support reconciling Deployment differences,
	// we should consider triggering a global reconciliation here to the
	// logging configuration changes are rolled out to active revisions.
	c.Logger.Infof("Logging config map is added or updated: %v", configMap)
	c.loggingConfig = newLoggingConfig
}

func (c *Controller) receiveControllerConfig(configMap *corev1.ConfigMap) {
	controllerConfig, err := config.NewControllerConfigFromConfigMap(configMap)

	c.controllerConfigMutex.Lock()
	defer c.controllerConfigMutex.Unlock()

	c.resolverMutex.Lock()
	defer c.resolverMutex.Unlock()

	if err != nil {
		if c.controllerConfig != nil {
			c.Logger.Errorf("Error updating Controller ConfigMap: %v", err)
		} else {
			c.Logger.Fatalf("Error initializing Controller ConfigMap: %v", err)
		}
		return
	}

	c.Logger.Infof("Controller config map is added or updated: %v", configMap)

	c.controllerConfig = controllerConfig
	c.resolver = &digestResolver{
		client:           c.KubeClientSet,
		transport:        http.DefaultTransport,
		registriesToSkip: controllerConfig.RegistriesSkippingTagResolving,
	}

}

func (c *Controller) receiveObservabilityConfig(configMap *corev1.ConfigMap) {
	newObservabilityConfig, err := config.NewObservabilityFromConfigMap(configMap)
	c.observabilityConfigMutex.Lock()
	defer c.observabilityConfigMutex.Unlock()
	if err != nil {
		if c.observabilityConfig != nil {
			c.Logger.Errorf("Error updating Observability ConfigMap: %v", err)
		} else {
			c.Logger.Fatalf("Error initializing Observability ConfigMap: %v", err)
		}
		return
	}
	c.Logger.Infof("Observability config map is added or updated: %v", configMap)
	c.observabilityConfig = newObservabilityConfig
}

func (c *Controller) receiveAutoscalerConfig(configMap *corev1.ConfigMap) {
	newAutoscalerConfig, err := autoscaler.NewConfigFromConfigMap(configMap)
	c.autoscalerConfigMutex.Lock()
	defer c.autoscalerConfigMutex.Unlock()
	if err != nil {
		if c.autoscalerConfig != nil {
			c.Logger.Errorf("Error updating Autoscaler ConfigMap: %v", err)
		} else {
			c.Logger.Fatalf("Error initializing Autoscaler ConfigMap: %v", err)
		}
		return
	}
	c.Logger.Infof("Autoscaler config map is added or updated: %v", configMap)
	c.autoscalerConfig = newAutoscalerConfig
}
