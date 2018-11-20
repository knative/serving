package kbuffer

import (
	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
	"sync"
	"time"

	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"strings"
)

func NewEndpointObserver() *EndpointObserver {
	endpoints := make(map[revisionID]int)
	stopChan := make(chan struct{})
	defaultResync := 100 * time.Millisecond

	return &EndpointObserver{endpoints: endpoints, stopChan: stopChan, defaultResync: defaultResync}
}

type EndpointObserver struct {
	endpoints     map[revisionID]int
	informer      v1.EndpointsInformer
	defaultResync time.Duration
	stopChan      chan struct{}
	mux           sync.Mutex
}

func getEndpointAddresses(subsets *[]corev1.EndpointSubset) int {
	var total int
	subsetLength := len(*subsets)
	if subsetLength == 0 {
		return 0
	}
	for s := 0; s < subsetLength; s++ {
		addresses := (*subsets)[s].Addresses
		total += len(addresses)
	}
	return total
}

func (eo *EndpointObserver) update(oldObj, newObj interface{}) {

	old := oldObj.(*corev1.Endpoints)
	new := newObj.(*corev1.Endpoints)

	if len(old.Subsets) != 0 || len(new.Subsets) != 0 {
		oldAddresses := getEndpointAddresses(&old.Subsets)
		newAddresses := getEndpointAddresses(&new.Subsets)
		if oldAddresses != newAddresses {
			defer eo.mux.Unlock()
			eo.mux.Lock()
			eo.endpoints[revisionID{new.Namespace, new.Name}] = newAddresses
		}
	}
}

// noop
func (eo *EndpointObserver) add(obj interface{}) {
	return
}

func (eo *EndpointObserver) filter(namespace, name string) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		endpoints := obj.(*corev1.Endpoints)
		return endpoints.Namespace == namespace && endpoints.Name == name
	}
}

// Start the endpoint informer
func (eo *EndpointObserver) Start(clientset kubernetes.Interface) *kubeinformers.SharedInformerFactory {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(clientset, eo.defaultResync)
	eo.informer = kubeInformerFactory.Core().V1().Endpoints()

	go eo.informer.Informer().Run(eo.stopChan)
	kubeInformerFactory.Start(eo.stopChan)
	return &kubeInformerFactory
}

// Get the number of endpoints for a revision
// _ok_ is true if the endpoint exists in the map
func (eo *EndpointObserver) Get(id revisionID, postfix ...string) (int, bool) {
	name := id.name+strings.Join(postfix,"")
	id.name = name
	eo.mux.Lock()
	defer eo.mux.Unlock()
	hosts, ok := eo.endpoints[id]
	return hosts, ok
}

// Add handler function to watch a specific revision
// Is not blocking, the updates are received via polling the _Get_ method
func (eo *EndpointObserver) WatchEndpoint(id revisionID, postfix ...string) {
	name := id.name+strings.Join(postfix,"")
	eo.informer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: eo.filter(id.namespace, name),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    eo.add,
			UpdateFunc: eo.update,
		},
	})
}
