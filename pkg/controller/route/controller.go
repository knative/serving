/*
Copyright 2017 The Kubernetes Authors.

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

package route

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/google/elafros/pkg/client/clientset/versioned"
	elascheme "github.com/google/elafros/pkg/client/clientset/versioned/scheme"
	informers "github.com/google/elafros/pkg/client/informers/externalversions"
	listers "github.com/google/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/google/elafros/pkg/controller"
)

var (
	serviceKind           = v1alpha1.SchemeGroupVersion.WithKind("Route")
	routeProcessItemCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "elafros",
		Name:      "route_process_item_count",
		Help:      "Counter to keep track of items in the route work queue",
	}, []string{"status"})
)

const (
	controllerAgentName = "route-controller"

	// SuccessSynced is used as part of the Event 'reason' when a Foo is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Foo fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceSynced is the message used for an Event fired when a Foo
	// is synced successfully
	MessageResourceSynced = "Route synced successfully"
)

// RevisionRoute represents a single target to route to.
// Basically represents a k8s service representing a specific Revision
// and how much of the traffic goes to it.
type RevisionRoute struct {
	// Name for external routing. Optional
	Name string
	// RevisionName is the underlying revision that we're currently
	// routing to. Could be resolved from the Configuration or
	// specified explicitly in TrafficTarget
	RevisionName string
	// Service is the name of the k8s service we route to
	Service string
	Weight  int
}

// +controller:group=ela,version=v1alpha1,kind=Route,resource=routes
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	elaclientset  clientset.Interface

	// lister indexes properties about Configuration
	lister listers.RouteLister
	synced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

func init() {
	prometheus.MustRegister(routeProcessItemCount)
}

// Init initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// reconcileKey - function for mapping queue keys to resource names
//TODO(vaikas): somewhat generic (generic behavior)
func NewController(
	kubeclientset kubernetes.Interface,
	elaclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config) controller.Interface {

	glog.Infof("Route controller Init")

	// obtain a reference to a shared index informer for the Routes type.
	informer := elaInformerFactory.Elafros().V1alpha1().Routes()

	// Create event broadcaster
	// Add ela types to the default Kubernetes Scheme so Events can be
	// logged for ela types.
	elascheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset: kubeclientset,
		elaclientset:  elaclientset,
		lister:        informer.Lister(),
		synced:        informer.Informer().HasSynced,
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Routes"),
		recorder:      recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when Configuration resources change
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRoute,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueRoute(new)
		},
	})

	return controller

}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
//TODO(grantr): generic
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting Route controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process Foo resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
//TODO(grantr): generic
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
//TODO(grantr): generic
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err, promStatus := func(obj interface{}) (error, string) {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil, controller.PromLabelValueInvalid
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing %q: %s", key, err.Error()), controller.PromLabelValueFailure
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil, controller.PromLabelValueSuccess
	}(obj)

	routeProcessItemCount.With(prometheus.Labels{"status": promStatus}).Inc()

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// enqueueRoute takes a Route resource and
// converts it into a namespace/name string which is then put onto the work
// queue. This method should *not* be passed resources of any type other than
// Route.
//TODO(grantr): generic
func (c *Controller) enqueueRoute(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Foo resource
// with the current status of the resource.
//TODO(grantr): not generic
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Route resource with this namespace/name
	route, err := c.lister.Routes(namespace).Get(name)

	// Don't modify the informers copy
	route = route.DeepCopy()

	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("route '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	glog.Infof("Running reconcile Route for %s\n%+v\n", route.Name, route)

	// Create a placeholder service that is simply used by istio as a placeholder.
	// This service could eventually be the 'router' service that will get all the
	// fallthrough traffic if there are no route rules (revisions to target).
	// This is one way to implement the 0->1. For now, we'll just create a placeholder
	// that selects nothing.
	glog.Infof("Creating/Updating placeholder k8s services")
	err = c.createPlaceholderService(route, namespace)
	if err != nil {
		return err
	}

	// Then create the Ingress rule for this service
	glog.Infof("Creating or updating ingress rule")
	err = c.createOrUpdateIngress(route, namespace)
	if err != nil {
		if !apierrs.IsAlreadyExists(err) {
			glog.Infof("Failed to create ingress rule: %s", err)
			return err
		}
	}

	// Then create the actual route rules.
	glog.Infof("Creating istio route rules")
	routes, err := c.createOrUpdateRoutes(route, namespace)
	if err != nil {
		glog.Infof("Failed to create Routes: %s", err)
		return err
	}

	// If routes were configured, update them
	if routes != nil {
		traffic := []v1alpha1.TrafficTarget{}
		for _, r := range routes {
			traffic = append(traffic, v1alpha1.TrafficTarget{
				Name:     r.Name,
				Revision: r.RevisionName,
				Percent:  r.Weight,
			})
		}
		route.Status.Traffic = traffic
	}
	updated, err := c.updateStatus(route)
	if err != nil {
		glog.Warningf("Failed to update service status: %s", err)
		return err
	}

	c.recorder.Event(updated, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

func (c *Controller) createPlaceholderService(u *v1alpha1.Route, ns string) error {
	service := MakeRouteK8SService(u)
	serviceRef := metav1.NewControllerRef(u, serviceKind)
	service.OwnerReferences = append(service.OwnerReferences, *serviceRef)

	sc := c.kubeclientset.Core().Services(ns)
	_, err := sc.Create(service)
	if err != nil {
		if !apierrs.IsAlreadyExists(err) {
			glog.Infof("Failed to create service: %s", err)
			return err
		}
	}
	glog.Infof("Created service: %q", service.Name)
	return nil
}

func (c *Controller) createOrUpdateIngress(route *v1alpha1.Route, ns string) error {
	ingressName := controller.GetElaK8SIngressName(route)

	ic := c.kubeclientset.Extensions().Ingresses(ns)

	// Check to see if we need to create or update
	ingress := MakeRouteIngress(route, ns)
	serviceRef := metav1.NewControllerRef(route, serviceKind)
	ingress.OwnerReferences = append(ingress.OwnerReferences, *serviceRef)

	_, err := ic.Get(ingressName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
		_, createErr := ic.Create(ingress)
		glog.Infof("Created ingress %q", ingress.Name)
		return createErr
	}
	return nil
}

func (c *Controller) getRoutes(u *v1alpha1.Route) ([]RevisionRoute, error) {
	glog.Infof("Figuring out routes for Route: %s", u.Name)
	ret := []RevisionRoute{}
	for _, tt := range u.Spec.Traffic {
		rr, err := c.getRouteForTrafficTarget(tt, u.Namespace)
		if err != nil {
			glog.Infof("Failed to get a route for target %+v : %q", tt, err)
			return nil, err
		}
		ret = append(ret, rr)
	}
	return ret, nil
}

func (c *Controller) getRouteForTrafficTarget(tt v1alpha1.TrafficTarget, ns string) (RevisionRoute, error) {
	elaNS := controller.GetElaNamespaceName(ns)
	// If template specified, fetch last revision otherwise use Revision
	revisionName := tt.Revision
	if tt.Configuration != "" {
		configClient := c.elaclientset.ElafrosV1alpha1().Configurations(ns)
		config, err := configClient.Get(tt.Configuration, metav1.GetOptions{})
		if err != nil {
			return RevisionRoute{}, err
		}
		revisionName = config.Status.LatestReady
	}
	prClient := c.elaclientset.ElafrosV1alpha1().Revisions(ns)
	rev, err := prClient.Get(revisionName, metav1.GetOptions{})
	if err != nil {
		glog.Infof("Failed to fetch Revision: %s : %s", revisionName, err)
		return RevisionRoute{}, err
	}
	return RevisionRoute{
		Name:         tt.Name,
		RevisionName: rev.Name,
		Service:      fmt.Sprintf("%s.%s", rev.Status.ServiceName, elaNS),
		Weight:       tt.Percent,
	}, nil
}

func (c *Controller) createOrUpdateRoutes(u *v1alpha1.Route, ns string) ([]RevisionRoute, error) {
	// grab a client that's specific to RouteRule.
	routeClient := c.elaclientset.ConfigV1alpha2().RouteRules(ns)
	if routeClient == nil {
		glog.Infof("Failed to create resource client")
		return nil, fmt.Errorf("Couldn't get a routeClient")
	}

	routes, err := c.getRoutes(u)
	if err != nil {
		glog.Infof("Failed to get routes for %s : %q", u.Name, err)
		return nil, err
	}
	if len(routes) == 0 {
		glog.Infof("No routes were found for the service %q", u.Name)
		return nil, nil
	}
	for _, r := range routes {
		glog.Infof("Adding a route to %q Weight: %d", r.Service, r.Weight)
	}

	routeRuleName := controller.GetElaIstioRouteRuleName(u)
	routeRules, err := routeClient.Get(routeRuleName, metav1.GetOptions{})
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return nil, err
		}
		routeRules = MakeRouteIstioRoutes(u, ns, routes)
		_, createErr := routeClient.Create(routeRules)
		return nil, createErr
	}

	routeRules.Spec = MakeRouteIstioSpec(u, ns, routes)
	_, err = routeClient.Update(routeRules)
	if err != nil {
		return nil, err
	}
	return routes, nil
}

func (c *Controller) updateStatus(u *v1alpha1.Route) (*v1alpha1.Route, error) {
	routeClient := c.elaclientset.ElafrosV1alpha1().Routes(u.Namespace)
	newu, err := routeClient.Get(u.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newu.Status = u.Status

	// TODO: for CRD there's no updatestatus, so use normal update
	return routeClient.Update(newu)
	//	return routeClient.UpdateStatus(newu)
}
