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
	"reflect"
	"strings"
	"sync"
	"time"

	// TODO(mattmoor): Used by the commented checkAndUpdateDeployment logic below.
	// "github.com/google/go-cmp/cmp/cmpopts"
	// "k8s.io/apimachinery/pkg/api/resource"

	"github.com/knative/serving/pkg"

	"github.com/google/go-cmp/cmp"
	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/equality"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	buildinformers "github.com/knative/build/pkg/client/informers/externalversions/build/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	vpav1alpha1informers "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/informers/externalversions/poc.autoscaling.k8s.io/v1alpha1"
	appsv1informers "k8s.io/client-go/informers/apps/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/runtime"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	vpalisters "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/listers/poc.autoscaling.k8s.io/v1alpha1"
)

const (
	userContainerName string = "user-container"
	userPortName      string = "user-port"
	userPort                 = 8080

	fluentdContainerName string = "fluentd-proxy"
	queueContainerName   string = "queue-proxy"
	envoyContainerName   string = "istio-proxy"
	// queueSidecarName set by -queueSidecarName flag
	queueHTTPPortName string = "queue-http-port"

	requestQueueContainerName string = "request-queue"

	controllerAgentName = "revision-controller"
	autoscalerPort      = 8080

	serviceTimeoutDuration       = 5 * time.Minute
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
)

var (
	elaPodReplicaCount   = int32(1)
	elaPodMaxUnavailable = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	elaPodMaxSurge       = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	foregroundDeletion   = metav1.DeletePropagationForeground
	fgDeleteOptions      = &metav1.DeleteOptions{
		PropagationPolicy: &foregroundDeletion,
	}
)

type resolver interface {
	Resolve(*appsv1.Deployment) error
}

// Controller implements the controller for Revision resources.
type Controller struct {
	*controller.Base

	// VpaLister allows us to configure VPA objects

	vpaLister vpalisters.VerticalPodAutoscalerLister

	// lister indexes properties about Revision
	revisionLister   listers.RevisionLister
	buildLister      buildlisters.BuildLister
	deploymentLister appsv1listers.DeploymentLister
	serviceLister    corev1listers.ServiceLister
	endpointsLister  corev1listers.EndpointsLister

	buildtracker *buildTracker

	resolver resolver

	// enableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	enableVarLogCollection bool

	// controllerConfig includes the configurations for the controller
	controllerConfig *ControllerConfig

	// networkConfig could change over time and access to it
	// must go through networkConfigMutex
	networkConfig      *NetworkConfig
	networkConfigMutex sync.Mutex
}

// ControllerConfig includes the configurations for the controller.
type ControllerConfig struct {
	// Autoscale part

	// see (config-autoscaler.yaml)
	AutoscaleConcurrencyQuantumOfTime     *k8sflag.DurationFlag
	AutoscaleEnableSingleConcurrency      *k8sflag.BoolFlag
	AutoscaleEnableVerticalPodAutoscaling *k8sflag.BoolFlag

	// AutoscalerImage is the name of the image used for the autoscaler pod.
	AutoscalerImage string

	// QueueSidecarImage is the name of the image used for the queue sidecar
	// injected into the revision pod
	QueueSidecarImage string

	// logging part

	// EnableVarLogCollection dedicates whether to set up a fluentd sidecar to
	// collect logs under /var/log/.
	EnableVarLogCollection bool

	// TODO(#818): Use the fluentd deamon set to collect /var/log.
	// FluentdSidecarImage is the name of the image used for the fluentd sidecar
	// injected into the revision pod. It is used only when enableVarLogCollection
	// is true.
	FluentdSidecarImage string
	// FluentdSidecarOutputConfig is the config for fluentd sidecar to specify
	// logging output destination.
	FluentdSidecarOutputConfig string

	// LoggingURLTemplate is a string containing the logging url template where
	// the variable REVISION_UID will be replaced with the created revision's UID.
	LoggingURLTemplate string

	// QueueProxyLoggingConfig is a string containing the logger configuration for queue proxy.
	QueueProxyLoggingConfig string

	// QueueProxyLoggingLevel is a string containing the logger level for queue proxy.
	QueueProxyLoggingLevel string
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// queue - message queue for handling new events.  unique to this controller.
func NewController(
	opt controller.Options,
	revisionInformer servinginformers.RevisionInformer,
	buildInformer buildinformers.BuildInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
	serviceInformer corev1informers.ServiceInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	vpaInformer vpav1alpha1informers.VerticalPodAutoscalerInformer,
	config *rest.Config,
	controllerConfig *ControllerConfig) controller.Interface {

	networkConfig, err := NewNetworkConfig(opt.KubeClientSet)
	if err != nil {
		opt.Logger.Fatalf("Error loading network config: %v", err)
	}

	c := &Controller{
		Base:             controller.NewBase(opt, controllerAgentName, "Revisions"),
		vpaLister:        vpaInformer.Lister(),
		revisionLister:   revisionInformer.Lister(),
		buildLister:      buildInformer.Lister(),
		deploymentLister: deploymentInformer.Lister(),
		serviceLister:    serviceInformer.Lister(),
		endpointsLister:  endpointsInformer.Lister(),
		buildtracker:     &buildTracker{builds: map[key]set{}},
		resolver:         &digestResolver{client: opt.KubeClientSet, transport: http.DefaultTransport},
		controllerConfig: controllerConfig,
		networkConfig:    networkConfig,
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

	configMapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addConfigMapEvent,
		UpdateFunc: c.updateConfigMapEvent,
	})

	return c
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
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
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	logger := loggerWithRevisionInfo(c.Logger, namespace, name)
	ctx := logging.WithLogger(context.TODO(), logger)
	logger.Info("Running reconcile Revision")

	// Get the Revision resource with this namespace/name
	original, err := c.revisionLister.Revisions(namespace).Get(name)
	// The resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		runtime.HandleError(fmt.Errorf("revision %q in work queue no longer exists", key))
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
	} else if _, err := c.updateStatus(rev); err != nil {
		logger.Warn("Failed to update revision status", zap.Error(err))
		return err
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

		// Set up the user resources
		if err := c.reconcileDeployment(ctx, rev); err != nil {
			logger.Error("Failed to create a deployment", zap.Error(err))
			return err
		}
		if err := c.reconcileService(ctx, rev); err != nil {
			logger.Error("Failed to create k8s service", zap.Error(err))
			return err
		}

		switch rev.Spec.ServingState {
		case v1alpha1.RevisionServingStateActive:
			return c.createK8SResources(ctx, rev)

		case v1alpha1.RevisionServingStateReserve:
			return c.deleteK8SResources(ctx, rev)

		// TODO(mattmoor): Nothing sets this state, and it should be removed.
		case v1alpha1.RevisionServingStateRetired:
			return c.deleteK8SResources(ctx, rev)

		default:
			logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
		}
	}

	return nil
}

func (c *Controller) updateRevisionLoggingURL(rev *v1alpha1.Revision) {
	logURLTmpl := c.controllerConfig.LoggingURLTemplate
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

func (c *Controller) deleteK8SResources(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)

	// Delete the resources we set up to autoscale the user resources.
	if err := c.deleteAutoscalerDeployment(ctx, rev); err != nil {
		logger.Error("Failed to delete autoscaler Deployment", zap.Error(err))
		return err
	}
	if err := c.deleteAutoscalerService(ctx, rev); err != nil {
		logger.Error("Failed to delete autoscaler Service", zap.Error(err))
		return err
	}
	if err := c.deleteVpa(ctx, rev); err != nil {
		logger.Error("Failed to delete VPA", zap.Error(err))
		return err
	}

	// And the deployment is no longer ready, so update that
	rev.Status.MarkInactive()

	return nil
}

func (c *Controller) createK8SResources(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)

	// Set up resources to autoscale the user resources.
	if err := c.reconcileAutoscalerDeployment(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Deployment", zap.Error(err))
		return err
	}
	if err := c.reconcileAutoscalerService(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Service", zap.Error(err))
		return err
	}
	if err := c.reconcileVpa(ctx, rev); err != nil {
		logger.Error("Failed to create the vertical pod autoscaler for Deployment", zap.Error(err))
		return err
	}

	// Ensure our namespace has the configuration for the fluentd sidecar.
	if err := c.reconcileFluentdConfigMap(ctx, rev); err != nil {
		logger.Error("Failed to create fluent config map", zap.Error(err))
		return err
	}

	return nil
}

func (c *Controller) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	deploymentName := controller.GetRevisionDeploymentName(rev)

	deployment, err := c.deploymentLister.Deployments(ns).Get(deploymentName)
	switch rev.Spec.ServingState {
	case v1alpha1.RevisionServingStateActive:
		// When Active, the Deployment should exist and have a particular specification.
		if apierrs.IsNotFound(err) {
			// If it does not exist, then create it.
			rev.Status.MarkDeploying("Deploying")
			deployment, err = c.createDeployment(ctx, rev)
			if err != nil {
				logger.Errorf("Error creating Deployment %q: %v", deploymentName, err)
				return err
			}
			logger.Infof("Created Deployment %q", deploymentName)
		} else if err != nil {
			logger.Errorf("Error reconciling Active Deployment %q: %v", deploymentName, err)
			return err
		} else {
			// TODO(mattmoor): Don't reconcile Deployments until we can avoid fighting with
			// its defaulter.
			// // If it exists, then make sure if looks as we expect.
			// // It may change if a user edits things around our controller, which we
			// // should not allow, or if our expectations of how the deployment should look
			// // changes (e.g. we update our controller with new sidecars).
			// var updated bool
			// deployment, updated, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
			// if err != nil {
			// 	logger.Errorf("Error updating Deployment %q: %v", deploymentName, err)
			// 	return err
			// }
			// if updated {
			// 	logger.Infof("Updated Deployment %q", deploymentName)
			// 	rev.Status.MarkDeploying("Updating")
			// }
		}

		// Now that we have a Deployment, determine whether there is any relevant
		// status to surface in the Revision.
		if cond := getDeploymentProgressCondition(deployment); cond != nil {
			rev.Status.MarkProgressDeadlineExceeded(fmt.Sprintf(
				"Unable to create pods for more than %d seconds.", progressDeadlineSeconds))
			c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded",
				"Revision %s not ready due to Deployment timeout", rev.Name)
		}
		return nil

	case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
		// When Reserve or Retired, we remove the underlying Deployment.
		if apierrs.IsNotFound(err) {
			// If it does not exist, then we have nothing to do.
			return nil
		}
		if err := c.deleteDeployment(ctx, deployment); err != nil {
			logger.Errorf("Error deleting Deployment %q: %v", deploymentName, err)
			return err
		}
		logger.Infof("Deleted Deployment %q", deploymentName)
		rev.Status.MarkInactive()
		return nil

	default:
		logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
		return nil
	}
}

func (c *Controller) createDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)

	// Create the deployment.
	deployment := MakeServingDeployment(logger, rev, c.getNetworkConfig(), c.controllerConfig)

	// Resolve tag image references to digests.
	if err := c.resolver.Resolve(deployment); err != nil {
		logger.Error("Error resolving deployment", zap.Error(err))
		rev.Status.MarkContainerMissing(err.Error())
		return nil, fmt.Errorf("Error resolving container to digest: %v", err)
	}

	return c.KubeClientSet.AppsV1().Deployments(ns).Create(deployment)
}

// TODO(mattmoor): See the comment at the commented call site above.
// func (c *Controller) checkAndUpdateDeployment(ctx context.Context, rev *v1alpha1.Revision, deployment *appsv1.Deployment) (*appsv1.Deployment, bool, error) {
// 	logger := logging.FromContext(ctx)

// 	desiredDeployment := MakeServingDeployment(logger, rev, c.getNetworkConfig(), c.controllerConfig)

// 	// Copy the userContainerImage digest and the replica count.
// 	// We don't want autoscaling differences or user updates to the image tag
// 	// to trigger redeployments.
// 	desiredDeployment.Spec.Replicas = deployment.Spec.Replicas
// 	pod := desiredDeployment.Spec.Template.Spec
// 	for i := range pod.Containers {
// 		if pod.Containers[i].Name == userContainerName {
// 			pod.Containers[i].Image = desiredDeployment.Spec.Template.Spec.Containers[i].Image
// 		}
// 	}

// 	if equality.Semantic.DeepEqual(desiredDeployment.Spec, deployment.Spec) {
// 		return deployment, false, nil
// 	}
// 	logger.Infof("Reconciling deployment diff (-desired, +observed): %v",
// 		cmp.Diff(desiredDeployment.Spec, deployment.Spec, cmpopts.IgnoreUnexported(resource.Quantity{})))
// 	deployment.Spec = desiredDeployment.Spec

// 	d, err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Update(deployment)
// 	return d, true, err
// }

func (c *Controller) deleteDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	logger := logging.FromContext(ctx)

	err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Delete(deployment.Name, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("deployments.Delete for %q failed: %s", deployment.Name, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	serviceName := controller.GetServingK8SServiceNameForRevision(rev)

	rev.Status.ServiceName = serviceName

	service, err := c.serviceLister.Services(ns).Get(serviceName)
	switch rev.Spec.ServingState {
	case v1alpha1.RevisionServingStateActive:
		// When Active, the Service should exist and have a particular specification.
		if apierrs.IsNotFound(err) {
			// If it does not exist, then create it.
			rev.Status.MarkDeploying("Deploying")
			service, err = c.createService(ctx, rev)
			if err != nil {
				logger.Errorf("Error creating Service %q: %v", serviceName, err)
				return err
			}
			logger.Infof("Created Service %q", serviceName)
		} else if err != nil {
			logger.Errorf("Error reconciling Active Service %q: %v", serviceName, err)
			return err
		} else {
			// If it exists, then make sure if looks as we expect.
			// It may change if a user edits things around our controller, which we
			// should not allow, or if our expectations of how the service should look
			// changes (e.g. we update our controller with new sidecars).
			var updated bool
			service, updated, err = c.checkAndUpdateService(ctx, rev, service)
			if err != nil {
				logger.Errorf("Error updating Service %q: %v", serviceName, err)
				return err
			}
			if updated {
				logger.Infof("Updated Service %q", serviceName)
				rev.Status.MarkDeploying("Updating")
			}
		}

		// We cannot determine readiness from the Service directly.  Instead, we look up
		// the backing Endpoints resource and check it for healthy pods.  The name of the
		// Endpoints resource matches the Service it backs.
		endpoints, err := c.endpointsLister.Endpoints(ns).Get(serviceName)
		if apierrs.IsNotFound(err) {
			// If it isn't found, then we need to wait for the Service controller to
			// create it.
			rev.Status.MarkDeploying("Deploying")
			return nil
		} else if err != nil {
			logger.Errorf("Error checking Active Endpoints %q: %v", serviceName, err)
			return err
		}
		// If the endpoints resource indicates that the Service it sits in front of is ready,
		// then surface this in our Revision status as resources available (pods were scheduled)
		// and container healthy (endpoints should be gated by any provided readiness checks).
		if getIsServiceReady(endpoints) {
			rev.Status.MarkResourcesAvailable()
			rev.Status.MarkContainerHealthy()
			// TODO(mattmoor): How to ensure this only fires once?
			c.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady",
				"Revision becomes ready upon endpoint %q becoming ready", serviceName)
		} else {
			// If the endpoints is NOT ready, then check whether it is taking unreasonably
			// long to become ready and if so mark our revision as having timed out waiting
			// for the Service to become ready.
			revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
			if revisionAge >= serviceTimeoutDuration {
				rev.Status.MarkServiceTimeout()
				// TODO(mattmoor): How to ensure this only fires once?
				c.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed",
					"Revision did not become ready due to endpoint %q", serviceName)
			}
		}
		return nil

	case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
		// When Reserve or Retired, we remove the underlying Service.
		if apierrs.IsNotFound(err) {
			// If it does not exist, then we have nothing to do.
			return nil
		}
		if err := c.deleteService(ctx, service); err != nil {
			logger.Errorf("Error deleting Service %q: %v", serviceName, err)
			return err
		}
		logger.Infof("Deleted Service %q", serviceName)
		rev.Status.MarkInactive()
		return nil

	default:
		logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
		return nil
	}
}

func (c *Controller) createService(ctx context.Context, rev *v1alpha1.Revision) (*corev1.Service, error) {
	ns := controller.GetServingNamespaceName(rev.Namespace)

	// Create the service.
	service := MakeRevisionK8sService(rev)

	return c.KubeClientSet.CoreV1().Services(ns).Create(service)
}

func (c *Controller) checkAndUpdateService(ctx context.Context, rev *v1alpha1.Revision, service *corev1.Service) (*corev1.Service, bool, error) {
	logger := logging.FromContext(ctx)

	desiredService := MakeRevisionK8sService(rev)

	if equality.Semantic.DeepEqual(desiredService.Spec, service.Spec) {
		return service, false, nil
	}
	logger.Infof("Reconciling service diff (-desired, +observed): %v",
		cmp.Diff(desiredService.Spec, service.Spec))
	service.Spec = desiredService.Spec

	d, err := c.KubeClientSet.CoreV1().Services(service.Namespace).Update(service)
	return d, true, err
}

func (c *Controller) deleteService(ctx context.Context, svc *corev1.Service) error {
	logger := logging.FromContext(ctx)

	err := c.KubeClientSet.CoreV1().Services(svc.Namespace).Delete(svc.Name, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("service.Delete for %q failed: %s", svc.Name, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileFluentdConfigMap(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	if !c.controllerConfig.EnableVarLogCollection {
		return nil
	}
	ns := rev.Namespace

	// One ConfigMap for Fluentd sidecar per namespace. It has multiple owner
	// references. Can not set blockOwnerDeletion and Controller to true.
	revRef := newRevisionNonControllerRef(rev)

	cmc := c.KubeClientSet.CoreV1().ConfigMaps(ns)
	configMap, err := cmc.Get(fluentdConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("configmaps.Get for %q failed: %s", fluentdConfigMapName, err)
			return err
		}
		// ConfigMap doesn't exist, going to create it
		configMap = MakeFluentdConfigMap(ns, c.controllerConfig.FluentdSidecarOutputConfig)
		configMap.OwnerReferences = append(configMap.OwnerReferences, *revRef)
		logger.Infof("Creating configmap: %q", configMap.Name)
		_, err = cmc.Create(configMap)
		return err
	}

	// ConfigMap exists, going to update it
	desiredConfigMap := configMap.DeepCopy()
	desiredConfigMap.Data = map[string]string{
		"varlog.conf": makeFullFluentdConfig(c.controllerConfig.FluentdSidecarOutputConfig),
	}
	addOwnerReference(desiredConfigMap, revRef)
	if !reflect.DeepEqual(desiredConfigMap, configMap) {
		logger.Infof("Updating configmap: %q", desiredConfigMap.Name)
		_, err = cmc.Update(desiredConfigMap)
		return err
	}
	return nil
}

func newRevisionNonControllerRef(rev *v1alpha1.Revision) *metav1.OwnerReference {
	blockOwnerDeletion := false
	isController := false
	revRef := controller.NewRevisionControllerRef(rev)
	revRef.BlockOwnerDeletion = &blockOwnerDeletion
	revRef.Controller = &isController
	return revRef
}

func addOwnerReference(configMap *corev1.ConfigMap, ownerReference *metav1.OwnerReference) {
	isOwner := false
	for _, existingOwner := range configMap.OwnerReferences {
		if ownerReference.Name == existingOwner.Name {
			isOwner = true
			break
		}
	}
	if !isOwner {
		configMap.OwnerReferences = append(configMap.OwnerReferences, *ownerReference)
	}
}

func (c *Controller) deleteAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()

	err := c.KubeClientSet.CoreV1().Services(ns).Delete(autoscalerName, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("Autoscaler Service delete for %q failed: %s", autoscalerName, err)
		return err
	}
	logger.Infof("Deleted autoscaler Service %q", autoscalerName)
	return nil
}

func (c *Controller) reconcileAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()
	sc := c.KubeClientSet.CoreV1().Services(ns)
	_, err := sc.Get(autoscalerName, metav1.GetOptions{})
	if err == nil {
		logger.Infof("Found existing autoscaler Service %q", autoscalerName)
		return nil
	}
	if !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Service get for %q failed: %s", autoscalerName, err)
		return err
	}
	logger.Infof("Autoscaler Service %q doesn't exist, creating", autoscalerName)

	service := MakeServingAutoscalerService(rev)
	logger.Infof("Creating autoscaler Service: %q", service.Name)
	_, err = sc.Create(service)
	return err
}

func (c *Controller) deleteAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()

	err := c.KubeClientSet.AppsV1().Deployments(ns).Delete(autoscalerName, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("Autoscaler Deployment delete for %q failed: %s", autoscalerName, err)
		return err
	}
	logger.Infof("Deleted autoscaler Deployment %q", autoscalerName)
	return nil
}

func (c *Controller) reconcileAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err == nil {
		logger.Infof("Found existing autoscaler Deployment %q", autoscalerName)
		return nil
	}
	if !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Deployment get for %q failed: %s", autoscalerName, err)
		return err
	}
	logger.Infof("Autoscaler Deployment %q doesn't exist, creating", autoscalerName)

	deployment := MakeServingAutoscalerDeployment(rev, c.controllerConfig.AutoscalerImage)
	logger.Infof("Creating autoscaler Deployment: %q", deployment.Name)
	_, err = dc.Create(deployment)
	return err
}

func (c *Controller) deleteVpa(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	if !c.controllerConfig.AutoscaleEnableVerticalPodAutoscaling.Get() {
		return nil
	}

	vpaName := controller.GetRevisionVpaName(rev)
	err := c.VPAClientSet.PocV1alpha1().VerticalPodAutoscalers(rev.Namespace).Delete(vpaName, fgDeleteOptions)
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		logger.Errorf("VPA delete for %q failed: %v", vpaName, err)
		return err
	}
	logger.Infof("Deleted VPA %q", vpaName)
	return nil
}

func (c *Controller) reconcileVpa(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	if !c.controllerConfig.AutoscaleEnableVerticalPodAutoscaling.Get() {
		return nil
	}

	vpaName := controller.GetRevisionVpaName(rev)
	_, err := c.vpaLister.VerticalPodAutoscalers(rev.Namespace).Get(vpaName)
	if err == nil {
		logger.Infof("Found exising VPA %q", vpaName)
		return nil
	}
	if !apierrs.IsNotFound(err) {
		logger.Errorf("VPA get for %q failed: %v", vpaName, err)
		return err
	}
	logger.Infof("VPA %q doesn't exist, creating", vpaName)

	controllerRef := controller.NewRevisionControllerRef(rev)
	vpaObj := MakeVpa(rev)
	vpaObj.OwnerReferences = append(vpaObj.OwnerReferences, *controllerRef)
	logger.Infof("Creating VPA: %q", vpaObj.Name)
	vs := c.VPAClientSet.PocV1alpha1().VerticalPodAutoscalers(rev.Namespace)
	_, err = vs.Create(vpaObj)
	return err
}

func (c *Controller) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
	prClient := c.ServingClientSet.ServingV1alpha1().Revisions(rev.Namespace)
	newRev, err := prClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(newRev.Status, rev.Status) {
		newRev.Status = rev.Status

		// TODO: for CRD there's no updatestatus, so use normal update
		return prClient.Update(newRev)
		//	return prClient.UpdateStatus(newRev)
	}
	return rev, nil
}

func (c *Controller) addConfigMapEvent(obj interface{}) {
	configMap := obj.(*corev1.ConfigMap)
	if configMap.Namespace != pkg.GetServingSystemNamespace() || configMap.Name != controller.GetNetworkConfigMapName() {
		return
	}

	c.Logger.Infof("Network config map is added or updated: %v", configMap)
	newNetworkConfig := NewNetworkConfigFromConfigMap(configMap)
	c.Logger.Infof("IstioOutboundIPRanges: %v", newNetworkConfig.IstioOutboundIPRanges)
	c.setNetworkConfig(newNetworkConfig)
}

func (c *Controller) updateConfigMapEvent(old, new interface{}) {
	c.addConfigMapEvent(new)
}

func (c *Controller) getNetworkConfig() *NetworkConfig {
	c.networkConfigMutex.Lock()
	defer c.networkConfigMutex.Unlock()
	return c.networkConfig
}

func (c *Controller) setNetworkConfig(cfg *NetworkConfig) {
	c.networkConfigMutex.Lock()
	defer c.networkConfigMutex.Unlock()
	c.networkConfig = cfg
}
