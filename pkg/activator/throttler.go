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
)

func NewThrottler(queueDepth int32, maxConcurrency int32, rev RevisionID, revisionInformer v1alpha1.RevisionInformer) *Throttler {
	breaker := queue.NewBreaker(queueDepth, maxConcurrency, true)
	return &Throttler{queueDepth: queueDepth, maxConcurrency: maxConcurrency, Breaker: breaker, revisionID: rev, revisionInformer: revisionInformer}
}

type Throttler struct {
	revisionID       RevisionID
	Breaker          *queue.Breaker
	revisionInformer v1alpha1.RevisionInformer
	queueDepth       int32
	maxConcurrency   int32
	mux              sync.Mutex
}

func ServiceName(name string) string {
	return name + "-service"
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

func (t *Throttler) Update(oldObj, newObj interface{}) {
	old := oldObj.(*corev1.Endpoints)
	new := newObj.(*corev1.Endpoints)
	rev := RevisionID{Name: new.Name, Namespace: new.Namespace}
	fmt.Println(old)
	fmt.Println(new)
	if rev.Namespace == t.revisionID.Namespace && rev.Name == t.revisionID.Name {
		if len(old.Subsets) != 0 || len(new.Subsets) != 0 {
			oldAddresses := getEndpointAddresses(&old.Subsets)
			newAddresses := getEndpointAddresses(&new.Subsets)
			if oldAddresses != newAddresses {
				fmt.Printf("old: %d, new: %d\n", oldAddresses, newAddresses)
				diff := newAddresses - oldAddresses
				t.updateBreaker(rev, diff)
			}
		}
	}
}

// depending on whether the delta is positive or negative - Release or Acquire the Lock
func (t *Throttler) updateBreaker(rev RevisionID, diff int) {
	defer t.mux.Unlock()
	t.mux.Lock()
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
