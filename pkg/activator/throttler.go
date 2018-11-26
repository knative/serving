package activator

import (
	"github.com/knative/serving/pkg/queue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"sync"
	"fmt"
	"net/http"
	"github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	v1alpha12 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"k8s.io/client-go/informers/core/v1"
	"time"
)

func NewThrottler(queueDepth int32, maxConcurrency int32, rev RevisionID, revisionInformer v1alpha1.RevisionInformer, endpointInformer v1.EndpointsInformer) *Throttler {
	breaker := queue.NewBreaker(queueDepth, maxConcurrency, true)
	tickChan := make(chan struct{})
	stopChan := make(chan struct{})
	return &Throttler{queueDepth: queueDepth, maxConcurrency: maxConcurrency, Breaker: breaker, revisionID: rev, revisionInformer: revisionInformer, endpointsInformer: endpointInformer, tickChan: tickChan, stopChan: stopChan}
}

type Throttler struct {
	revisionID        RevisionID
	Breaker           *queue.Breaker
	revisionInformer  v1alpha1.RevisionInformer
	endpointsInformer v1.EndpointsInformer
	queueDepth        int32
	maxConcurrency    int32
	endpoints         int32
	tickChan          chan struct{}
	stopChan          chan struct{}
	mux               sync.Mutex
}

func ServiceName(name string) string {
	return name + "-service"
}

func (t *Throttler) Tick(interval time.Duration) error {
	for {
		t.tickChan <- struct{}{}
		time.Sleep(interval)
	}
}

func (t *Throttler) CheckEndpoints(rev RevisionID) {
	for {
		select {
		case <-t.tickChan:
			t.checkAndUpdate(rev)
		case <-t.stopChan:
			return
		}
	}
}

func (t *Throttler) checkAndUpdate(rev RevisionID) {
	defer t.mux.Unlock()
	t.mux.Lock()
	key := cache.ExplicitKey(rev.Namespace + "/" + rev.Name)
	e, exists, err := t.endpointsInformer.Informer().GetIndexer().Get(key)
	if !exists {
		fmt.Println("No endpoint exist yet for the revision: " + key)
		return
	}
	if err != nil {
		fmt.Println(err)
		return
	}
	endpoints := e.(*corev1.Endpoints)
	addresses := getEndpointAddresses(&endpoints.Subsets)
	delta := int32(addresses) - t.endpoints
	if delta != 0 {
		t.updateBreaker(rev, delta)
		t.endpoints += delta
	}
}

// TODO: Rename the method
func (t *Throttler) ActiveEndpoint(namespace, name string) ActivationResult {
	var err error
	// TODO: update the generic function for this
	serviceName := ServiceName(name)

	fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)

	revisionKey := cache.ExplicitKey(namespace + "/" + name)
	rev, exists, err := t.revisionInformer.Informer().GetIndexer().Get(revisionKey)
	if !exists {
		fmt.Println("Revision doesn't exist yet")
	}
	if err != nil {
		fmt.Println(err)
	}

	revision := rev.(*v1alpha12.Revision)

	serviceName, configurationName := getServiceAndConfigurationLabels(revision)

	// TODO: use the service client to get the port
	port := int32(80)

	if err != nil {
		fmt.Println(err)
	}
	endpoint := Endpoint{FQDN: fqdn, Port: port}

	return ActivationResult{
		Status:            http.StatusOK,
		Endpoint:          endpoint,
		ServiceName:       serviceName,
		ConfigurationName: configurationName,
		Error:             err,
	}
}

// depending on whether the delta is positive or negative - Release or Acquire the Lock
func (t *Throttler) updateBreaker(rev RevisionID, diff int32) {
	if diff > 0 {
		t.Breaker.Sem.Put(diff)
	} else {
		t.Breaker.Sem.Get()
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

func getServiceAndConfigurationLabels(rev *v1alpha12.Revision) (string, string) {
	if rev.Labels == nil {
		return "", ""
	}
	return rev.Labels[serving.ServiceLabelKey], rev.Labels[serving.ConfigurationLabelKey]
}
