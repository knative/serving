/*
Copyright 2019 The Knative Authors

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

package activator

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
)

// RevisionDestsUpdate contains the state of healthy l4 dests for talking to a revision and is the
// primary output from the RevisionBackendsManager system. If a healthy ClusterIP is found then
// ClusterIPDest will be set to non empty string and Dests will be nil. Otherwise Dests will be set
// to a slice of healthy l4 dests for reaching the revision.
type RevisionDestsUpdate struct {
	Rev           types.NamespacedName
	ClusterIPDest string
	Dests         []string

	// Number of addresses marked ready in the private endpoint for this revision
	ReadyAddressCount int
}

const (
	probeTimeout   time.Duration = 300 * time.Millisecond
	probeFrequency time.Duration = 200 * time.Millisecond
)

// revisionWatcher watches the podIPs and ClusterIP of the service for a revision. It implements the logic
// to supply RevisionDestsUpdate events on updateCh
type revisionWatcher struct {
	rev      types.NamespacedName
	protocol networking.ProtocolType
	updateCh chan<- *RevisionDestsUpdate

	// Stores the list of pods that have been successfully probed.
	healthyPods sets.String
	// Stores whether the service ClusterIP has been seen as healthy
	clusterIPHealthy bool

	transport     http.RoundTripper
	destsChan     <-chan []string
	serviceLister corev1listers.ServiceLister
	logger        *zap.SugaredLogger
}

func newRevisionWatcher(rev types.NamespacedName, protocol networking.ProtocolType,
	updateCh chan<- *RevisionDestsUpdate, destsChan <-chan []string,
	transport http.RoundTripper, serviceLister corev1listers.ServiceLister,
	logger *zap.SugaredLogger) *revisionWatcher {
	return &revisionWatcher{
		rev:           rev,
		protocol:      protocol,
		updateCh:      updateCh,
		healthyPods:   sets.NewString(),
		transport:     transport,
		destsChan:     destsChan,
		serviceLister: serviceLister,
		logger:        logger,
	}
}

func (rw *revisionWatcher) getK8sPrivateService() (*corev1.Service, error) {
	selector := labels.SelectorFromSet(map[string]string{
		serving.RevisionLabelKey:  rw.rev.Name,
		networking.ServiceTypeKey: string(networking.ServiceTypePrivate),
	})
	svcList, err := rw.serviceLister.Services(rw.rev.Namespace).List(selector)
	if err != nil {
		return nil, err
	}

	switch len(svcList) {
	case 0:
		return nil, fmt.Errorf("found no private services for revision %q", rw.rev.String())
	case 1:
		return svcList[0], nil
	default:
		return nil, fmt.Errorf("found multiple private services matching revision %v", rw.rev)
	}
}

func (rw *revisionWatcher) probe(ctx context.Context, dest string) (bool, error) {
	httpDest := url.URL{
		Scheme: "http",
		Host:   dest,
	}
	return prober.Do(ctx, rw.transport, httpDest.String(),
		prober.WithHeader(network.ProbeHeaderName, queue.Name),
		prober.ExpectsBody(queue.Name),
		prober.ExpectsStatusCodes([]int{http.StatusOK}))

}

func (rw *revisionWatcher) probeClusterIP(svc *corev1.Service) (bool, string, error) {
	if svc.Spec.ClusterIP == "" {
		return false, "", fmt.Errorf("private service %s/%s clusterIP is nil, this should never happen", svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
	}

	svcPort, ok := GetServicePort(rw.protocol, svc)
	if !ok {
		return false, "", fmt.Errorf("unable to find port in service %s/%s", svc.Namespace, svc.Name)
	}

	// Context used for our probe requests
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	dest := net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(svcPort))
	ok, err := rw.probe(ctx, dest)
	return ok, dest, err
}

// probePodIPs will probe the given target Pod IPs and will return
// the ones that are successfully probed, or an error.
func (rw *revisionWatcher) probePodIPs(dests []string) (sets.String, error) {
	// Context used for our probe requests
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	var probeGroup errgroup.Group
	healthyDests := make(chan string, len(dests))

	for _, dest := range dests {
		// Good known Pod, no need to re-probe.
		if rw.healthyPods.Has(dest) {
			healthyDests <- dest
			continue
		}

		dest := dest // Standard Go concurrency pattern.
		probeGroup.Go(func() error {
			ok, err := rw.probe(ctx, dest)
			if ok {
				healthyDests <- dest
			}
			return err
		})
	}

	err := probeGroup.Wait()
	close(healthyDests)

	hs := sets.NewString()
	for d := range healthyDests {
		hs.Insert(d)
	}

	return hs, err
}

// checkDests performs probing and potentially sends a dests update. It is
// assumed this method is not called concurrently.
func (rw *revisionWatcher) checkDests(dests []string) {
	if len(dests) == 0 {
		if rw.clusterIPHealthy {
			// we have a healthy clusterIP but revision is not ready. We must have scaled down.
			rw.clusterIPHealthy = false

			// Send update that were now inactive
			rw.updateCh <- &RevisionDestsUpdate{Rev: rw.rev}
		}

		// We know this revision cannot be healthy so short circuit
		return
	}

	if rw.clusterIPHealthy {
		// cluster IP is healthy and we havent scaled down, short circuit
		return
	}

	// First check the clusterIP
	svc, err := rw.getK8sPrivateService()
	if err != nil {
		rw.logger.Errorw("Failed to lookup private service for revision", zap.Error(err))
		return
	}

	// If clusterIP is healthy send this update and we are done
	if ok, dest, err := rw.probeClusterIP(svc); err != nil {
		rw.logger.Errorw(fmt.Sprintf("Failed to probe clusterIP %s/%s", svc.Namespace, svc.Name), zap.Error(err))
	} else if ok {
		rw.logger.Debug("ClusterIP is successfully probed:", dest)
		rw.clusterIPHealthy = true
		rw.healthyPods = nil
		rw.updateCh <- &RevisionDestsUpdate{Rev: rw.rev, ClusterIPDest: dest, ReadyAddressCount: len(dests)}
		return
	}

	hs, err := rw.probePodIPs(dests)
	if err != nil {
		rw.logger.Errorw("Failed probing", zap.Error(err))
		// We dont want to return here as an error still affects health states
	}

	rw.logger.Debug("Done probing, got healthy pods: ", hs)
	if !reflect.DeepEqual(rw.healthyPods, hs) {
		rw.healthyPods = hs
		rw.updateCh <- &RevisionDestsUpdate{
			Rev:               rw.rev,
			Dests:             hs.UnsortedList(),
			ReadyAddressCount: len(dests),
		}
	}
}

func (rw *revisionWatcher) runWithTickCh(tickCh <-chan time.Time) {
	var dests []string
	for {
		select {
		case x, ok := <-rw.destsChan:
			if !ok {
				// shutdown
				return
			}
			dests = x
		case <-tickCh:
		}
		rw.checkDests(dests)
	}
}

func (rw *revisionWatcher) run(probeFrequency time.Duration) {
	ticker := time.NewTicker(probeFrequency)
	defer ticker.Stop()

	rw.runWithTickCh(ticker.C)
}

type revisionWatcherCh struct {
	revisionWatcher *revisionWatcher
	ch              chan []string
}

// RevisionBackendsManager listens to revision endpoints and keeps track of healthy
// l4 dests which can be used to reach a revision
type RevisionBackendsManager struct {
	revisionLister servinglisters.RevisionLister
	serviceLister  corev1listers.ServiceLister

	revisionWatchers    map[types.NamespacedName]*revisionWatcherCh
	revisionWatchersMux sync.RWMutex

	updateCh       chan<- *RevisionDestsUpdate
	transport      http.RoundTripper
	logger         *zap.SugaredLogger
	probeFrequency time.Duration
}

// NewRevisionBackendsManagerWithProbeFrequency returnes a RevisionBackendsManager that uses the supplied
// probe frequency
func NewRevisionBackendsManagerWithProbeFrequency(updateCh chan<- *RevisionDestsUpdate,
	transport http.RoundTripper, revisionLister servinglisters.RevisionLister,
	serviceLister corev1listers.ServiceLister, endpointsInformer corev1informers.EndpointsInformer,
	logger *zap.SugaredLogger, probeFrequency time.Duration) *RevisionBackendsManager {
	rbm := &RevisionBackendsManager{
		revisionLister:   revisionLister,
		serviceLister:    serviceLister,
		revisionWatchers: make(map[types.NamespacedName]*revisionWatcherCh),
		updateCh:         updateCh,
		transport:        transport,
		logger:           logger,
		probeFrequency:   probeFrequency,
	}

	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.LabelExistsFilterFunc(serving.RevisionUID),
			// We are only interested in the private services, since that is
			// what is populated by the actual revision backends.
			reconciler.LabelFilterFunc(networking.ServiceTypeKey, string(networking.ServiceTypePrivate), false),
		),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    rbm.endpointsUpdated,
			UpdateFunc: controller.PassNew(rbm.endpointsUpdated),
			DeleteFunc: rbm.endpointsDeleted,
		},
	})

	return rbm
}

// NewRevisionBackendsManager creates a RevisionBackendsManager
func NewRevisionBackendsManager(updateCh chan<- *RevisionDestsUpdate,
	transport http.RoundTripper, revisionLister servinglisters.RevisionLister,
	serviceLister corev1listers.ServiceLister, endpointsInformer corev1informers.EndpointsInformer,
	logger *zap.SugaredLogger) *RevisionBackendsManager {
	return NewRevisionBackendsManagerWithProbeFrequency(updateCh, transport, revisionLister, serviceLister,
		endpointsInformer, logger, probeFrequency)
}

func (rbm *RevisionBackendsManager) getRevisionProtocol(revID types.NamespacedName) (networking.ProtocolType, error) {
	revision, err := rbm.revisionLister.Revisions(revID.Namespace).Get(revID.Name)
	if err != nil {
		return "", err
	}
	return revision.GetProtocol(), nil
}

func (rbm *RevisionBackendsManager) getOrCreateRevisionWatcher(rev types.NamespacedName) (*revisionWatcherCh, error) {
	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()

	rwCh, ok := rbm.revisionWatchers[rev]
	if !ok {
		proto, err := rbm.getRevisionProtocol(rev)
		if err != nil {
			return nil, err
		}

		destsCh := make(chan []string)
		rw := newRevisionWatcher(rev, proto, rbm.updateCh, destsCh, rbm.transport, rbm.serviceLister, rbm.logger)
		rbm.revisionWatchers[rev] = &revisionWatcherCh{rw, destsCh}
		go rw.run(rbm.probeFrequency)
		return rbm.revisionWatchers[rev], nil
	}

	return rwCh, nil
}

// endpointsUpdated is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the RevisionBackendsManager if the hosts changed
func (rbm *RevisionBackendsManager) endpointsUpdated(newObj interface{}) {
	rbm.logger.Debugf("Updating Endpoints: %+v", newObj)
	endpoints := newObj.(*corev1.Endpoints)
	revID := types.NamespacedName{endpoints.Namespace, endpoints.Labels[serving.RevisionLabelKey]}

	rwCh, err := rbm.getOrCreateRevisionWatcher(revID)
	if err != nil {
		rbm.logger.Errorw(fmt.Sprintf("Failed to get revision watcher for revision %q", revID.String()),
			zap.Error(err))
		return
	}
	rwCh.ch <- EndpointsToDests(endpoints, networking.ServicePortName(rwCh.revisionWatcher.protocol))
}

// deleteRevisionWatcher deletes the revision wathcher for rev if it exists. It expects
// a write lock is held on revisionWatchersMux when calling.
func (rbm *RevisionBackendsManager) deleteRevisionWatcher(rev types.NamespacedName) {
	if rw, ok := rbm.revisionWatchers[rev]; ok {
		close(rw.ch)
		delete(rbm.revisionWatchers, rev)
	}
}

func (rbm *RevisionBackendsManager) endpointsDeleted(obj interface{}) {
	ep := obj.(*corev1.Endpoints)
	revID := types.NamespacedName{ep.Namespace, ep.Labels[serving.RevisionLabelKey]}

	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()
	rbm.deleteRevisionWatcher(revID)
}

// Clear removes all watches and essentially "shuts down" the PodIPWatcher. This should
// be called after endpointsInformer is shut down as any subsequent informer events will
// cause new watchers to be created.
func (rbm *RevisionBackendsManager) Clear() {
	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()

	for rev := range rbm.revisionWatchers {
		rbm.deleteRevisionWatcher(rev)
	}
}
