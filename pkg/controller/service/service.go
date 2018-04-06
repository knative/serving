/*
Copyright 2018 Google LLC

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

package service

import (
	"fmt"
	"reflect"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
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

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	clientset "github.com/elafros/elafros/pkg/client/clientset/versioned"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	listers "github.com/elafros/elafros/pkg/client/listers/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"
)

var (
	controllerKind          = v1alpha1.SchemeGroupVersion.WithKind("Service")
	serviceProcessItemCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "elafros",
		Name:      "service_process_item_count",
		Help:      "Counter to keep track of items in the service work queue",
	}, []string{"status"})
)

const (
	controllerAgentName = "service-controller"
)

// Controller implements the controller for Service resources.
// +controller:group=ela,version=v1alpha1,kind=Service,resource=services
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	elaclientset  clientset.Interface

	// lister indexes properties about Services
	lister listers.ServiceLister
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
	prometheus.MustRegister(serviceProcessItemCount)
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
func NewController(
	kubeclientset kubernetes.Interface,
	elaclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	elaInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig controller.Config) controller.Interface {

	glog.Infof("Service controller Init")

	// obtain references to a shared index informer for the Services.
	informer := elaInformerFactory.Elafros().V1alpha1().Services()

	// Create event broadcaster
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
		workqueue:     workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Services"),
		recorder:      recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when Service resources change
	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueService,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueService(new)
		},
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting Service controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.synced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch threadiness workers to process Service resources
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
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the updateServiceEvent.
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
		// Run the updateServiceEvent passing it the namespace/name string of the
		// Foo resource to be synced.
		if err := c.updateServiceEvent(key); err != nil {
			return fmt.Errorf("error syncing %q: %v", key, err), controller.PromLabelValueFailure
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced %q", key)
		return nil, controller.PromLabelValueSuccess
	}(obj)

	serviceProcessItemCount.With(prometheus.Labels{"status": promStatus}).Inc()

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// enqueueService takes a Service resource and
// converts it into a namespace/name string which is then put onto the work
// queue. This method should *not* be passed resources of any type other than
// Service.
//TODO(grantr): generic
func (c *Controller) enqueueService(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// updateServiceEvent compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Service resource
// with the current status of the resource.
func (c *Controller) updateServiceEvent(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Service resource with this namespace/name
	service, err := c.lister.Services(namespace).Get(name)

	// Don't modify the informers copy
	service = service.DeepCopy()

	if err != nil {
		// The resource may no longer exist, in which case we stop
		// processing.
		if apierrs.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("service %q in work queue no longer exists", key))
			return nil
		}

		return err
	}

	glog.Infof("Running reconcile Service for %s\n%+v\n", service.Name, service)

	config := MakeServiceConfiguration(service)
	if err = c.reconcileConfiguration(config); err != nil {
		return err
	}

	// TODO: If revision is specified, check that the revision is ready before
	// switching routes to it. Though route controller might just do the right thing?

	route := MakeServiceRoute(service, config.Name)
	return c.reconcileRoute(route)

	// TODO: update status appropriately.
}

func (c *Controller) updateStatus(service *v1alpha1.Service) (*v1alpha1.Service, error) {
	serviceClient := c.elaclientset.ElafrosV1alpha1().Services(service.Namespace)
	existing, err := serviceClient.Get(service.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(existing.Status, service.Status) {
		existing.Status = service.Status
		// TODO: for CRD there's no updatestatus, so use normal update.
		return serviceClient.Update(existing)
	}
	return existing, nil
}

func (c *Controller) reconcileConfiguration(config *v1alpha1.Configuration) error {
	configClient := c.elaclientset.ElafrosV1alpha1().Configurations(config.Namespace)

	existing, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			_, err := configClient.Create(config)
			return err
		}
		return err
	}
	// TODO(vaikas): Perhaps only update if there are actual changes.
	copy := existing.DeepCopy()
	copy.Spec = config.Spec
	_, err = configClient.Update(copy)
	return err
}

func (c *Controller) reconcileRoute(route *v1alpha1.Route) error {
	routeClient := c.elaclientset.ElafrosV1alpha1().Routes(route.Namespace)

	existing, err := routeClient.Get(route.Name, metav1.GetOptions{})
	if err != nil {
		if apierrs.IsNotFound(err) {
			_, err := routeClient.Create(route)
			return err
		}
		return err
	}
	// TODO(vaikas): Perhaps only update if there are actual changes.
	copy := existing.DeepCopy()
	copy.Spec = route.Spec
	_, err = routeClient.Update(copy)
	return err
}
