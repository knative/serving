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

package revision

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/knative/serving/pkg"

	"github.com/josephburnett/k8sflag/pkg/k8sflag"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"

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
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildlisters "github.com/knative/build/pkg/client/listers/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
)

const (
	istioInjectionLabel string = "istio-injection"

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
	revisionLister listers.RevisionLister
	buildLister    buildlisters.BuildLister

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
	vpaClient vpa.Interface,
	revisionInformer servinginformers.RevisionInformer,
	buildInformer buildinformers.BuildInformer,
	configMapInformer corev1informers.ConfigMapInformer,
	deploymentInformer appsv1informers.DeploymentInformer,
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
		vpaClient:        vpaClient,
		revisionLister:   revisionInformer.Lister(),
		buildLister:      buildInformer.Lister(),
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
		AddFunc: func(obj interface{}) {
			c.SyncEndpoints(obj.(*corev1.Endpoints))
		},
		UpdateFunc: func(old, new interface{}) {
			c.SyncEndpoints(new.(*corev1.Endpoints))
		},
	})

	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.SyncDeployment(obj.(*appsv1.Deployment))
		},
		UpdateFunc: func(old, new interface{}) {
			c.SyncDeployment(new.(*appsv1.Deployment))
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
	rev, err := c.revisionLister.Revisions(namespace).Get(name)
	// The resource may no longer exist, in which case we stop processing.
	if apierrs.IsNotFound(err) {
		runtime.HandleError(fmt.Errorf("revision %q in work queue no longer exists", key))
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()
	rev.Status.InitializeConditions()
	c.updateRevisionLoggingURL(rev)

	if rev.Spec.BuildName != "" {
		rev.Status.InitializeBuildCondition()
		build, err := c.buildLister.Builds(rev.Namespace).Get(rev.Spec.BuildName)
		if err != nil {
			logger.Errorf("Error fetching Build %q for Revision %q: %v", rev.Spec.BuildName, key, err)
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

	// TODO(mattmoor): Remove this comment.
	// In the level-based reconciliation we've started moving to in #1208, this should be the last thing we do.
	// This controller is substantial enough that we will slowly move pieces above this line until it is the
	// last thing, and then remove this comment.
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error updating Revision status", zap.Error(err))
		return err
	}

	bc := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	if bc == nil || bc.Status == corev1.ConditionTrue {
		// There is no build, or the build completed successfully.

		switch rev.Spec.ServingState {
		case v1alpha1.RevisionServingStateActive:
			logger.Info("Creating or reconciling resources for revision")
			err := c.checkNamespaceHasIstioInjectionLabel(rev.Namespace)
			if err != nil {
				rev.Status.MarkNetworkProxyMissing()
				if _, err := c.updateStatus(rev); err != nil {
					logger.Error("Error recording inactivation of revision for: ", zap.Error(err))
					return err
				}
				return apierrs.NewBadRequest("Missing Istio Proxy")
			}
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

func (c *Controller) SyncDeployment(deployment *appsv1.Deployment) {
	cond := getDeploymentProgressCondition(deployment)
	if cond == nil {
		return
	}

	or := metav1.GetControllerOf(deployment)
	if or == nil || or.Kind != "Revision" {
		return
	}

	// Get the handle of Revision in context
	revName := or.Name
	namespace := deployment.Namespace
	logger := loggerWithRevisionInfo(c.Logger, namespace, revName)

	rev, err := c.revisionLister.Revisions(namespace).Get(revName)
	if err != nil {
		logger.Error("Error fetching revision", zap.Error(err))
		return
	}
	//Set the revision condition reason to ProgressDeadlineExceeded
	rev.Status.MarkProgressDeadlineExceeded(
		fmt.Sprintf("Unable to create pods for more than %d seconds.", progressDeadlineSeconds))

	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error recording revision completion", zap.Error(err))
		return
	}
	c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded", "Revision %s not ready due to Deployment timeout", revName)
	return
}

func (c *Controller) SyncEndpoints(endpoint *corev1.Endpoints) {
	eName := endpoint.Name
	namespace := endpoint.Namespace
	// Lookup and see if this endpoints corresponds to a service that
	// we own and hence the Revision that created this service.
	revName := lookupServiceOwner(endpoint)
	if revName == "" {
		return
	}
	logger := loggerWithRevisionInfo(c.Logger, namespace, revName)

	rev, err := c.revisionLister.Revisions(namespace).Get(revName)
	if err != nil {
		logger.Error("Error fetching revision", zap.Error(err))
		return
	}

	// Check to see if endpoint is the service endpoint
	if eName != controller.GetServingK8SServiceNameForRevision(rev) {
		return
	}

	// Check to see if the revision has already been marked as ready or failed
	// and if it is, then there's no need to do anything to it.
	if c := rev.Status.GetCondition(v1alpha1.RevisionConditionReady); c != nil && c.Status != corev1.ConditionUnknown {
		return
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	if getIsServiceReady(endpoint) {
		logger.Infof("Endpoint %q is ready", eName)
		rev.Status.MarkResourcesAvailable()
		rev.Status.MarkContainerHealthy()
		log.Printf("UPDATING STATUS TO: %v", rev.Status.Conditions)
		if _, err := c.updateStatus(rev); err != nil {
			logger.Error("Error marking revision ready", zap.Error(err))
			return
		}
		c.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon endpoint %q becoming ready", endpoint.Name)
		return
	}

	revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
	if revisionAge < serviceTimeoutDuration {
		return
	}

	rev.Status.MarkServiceTimeout()
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error marking revision failed", zap.Error(err))
		return
	}
	c.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed", "Revision did not become ready due to endpoint %q", endpoint.Name)
	return
}

func (c *Controller) checkNamespaceHasIstioInjectionLabel(namespace string) error {
	ns, err := c.KubeClientSet.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("error fetching namespace: %s", err)
	}

	val, ok := ns.GetLabels()[istioInjectionLabel]

	if !ok {
		return errors.New("Missing istio injection label")
	}

	if val != "enabled" {
		return errors.New("Expected istio injection label value to be 'enabled'")
	}
	return nil
}

func (c *Controller) deleteK8SResources(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	logger.Info("Deleting the resources for revision")
	err := c.deleteDeployment(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete a deployment", zap.Error(err))
	}
	logger.Info("Deleted deployment")

	err = c.deleteAutoscalerDeployment(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete autoscaler Deployment", zap.Error(err))
	}
	logger.Info("Deleted autoscaler Deployment")

	err = c.deleteAutoscalerService(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete autoscaler Service", zap.Error(err))
	}
	logger.Info("Deleted autoscaler Service")

	if c.controllerConfig.AutoscaleEnableVerticalPodAutoscaling.Get() {
		if err := c.deleteVpa(ctx, rev); err != nil {
			logger.Error("Failed to delete VPA", zap.Error(err))
		}
		logger.Info("Deleted VPA")
	}

	err = c.deleteService(ctx, rev)
	if err != nil {
		logger.Error("Failed to delete k8s service", zap.Error(err))
	}
	logger.Info("Deleted service")

	// And the deployment is no longer ready, so update that
	rev.Status.MarkInactive()
	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error recording inactivation of revision", zap.Error(err))
		return err
	}

	return nil
}

func (c *Controller) createK8SResources(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	// Fire off a Deployment..
	if err := c.reconcileDeployment(ctx, rev); err != nil {
		logger.Error("Failed to create a deployment", zap.Error(err))
		return err
	}

	// Autoscale the service
	if err := c.reconcileAutoscalerDeployment(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Deployment", zap.Error(err))
	}
	if err := c.reconcileAutoscalerService(ctx, rev); err != nil {
		logger.Error("Failed to create autoscaler Service", zap.Error(err))
	}
	if c.controllerConfig.EnableVarLogCollection {
		if err := c.reconcileFluentdConfigMap(ctx, rev); err != nil {
			logger.Error("Failed to create fluent config map", zap.Error(err))
		}
	}

	// Vertically autoscale the revision pods
	if c.controllerConfig.AutoscaleEnableVerticalPodAutoscaling.Get() {
		if err := c.reconcileVpa(ctx, rev); err != nil {
			logger.Error("Failed to create the vertical pod autoscaler for Deployment", zap.Error(err))
		}
	}

	// Create k8s service
	serviceName, err := c.reconcileService(ctx, rev)
	if err != nil {
		logger.Error("Failed to create k8s service", zap.Error(err))
	} else {
		rev.Status.ServiceName = serviceName
	}

	// Check to see if the revision has already been marked as ready and
	// don't mark it if it's already ready.
	// TODO: could always fetch the endpoint again and double-check it is still
	// ready.
	if rev.Status.IsReady() {
		return nil
	}

	// Before toggling the status to Deploying, fetch the latest state of the revision.
	latestRev, err := c.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
	if err != nil {
		logger.Error("Error refetching revision", zap.Error(err))
		return err
	}
	if latestRev.Status.IsReady() {
		return nil
	}

	// Checking existing revision condition to see if it is the initial deployment or
	// during the reactivating process. If a revision is in condition "Inactive" or "Activating",
	// we need to route traffic to the activator; if a revision is in condition "Deploying",
	// we need to route traffic to the revision directly.
	reason := "Deploying"
	if cond := rev.Status.GetCondition(v1alpha1.RevisionConditionReady); cond != nil {
		if (cond.Reason == "Inactive" && cond.Status == corev1.ConditionFalse) ||
			(cond.Reason == "Activating" && cond.Status == corev1.ConditionUnknown) {
			reason = "Activating"
		}
	}
	rev.Status.MarkDeploying(reason)

	// By updating our deployment status we will trigger a Reconcile()
	// that will watch for service to become ready for serving traffic.
	logger.Infof("Updating status with the following conditions %+v", rev.Status.Conditions)
	if _, err := c.updateStatus(rev); err != nil {
		logger.Error("Error recording build completion", zap.Error(err))
		return err
	}

	return nil
}

func (c *Controller) deleteDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	deploymentName := controller.GetRevisionDeploymentName(rev)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}

	logger.Infof("Deleting Deployment %q", deploymentName)
	tmp := metav1.DeletePropagationForeground
	err := dc.Delete(deploymentName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("deployments.Delete for %q failed: %s", deploymentName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	// First, check if deployment exists already.
	deploymentName := controller.GetRevisionDeploymentName(rev)

	if _, err := dc.Get(deploymentName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("deployments.Get for %q failed: %s", deploymentName, err)
			return err
		}
		logger.Infof("Deployment %q doesn't exist, creating", deploymentName)
	} else {
		// TODO(mattmoor): Compare the deployments and update if it has changed
		// out from under us.
		logger.Infof("Found existing deployment %q", deploymentName)
		return nil
	}

	// Create the deployment.
	deployment := MakeServingDeployment(logger, rev, c.getNetworkConfig(), c.controllerConfig)

	// Resolve tag image references to digests.
	if err := c.resolver.Resolve(deployment); err != nil {
		logger.Error("Error resolving deployment", zap.Error(err))
		rev.Status.MarkContainerMissing(err.Error())
		if _, err := c.updateStatus(rev); err != nil {
			logger.Error("Error recording resolution problem", zap.Error(err))
			return err
		}
		return err
	}

	logger.Infof("Creating Deployment: %q", deployment.Name)
	_, createErr := dc.Create(deployment)

	return createErr
}

func (c *Controller) deleteService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	sc := c.KubeClientSet.CoreV1().Services(ns)
	serviceName := controller.GetServingK8SServiceNameForRevision(rev)

	logger.Infof("Deleting service %q", serviceName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(serviceName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("service.Delete for %q failed: %s", serviceName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileService(ctx context.Context, rev *v1alpha1.Revision) (string, error) {
	logger := logging.FromContext(ctx)
	ns := controller.GetServingNamespaceName(rev.Namespace)
	sc := c.KubeClientSet.CoreV1().Services(ns)
	serviceName := controller.GetServingK8SServiceNameForRevision(rev)

	if _, err := sc.Get(serviceName, metav1.GetOptions{}); err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("services.Get for %q failed: %s", serviceName, err)
			return "", err
		}
		logger.Infof("serviceName %q doesn't exist, creating", serviceName)
	} else {
		// TODO(vaikas): Check that the service is legit and matches what we expect
		// to have there.
		logger.Infof("Found existing service %q", serviceName)
		return serviceName, nil
	}

	service := MakeRevisionK8sService(rev)
	logger.Infof("Creating service: %q", service.Name)
	_, err := sc.Create(service)
	return serviceName, err
}

func (c *Controller) reconcileFluentdConfigMap(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
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
	sc := c.KubeClientSet.CoreV1().Services(ns)
	if _, err := sc.Get(autoscalerName, metav1.GetOptions{}); err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	logger.Infof("Deleting autoscaler Service %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err := sc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Service delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()
	sc := c.KubeClientSet.CoreV1().Services(ns)
	_, err := sc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("Autoscaler Service get for %q failed: %s", autoscalerName, err)
			return err
		}
		logger.Infof("Autoscaler Service %q doesn't exist, creating", autoscalerName)
	} else {
		logger.Infof("Found existing autoscaler Service %q", autoscalerName)
		return nil
	}

	service := MakeServingAutoscalerService(rev)
	logger.Infof("Creating autoscaler Service: %q", service.Name)
	_, err = sc.Create(service)
	return err
}

func (c *Controller) deleteAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	logger.Infof("Deleting autoscaler Deployment %q", autoscalerName)
	tmp := metav1.DeletePropagationForeground
	err = dc.Delete(autoscalerName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("Autoscaler Deployment delete for %q failed: %s", autoscalerName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	autoscalerName := controller.GetRevisionAutoscalerName(rev)
	ns := pkg.GetServingSystemNamespace()
	dc := c.KubeClientSet.AppsV1().Deployments(ns)
	_, err := dc.Get(autoscalerName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("Autoscaler Deployment get for %q failed: %s", autoscalerName, err)
			return err
		}
		logger.Infof("Autoscaler Deployment %q doesn't exist, creating", autoscalerName)
	} else {
		logger.Infof("Found existing autoscaler Deployment %q", autoscalerName)
		return nil
	}

	deployment := MakeServingAutoscalerDeployment(rev, c.controllerConfig.AutoscalerImage)
	logger.Infof("Creating autoscaler Deployment: %q", deployment.Name)
	_, err = dc.Create(deployment)
	return err
}

func (c *Controller) deleteVpa(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	vpaName := controller.GetRevisionVpaName(rev)
	vs := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(rev.Namespace)
	_, err := vs.Get(vpaName, metav1.GetOptions{})
	if err != nil && apierrs.IsNotFound(err) {
		return nil
	}
	logger.Infof("Deleting VPA %q", vpaName)
	tmp := metav1.DeletePropagationForeground
	err = vs.Delete(vpaName, &metav1.DeleteOptions{
		PropagationPolicy: &tmp,
	})
	if err != nil && !apierrs.IsNotFound(err) {
		logger.Errorf("VPA delete for %q failed: %v", vpaName, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileVpa(ctx context.Context, rev *v1alpha1.Revision) error {
	logger := logging.FromContext(ctx)
	vpaName := controller.GetRevisionVpaName(rev)
	vs := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(rev.Namespace)
	_, err := vs.Get(vpaName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			logger.Errorf("VPA get for %q failed: %v", vpaName, err)
			return err
		}
		logger.Infof("VPA %q doesn't exist, creating", vpaName)
	} else {
		logger.Info("Found exising VPA %q", vpaName)
		return nil
	}

	controllerRef := controller.NewRevisionControllerRef(rev)
	vpaObj := MakeVpa(rev)
	vpaObj.OwnerReferences = append(vpaObj.OwnerReferences, *controllerRef)
	logger.Infof("Creating VPA: %q", vpaObj.Name)
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

// Given an endpoint see if it's managed by us and return the
// revision that created it.
// TODO: Consider using OwnerReferences.
// https://github.com/kubernetes/sample-controller/blob/master/controller.go#L373-L384
func lookupServiceOwner(endpoint *corev1.Endpoints) string {
	// see if there's a label on this object marking it as ours.
	if revisionName, ok := endpoint.Labels[serving.RevisionLabelKey]; ok {
		return revisionName
	}
	return ""
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
