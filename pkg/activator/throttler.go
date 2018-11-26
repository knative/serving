package activator

import (
	"github.com/knative/serving/pkg/queue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"sync"
	"fmt"
	"net/http"
	"github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	v1alpha12 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"k8s.io/client-go/informers/core/v1"
	"time"
	"go.uber.org/zap"
	"errors"
)

func NewThrottler(queueDepth int32, maxConcurrency int32, rev RevisionID, revisionInformer v1alpha1.RevisionInformer, endpointInformer v1.EndpointsInformer, logger *zap.SugaredLogger) *Throttler {
	breaker := queue.NewBreaker(queueDepth, maxConcurrency, true)
	tickChan := make(chan struct{})
	stopChan := make(chan struct{})
	return &Throttler{queueDepth: queueDepth, maxConcurrency: maxConcurrency, Breaker: breaker, revisionID: rev, revisionInformer: revisionInformer, endpointsInformer: endpointInformer, tickChan: tickChan, stopChan: stopChan, logger: logger}
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
	logger            *zap.SugaredLogger
	mux               sync.Mutex
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

// TODO: Rename the method
func (t *Throttler) ActiveEndpoint(namespace, name string) ActivationResult {
	var err error
	serviceName := reconciler.GetServingK8SServiceNameForObj(name)

	fqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
	revision, err := GetRevision(namespace, name, t.revisionInformer)
	if err != nil {
		t.logger.Error(err)
	}
	serviceName, configurationName := GetServiceAndConfigurationLabels(revision)

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

func (t *Throttler) checkAndUpdate(rev RevisionID) {
	defer t.mux.Unlock()
	t.mux.Lock()
	key := cache.ExplicitKey(rev.Namespace + "/" + rev.Name)
	e, exists, err := t.endpointsInformer.Informer().GetIndexer().Get(key)
	if !exists {
		t.logger.Errorf("No endpoint exist yet for the revision: %s", key)
		return
	}
	if err != nil {
		t.logger.Error(err)
		return
	}
	endpoints := e.(*corev1.Endpoints)
	addresses := t.getEndpointAddresses(&endpoints.Subsets)
	delta := int32(addresses) - t.endpoints
	if delta != 0 {
		t.updateBreaker(rev, delta)
		t.endpoints += delta
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

func (t *Throttler) getEndpointAddresses(subsets *[]corev1.EndpointSubset) int {
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

func GetServiceAndConfigurationLabels(rev *v1alpha12.Revision) (string, string) {
	if rev.Labels == nil {
		return "", ""
	}
	return rev.Labels[serving.ServiceLabelKey], rev.Labels[serving.ConfigurationLabelKey]
}

func GetRevision(namespace, name string, informer v1alpha1.RevisionInformer) (*v1alpha12.Revision, error) {
	var revision *v1alpha12.Revision
	revisionKey := cache.ExplicitKey(namespace + "/" + name)
	rev, exist, err := informer.Informer().GetIndexer().Get(revisionKey)
	if !exist {
		error := errors.New("Revision doesn't exist yet")
		return revision, errors.New(error.Error())
	}
	if err != nil {
		return revision, err
	}
	revision = rev.(*v1alpha12.Revision)
	return revision, nil
}
