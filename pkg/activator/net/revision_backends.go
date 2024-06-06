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

package net

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	pkgnet "knative.dev/networking/pkg/apis/networking"
	netcfg "knative.dev/networking/pkg/config"
	nethttp "knative.dev/networking/pkg/http"
	netheader "knative.dev/networking/pkg/http/header"
	netprober "knative.dev/networking/pkg/prober"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/reconciler/serverlessservice/resources/names"
)

// revisionDestsUpdate contains the state of healthy l4 dests for talking to a revision and is the
// primary output from the RevisionBackendsManager system. If a healthy ClusterIP is found then
// ClusterIPDest will be set to non empty string and Dests will be nil. Otherwise Dests will be set
// to a slice of healthy l4 dests for reaching the revision.
type revisionDestsUpdate struct {
	Rev           types.NamespacedName
	ClusterIPDest string
	Dests         sets.Set[string]
}

type dests struct {
	ready    sets.Set[string]
	notReady sets.Set[string]
}

func (d dests) becameNonReady(prev dests) sets.Set[string] {
	return prev.ready.Intersection(d.notReady)
}

// MarshalLogObject implements zapcore.ObjectMarshaler interface.
// This permits logging dests as Fields and permits evaluation of the
// string lazily only if we need to log, vs always if it's passed as a formatter
// string argument.
func (d dests) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("ready", strings.Join(d.ready.UnsortedList(), ","))
	enc.AddString("notReady", strings.Join(d.notReady.UnsortedList(), ","))
	return nil
}

const (
	probeTimeout          time.Duration = 300 * time.Millisecond
	defaultProbeFrequency time.Duration = 200 * time.Millisecond
)

// revisionWatcher watches the podIPs and ClusterIP of the service for a revision. It implements the logic
// to supply revisionDestsUpdate events on updateCh
type revisionWatcher struct {
	stopCh   <-chan struct{}
	cancel   context.CancelFunc
	rev      types.NamespacedName
	protocol pkgnet.ProtocolType
	updateCh chan<- revisionDestsUpdate
	done     chan struct{}

	// Stores the list of pods that have been successfully probed.
	healthyPods sets.Set[string]
	// Stores whether the service ClusterIP has been seen as healthy.
	clusterIPHealthy bool

	transport     http.RoundTripper
	destsCh       chan dests
	serviceLister corev1listers.ServiceLister
	logger        *zap.SugaredLogger

	// podsAddressable will be set to false if we cannot
	// probe a pod directly, but its cluster IP has been successfully probed.
	podsAddressable bool

	// usePassthroughLb makes the probing use the passthrough lb headers to enable
	// pod addressability even in meshes.
	usePassthroughLb bool

	// meshMode configures whether we always directly probe pods,
	// always use cluster IP, or attempt to autodetect
	meshMode netcfg.MeshCompatibilityMode

	// enableProbeOptimisation causes the activator to treat a container as ready
	// as soon as it gets a ready response from the Queue Proxy readiness
	// endpoint, even if kubernetes has not yet marked the pod Ready.
	// This must be disabled when the Queue Proxy readiness check does not fully
	// cover the revision's ready conditions, for example when an exec probe is
	// being used.
	enableProbeOptimisation bool
}

func newRevisionWatcher(ctx context.Context, rev types.NamespacedName, protocol pkgnet.ProtocolType,
	updateCh chan<- revisionDestsUpdate, destsCh chan dests,
	transport http.RoundTripper, serviceLister corev1listers.ServiceLister,
	usePassthroughLb bool, meshMode netcfg.MeshCompatibilityMode,
	enableProbeOptimisation bool,
	logger *zap.SugaredLogger) *revisionWatcher {
	ctx, cancel := context.WithCancel(ctx)
	return &revisionWatcher{
		stopCh:                  ctx.Done(),
		cancel:                  cancel,
		rev:                     rev,
		protocol:                protocol,
		updateCh:                updateCh,
		done:                    make(chan struct{}),
		transport:               transport,
		destsCh:                 destsCh,
		serviceLister:           serviceLister,
		podsAddressable:         true, // By default we presume we can talk to pods directly.
		usePassthroughLb:        usePassthroughLb,
		meshMode:                meshMode,
		enableProbeOptimisation: enableProbeOptimisation,
		logger:                  logger.With(zap.String(logkey.Key, rev.String())),
	}
}

// probe probes the destination and returns whether it is ready according to
// the probe. If the failure is not compatible with having been caused by mesh
// being enabled, notMesh will be true.
func (rw *revisionWatcher) probe(ctx context.Context, dest string) (pass bool, notMesh bool, err error) {
	httpDest := url.URL{
		Scheme: "http",
		Host:   dest,
		Path:   nethttp.HealthCheckPath,
	}

	// We don't want to unnecessarily fall back to ClusterIP if we see a failure
	// that could not have been caused by the mesh being enabled.
	var checkMesh netprober.Verifier = func(resp *http.Response, _ []byte) (bool, error) {
		notMesh = !nethttp.IsPotentialMeshErrorResponse(resp)
		return true, nil
	}

	// NOTE: changes below may require changes to testing/roundtripper.go to make unit tests pass.
	options := []interface{}{
		netprober.WithHeader(netheader.ProbeKey, queue.Name),
		netprober.WithHeader(netheader.UserAgentKey, netheader.ActivatorUserAgent),
		// Order is important since first failing verification short-circuits the rest: checkMesh must be first.
		checkMesh,
		netprober.ExpectsStatusCodes([]int{http.StatusOK}),
		netprober.ExpectsBody(queue.Name),
	}

	if rw.usePassthroughLb {
		// Add the passthrough header + force the Host header to point to the service
		// we're targeting, to make sure ingress can correctly route it.
		// We cannot set these headers unconditionally as the Host header will cause the
		// request to be loadbalanced by ingress "silently" if passthrough LB is not
		// configured, which will cause the request to "pass" but doesn't guarantee it
		// actually lands on the correct pod, which breaks our state keeping.
		options = append(options,
			netprober.WithHost(names.PrivateService(rw.rev.Name)+"."+rw.rev.Namespace),
			netprober.WithHeader(netheader.PassthroughLoadbalancingKey, "true"))
	}

	match, err := netprober.Do(ctx, rw.transport, httpDest.String(), options...)
	return match, notMesh, err
}

func (rw *revisionWatcher) getDest() (string, error) {
	svc, err := rw.serviceLister.Services(rw.rev.Namespace).Get(names.PrivateService(rw.rev.Name))
	if err != nil {
		return "", err
	}
	if svc.Spec.ClusterIP == "" {
		return "", fmt.Errorf("private service %s/%s clusterIP is nil, this should never happen", svc.ObjectMeta.Namespace, svc.ObjectMeta.Name)
	}

	svcPort, ok := getServicePort(rw.protocol, svc)
	if !ok {
		return "", fmt.Errorf("unable to find port in service %s/%s", svc.Namespace, svc.Name)
	}
	return net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(svcPort)), nil
}

func (rw *revisionWatcher) probeClusterIP(dest string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	match, _, err := rw.probe(ctx, dest)
	return match, err
}

// probePodIPs will probe the given target Pod IPs and will return
// the ones that are successfully probed, whether the update was a no-op, or an error.
// If probing fails but not all errors were compatible with being caused by
// mesh being enabled, being enabled, notMesh will be true.
func (rw *revisionWatcher) probePodIPs(ready, notReady sets.Set[string]) (succeeded sets.Set[string], noop bool, notMesh bool, err error) {
	dests := ready.Union(notReady)

	// Short circuit case where all the current pods are already known to be healthy.
	if rw.healthyPods.Equal(dests) {
		return rw.healthyPods, true /*no-op*/, false /* notMesh */, nil
	}

	healthy := rw.healthyPods.Union(nil)
	if rw.meshMode != netcfg.MeshCompatibilityModeAuto {
		// If Kubernetes marked the pod ready before we managed to probe it, and we're
		// not also using the probe to sniff whether mesh is enabled, we can just
		// trust Kubernetes and mark "Ready" pods healthy without probing.
		for r := range ready {
			healthy.Insert(r)
		}
	}

	// Context used for our probe requests.
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	// Empty errgroup is used as cancellation on first error is not desired, all probes should be
	// attempted even if one fails.
	var probeGroup errgroup.Group
	healthyDests := make(chan string, dests.Len())

	var sawNotMesh atomic.Bool
	for dest := range dests {
		if healthy.Has(dest) {
			// If we already know it's healthy we don't need to probe again.
			continue
		}

		dest := dest // Standard Go concurrency pattern.
		probeGroup.Go(func() error {
			ok, notMesh, err := rw.probe(ctx, dest)
			if ok && (ready.Has(dest) || rw.enableProbeOptimisation) {
				healthyDests <- dest
			}
			if notMesh {
				// If *any* of the errors are not mesh related, assume the mesh is not
				// enabled and stay with direct scraping.
				sawNotMesh.Store(true)
			}
			return err
		})
	}

	err = probeGroup.Wait()
	close(healthyDests)

	for d := range healthyDests {
		healthy.Insert(d)
	}

	for d := range healthy {
		if !dests.Has(d) {
			// Remove destinations that are no longer in the ready/notReady set.
			healthy.Delete(d)
		}
	}

	// Unchanged only if we match the incoming healthy set, as this handles all possible updates
	unchanged := healthy.Equal(rw.healthyPods)

	return healthy, unchanged, sawNotMesh.Load(), err
}

func (rw *revisionWatcher) sendUpdate(clusterIP string, dests sets.Set[string]) {
	select {
	case <-rw.stopCh:
		return
	default:
		rw.updateCh <- revisionDestsUpdate{Rev: rw.rev, ClusterIPDest: clusterIP, Dests: dests}
	}
}

// checkDests performs probing and potentially sends a dests update. It is
// assumed this method is not called concurrently.
func (rw *revisionWatcher) checkDests(curDests, prevDests dests) {
	if len(curDests.ready) == 0 && len(curDests.notReady) == 0 {
		// We must have scaled down.
		rw.clusterIPHealthy = false
		rw.healthyPods = nil
		rw.logger.Debug("ClusterIP is no longer healthy.")
		// Send update that we are now inactive (both params invalid).
		rw.sendUpdate("", nil)
		return
	}

	// If we have discovered (or have been told via meshMode) that this revision
	// cannot be probed directly do not spend time trying.
	if rw.podsAddressable && rw.meshMode != netcfg.MeshCompatibilityModeEnabled {
		// reprobe set contains the targets that moved from ready to non-ready set.
		// so they have to be re-probed.
		reprobe := curDests.becameNonReady(prevDests)
		if len(reprobe) > 0 {
			rw.logger.Infow("Need to reprobe pods who became non-ready",
				zap.Object("IPs", logging.StringSet(reprobe)))
			// Trim the pods that migrated to the non-ready set from the
			// ready set from the healthy pods. They will automatically
			// probed below.
			for p := range reprobe {
				rw.healthyPods.Delete(p)
			}
		}
		// First check the pod IPs. If we can individually address
		// the Pods we should go that route, since it permits us to do
		// precise load balancing in the throttler.
		hs, noop, notMesh, err := rw.probePodIPs(curDests.ready, curDests.notReady)
		if err != nil {
			rw.logger.Warnw("Failed probing pods", zap.Object("curDests", curDests), zap.Error(err))
			// We dont want to return here as an error still affects health states.
		}

		// We need to send update if reprobe is non-empty, since the state
		// of the world has been changed.
		rw.logger.Debugf("Done probing, got %d healthy pods", len(hs))
		if !noop || len(reprobe) > 0 {
			rw.healthyPods = hs
			// Note: it's important that this copies (via hs.Union) the healthy pods
			// set before sending the update to avoid concurrent modifications
			// affecting the throttler, which iterates over the set.
			rw.sendUpdate("" /*clusterIP*/, hs.Union(nil))
			return
		}
		// no-op, and we have successfully probed at least one pod.
		if len(hs) > 0 {
			return
		}
		// We didn't get any pods, but we know the mesh is not enabled since we got
		// a non-mesh status code while probing, so we don't want to fall back.
		if notMesh {
			return
		}
	}

	if rw.usePassthroughLb {
		// If passthrough lb is enabled we do not want to fall back to going via the
		// clusterIP and instead want to exit early.
		return
	}

	if rw.meshMode == netcfg.MeshCompatibilityModeDisabled {
		// If mesh is disabled we always want to use direct pod addressing, and
		// will not fall back to clusterIP.
		return
	}

	// If we failed to probe even a single pod, check the clusterIP.
	// NB: We can't cache the IP address, since user might go rogue
	// and delete the K8s service. We'll fix it, but the cluster IP will be different.
	dest, err := rw.getDest()
	if err != nil {
		rw.logger.Errorw("Failed to determine service destination", zap.Error(err))
		return
	}

	// If cluster IP is healthy and we haven't scaled down, short circuit.
	if rw.clusterIPHealthy {
		rw.logger.Debugf("ClusterIP %s already probed (ready backends: %d)", dest, len(curDests.ready))
		rw.sendUpdate(dest, curDests.ready)
		return
	}

	// If clusterIP is healthy send this update and we are done.
	if ok, err := rw.probeClusterIP(dest); err != nil {
		rw.logger.Errorw("Failed to probe clusterIP "+dest, zap.Error(err))
	} else if ok {
		// We can reach here only iff pods are not successfully individually probed
		// but ClusterIP conversely has been successfully probed.
		rw.podsAddressable = false
		rw.logger.Debugf("ClusterIP is successfully probed: %s (ready backends: %d)", dest, len(curDests.ready))
		rw.clusterIPHealthy = true
		rw.healthyPods = nil
		rw.sendUpdate(dest, curDests.ready)
	}
}

func (rw *revisionWatcher) run(probeFrequency time.Duration) {
	defer close(rw.done)

	var curDests, prevDests dests
	timer := time.NewTicker(probeFrequency)
	defer timer.Stop()

	var tickCh <-chan time.Time
	for {
		// If we have at least one pod and either there are pods that have not been
		// successfully probed or clusterIP has not been probed (no pod addressability),
		// then we want to probe on timer.
		rw.logger.Debugw("Revision state", zap.Object("dests", curDests),
			zap.Object("healthy", logging.StringSet(rw.healthyPods)),
			zap.Bool("clusterIPHealthy", rw.clusterIPHealthy))
		if len(curDests.ready)+len(curDests.notReady) > 0 && !(rw.clusterIPHealthy ||
			curDests.ready.Union(curDests.notReady).Equal(rw.healthyPods)) {
			rw.logger.Debug("Probing on timer")
			tickCh = timer.C
		} else {
			rw.logger.Debug("Not Probing on timer")
			tickCh = nil
		}

		select {
		case <-rw.stopCh:
			return
		case x := <-rw.destsCh:
			rw.logger.Debugf("Updating Endpoints: ready backends: %d, not-ready backends: %d", len(x.ready), len(x.notReady))
			prevDests, curDests = curDests, x
		case <-tickCh:
		}

		rw.checkDests(curDests, prevDests)
	}
}

// revisionBackendsManager listens to revision endpoints and keeps track of healthy
// l4 dests which can be used to reach a revision
type revisionBackendsManager struct {
	ctx            context.Context
	revisionLister servinglisters.RevisionLister
	serviceLister  corev1listers.ServiceLister

	revisionWatchers    map[types.NamespacedName]*revisionWatcher
	revisionWatchersMux sync.RWMutex

	updateCh         chan revisionDestsUpdate
	transport        http.RoundTripper
	usePassthroughLb bool
	meshMode         netcfg.MeshCompatibilityMode
	logger           *zap.SugaredLogger
	probeFrequency   time.Duration
}

// NewRevisionBackendsManager returns a new RevisionBackendsManager with default
// probe time out.
func newRevisionBackendsManager(ctx context.Context, tr http.RoundTripper, usePassthroughLb bool, meshMode netcfg.MeshCompatibilityMode) *revisionBackendsManager {
	return newRevisionBackendsManagerWithProbeFrequency(ctx, tr, usePassthroughLb, meshMode, defaultProbeFrequency)
}

// newRevisionBackendsManagerWithProbeFrequency creates a fully spec'd RevisionBackendsManager.
func newRevisionBackendsManagerWithProbeFrequency(ctx context.Context, tr http.RoundTripper,
	usePassthroughLb bool, meshMode netcfg.MeshCompatibilityMode, probeFreq time.Duration) *revisionBackendsManager {
	rbm := &revisionBackendsManager{
		ctx:              ctx,
		revisionLister:   revisioninformer.Get(ctx).Lister(),
		serviceLister:    serviceinformer.Get(ctx).Lister(),
		revisionWatchers: make(map[types.NamespacedName]*revisionWatcher),
		updateCh:         make(chan revisionDestsUpdate),
		transport:        tr,
		usePassthroughLb: usePassthroughLb,
		meshMode:         meshMode,
		logger:           logging.FromContext(ctx),
		probeFrequency:   probeFreq,
	}
	endpointsInformer := endpointsinformer.Get(ctx)
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

	go func() {
		// updateCh can only be closed after revisionWatchers are done running
		defer close(rbm.updateCh)

		// Wait for cancellation
		<-rbm.ctx.Done()

		// Wait for all revisionWatchers to be done
		rbm.revisionWatchersMux.Lock()
		defer rbm.revisionWatchersMux.Unlock()
		for _, rw := range rbm.revisionWatchers {
			<-rw.done
		}
	}()

	return rbm
}

// Returns channel where destination updates are sent to.
func (rbm *revisionBackendsManager) updates() <-chan revisionDestsUpdate {
	return rbm.updateCh
}

func (rbm *revisionBackendsManager) getOrCreateRevisionWatcher(revID types.NamespacedName) (*revisionWatcher, error) {
	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()

	rwCh, ok := rbm.revisionWatchers[revID]
	if !ok {
		rev, err := rbm.revisionLister.Revisions(revID.Namespace).Get(revID.Name)
		if err != nil {
			return nil, err
		}

		enableProbeOptimisation := true
		if rp := rev.Spec.GetContainer().ReadinessProbe; rp != nil && rp.Exec != nil {
			enableProbeOptimisation = false
		}
		// Startup probes are executed by Kubelet, so we can only mark the container as ready
		// once K8s sees it as ready.
		if sp := rev.Spec.GetContainer().StartupProbe; sp != nil {
			enableProbeOptimisation = false
		}

		destsCh := make(chan dests)
		rw := newRevisionWatcher(rbm.ctx, revID, rev.GetProtocol(), rbm.updateCh, destsCh, rbm.transport, rbm.serviceLister, rbm.usePassthroughLb, rbm.meshMode, enableProbeOptimisation, rbm.logger)
		rbm.revisionWatchers[revID] = rw
		go rw.run(rbm.probeFrequency)
		return rw, nil
	}

	return rwCh, nil
}

// endpointsUpdated is a handler function to be used by the Endpoints informer.
// It updates the endpoints in the RevisionBackendsManager if the hosts changed
func (rbm *revisionBackendsManager) endpointsUpdated(newObj interface{}) {
	// Ignore the updates when we've terminated.
	select {
	case <-rbm.ctx.Done():
		return
	default:
	}
	endpoints := newObj.(*corev1.Endpoints)
	revID := types.NamespacedName{Namespace: endpoints.Namespace, Name: endpoints.Labels[serving.RevisionLabelKey]}

	rw, err := rbm.getOrCreateRevisionWatcher(revID)
	if err != nil {
		rbm.logger.Errorw("Failed to get revision watcher", zap.Error(err), zap.String(logkey.Key, revID.String()))
		return
	}
	ready, notReady := endpointsToDests(endpoints, pkgnet.ServicePortName(rw.protocol))
	select {
	case <-rbm.ctx.Done():
		return
	case rw.destsCh <- dests{ready: ready, notReady: notReady}:
	}
}

// deleteRevisionWatcher deletes the revision watcher for rev if it exists. It expects
// a write lock is held on revisionWatchersMux when calling.
func (rbm *revisionBackendsManager) deleteRevisionWatcher(rev types.NamespacedName) {
	if rw, ok := rbm.revisionWatchers[rev]; ok {
		rw.cancel()
		delete(rbm.revisionWatchers, rev)
	}
}

func (rbm *revisionBackendsManager) endpointsDeleted(obj interface{}) {
	// Ignore the updates when we've terminated.
	select {
	case <-rbm.ctx.Done():
		return
	default:
	}
	ep := obj.(*corev1.Endpoints)
	revID := types.NamespacedName{Namespace: ep.Namespace, Name: ep.Labels[serving.RevisionLabelKey]}

	rbm.logger.Debugw("Deleting endpoint", zap.String(logkey.Key, revID.String()))
	rbm.revisionWatchersMux.Lock()
	defer rbm.revisionWatchersMux.Unlock()
	rbm.deleteRevisionWatcher(revID)
}
