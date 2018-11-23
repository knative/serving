package activator

import (
	"github.com/knative/serving/pkg/queue"
	"k8s.io/client-go/informers/core/v1"
	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"time"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"sync"
	"fmt"
)

func NewThrottler(queueDepth int32, maxConcurrency int32) *Throttler {
	stopChan := make(chan struct{})
	defaultResync := 100 * time.Millisecond
	breakers := make(map[revisionID]*queue.Breaker)
	return &Throttler{stopChan: stopChan, defaultResync: defaultResync, queueDepth: queueDepth, maxConcurrency: maxConcurrency, breakers: breakers}
}

type Throttler struct {
	breakers       map[revisionID]*queue.Breaker
	informer       v1.EndpointsInformer
	defaultResync  time.Duration
	queueDepth     int32
	maxConcurrency int32
	stopChan       chan struct{}
	mux            sync.Mutex
}

func (t *Throttler) Start(clientset kubernetes.Interface) *kubeinformers.SharedInformerFactory {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(clientset, t.defaultResync)
	t.informer = kubeInformerFactory.Core().V1().Endpoints()

	go t.informer.Informer().Run(t.stopChan)
	kubeInformerFactory.Start(t.stopChan)
	return &kubeInformerFactory
}

func (t *Throttler) WatchEndpoints() {
	t.informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: t.update,
	})
}

func (t *Throttler) update(oldObj, newObj interface{}) {
	old := oldObj.(*corev1.Endpoints)
	new := newObj.(*corev1.Endpoints)
	if len(old.Subsets) != 0 || len(new.Subsets) != 0 {
		oldAddresses := getEndpointAddresses(&old.Subsets)
		newAddresses := getEndpointAddresses(&new.Subsets)
		if oldAddresses != newAddresses {
			fmt.Printf("old: %d, new: %d\n", oldAddresses, newAddresses)
			diff := newAddresses - oldAddresses
			rev := revisionID{name: new.Name, namespace: new.Namespace}
			t.updateBreaker(rev, diff)
		}
	}
}

// Put the tokens to the semaphore of a corresponding breaker
func (t *Throttler) updateBreaker(rev revisionID, diff int) {
	defer t.mux.Unlock()
	t.mux.Lock()
	if breaker, ok := t.breakers[rev]; ok {
		if diff > 0 {
			breaker.Sem.Put(diff)
		} else {
			breaker.Sem.Get()
		}
	}
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
