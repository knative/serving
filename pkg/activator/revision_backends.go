package activator

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/prober"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler"
)

// RevisionDestsUpdate is the main output of a RevisionBackendsManager which contains the l4 dests
// information for how to reach a revision via PodIP
type RevisionDestsUpdate struct {
	Rev   RevisionID
	Dests []string
}

const (
	probePortName  string        = "http"
	probeTimeout   time.Duration = 300 * time.Millisecond
	probeFrequency time.Duration = 200 * time.Millisecond
)

type revisionWatcher struct {
	rev          RevisionID
	updateCh     chan<- *RevisionDestsUpdate
	dests        []string
	healthStates map[string]bool

	transport http.RoundTripper
	destsChan <-chan []string
	logger    *zap.SugaredLogger
}

func newRevisionWatcher(rev RevisionID, updateCh chan<- *RevisionDestsUpdate,
	destsChan <-chan []string, transport http.RoundTripper,
	logger *zap.SugaredLogger) *revisionWatcher {
	return &revisionWatcher{
		rev:          rev,
		updateCh:     updateCh,
		healthStates: make(map[string]bool),
		transport:    transport,
		destsChan:    destsChan,
		logger:       logger,
	}
}

func endpointsToDests(endpoints *corev1.Endpoints) []string {
	ret := []string{}

	for _, es := range endpoints.Subsets {
		portVal := int32(0)
		for _, port := range es.Ports {
			if port.Name == probePortName {
				portVal = port.Port
				break
			}
		}

		// This endpoint doesnt have a port we want to probe
		if portVal == 0 {
			continue
		}

		for _, addr := range es.Addresses {
			// Prefer IP as we can avoid a DNS lookup this way
			ret = append(ret, net.JoinHostPort(addr.IP, strconv.Itoa(int(portVal))))
		}
	}

	return ret
}

func filterHealthyDests(dests map[string]bool) []string {
	ret := make([]string, 0, len(dests))
	for dest, healthy := range dests {
		if healthy {
			ret = append(ret, dest)
		}
	}
	return ret
}

type destHealth struct {
	dest   string
	health bool
}

// checkDests performs probing and potentially sends a dests update. It is
// assumed this method is not called concurrently.
func (rw *revisionWatcher) checkDests() {
	healthStatesCh := make(chan *destHealth, len(rw.dests))
	defer close(healthStatesCh)

	// Context used for our requests
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	for _, dest := range rw.dests {
		// If the dest is already healthy then save this
		if curHealthy, ok := rw.healthStates[dest]; ok && curHealthy {
			healthStatesCh <- &destHealth{dest, true}
			continue
		}

		go func(dest string) {
			httpDest := url.URL{
				Scheme: "http",
				Host:   dest,
			}
			ok, err := prober.Do(ctx, rw.transport, httpDest.String(),
				prober.WithHeader(network.ProbeHeaderName, queue.Name),
				prober.ExpectsBody(queue.Name))

			healthStatesCh <- &destHealth{dest, ok}

			if err != nil {
				rw.logger.Errorw("Failed probing "+dest, zap.Error(err))
			} else if !ok {
				rw.logger.Info("Probing of dest " + dest)
			}
		}(dest)
	}

	healthStates := make(map[string]bool, len(rw.dests))
	for i := 0; i < len(rw.dests); i++ {
		dh := <-healthStatesCh
		healthStates[dh.dest] = dh.health
	}

	if !reflect.DeepEqual(rw.healthStates, healthStates) {
		rw.healthStates = healthStates
		destsArr := filterHealthyDests(healthStates)
		rw.updateCh <- &RevisionDestsUpdate{
			Rev:   rw.rev,
			Dests: destsArr,
		}
	}
}

func (rw *revisionWatcher) runWithTickCh(tickCh <-chan time.Time) {
	for {
		select {
		case dests, ok := <-rw.destsChan:
			if !ok {
				// shutdown
				return
			}
			rw.dests = dests
		case <-tickCh:
		}
		rw.checkDests()
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
// l4 dests which can be used to reach a revisoin
type RevisionBackendsManager struct {
	revisionWatchers    map[RevisionID]*revisionWatcherCh
	revisionWatchersMux sync.RWMutex

	updateCh       chan<- *RevisionDestsUpdate
	transport      http.RoundTripper
	logger         *zap.SugaredLogger
	probeFrequency time.Duration
}

// NewRevisionBackendsManagerWithProbeFrequency returnes a RevisionBackendsManager that uses the supplied
// probe frequency
func NewRevisionBackendsManagerWithProbeFrequency(updateCh chan<- *RevisionDestsUpdate,
	transport http.RoundTripper, endpointsInformer corev1informers.EndpointsInformer,
	logger *zap.SugaredLogger, probeFrequency time.Duration) *RevisionBackendsManager {
	rbm := &RevisionBackendsManager{
		revisionWatchers: make(map[RevisionID]*revisionWatcherCh),
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

func NewRevisionBackendsManager(updateCh chan<- *RevisionDestsUpdate,
	transport http.RoundTripper, endpointsInformer corev1informers.EndpointsInformer,
	logger *zap.SugaredLogger) *RevisionBackendsManager {
	return NewRevisionBackendsManagerWithProbeFrequency(updateCh, transport, endpointsInformer,
		logger, probeFrequency)
}

func (rbm *RevisionBackendsManager) getOrCreateDestsCh(rev RevisionID) chan []string {
	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()

	rwCh, ok := rbm.revisionWatchers[rev]
	if !ok {
		destsCh := make(chan []string)
		rw := newRevisionWatcher(rev, rbm.updateCh, destsCh, rbm.transport, rbm.logger)
		rbm.revisionWatchers[rev] = &revisionWatcherCh{rw, destsCh}
		go rw.run(rbm.probeFrequency)
		return destsCh
	}

	return rwCh.ch
}

// endpointsUpdated is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the RevisionBackendsManager if the hosts changed
func (rbm *RevisionBackendsManager) endpointsUpdated(newObj interface{}) {
	endpoints := newObj.(*corev1.Endpoints)
	revID := RevisionID{endpoints.Namespace, endpoints.Labels[serving.RevisionLabelKey]}

	rbm.getOrCreateDestsCh(revID) <- endpointsToDests(endpoints)
}

// deleteRevisionWatcher deletes the revision wathcher for rev if it exists. It expects
// a write lock is held on revisoinWatchersMux when calling.
func (rbm *RevisionBackendsManager) deleteRevisionWatcher(rev RevisionID) {
	if rw, ok := rbm.revisionWatchers[rev]; ok {
		close(rw.ch)
		delete(rbm.revisionWatchers, rev)
	}
}

func (rbm *RevisionBackendsManager) endpointsDeleted(obj interface{}) {
	ep := obj.(*corev1.Endpoints)
	revID := RevisionID{ep.Namespace, ep.Labels[serving.RevisionLabelKey]}

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
