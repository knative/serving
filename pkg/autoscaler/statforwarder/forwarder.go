/*
Copyright 2020 The Knative Authors

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

package statforwarder

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	coordinationv1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	leaseinformer "knative.dev/pkg/client/injection/kube/informers/coordination/v1/lease"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/network"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/autoscaler/bucket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
	retryTimeout       = 3 * time.Second
	retryInterval      = 100 * time.Millisecond

	// Retry at most 15 seconds to process a stat. NOTE: Retrying could
	// cause high delay and inaccurate scaling decision because we use
	// the timestamp on receiving.
	maxProcessingRetry      = 30
	retryProcessingInterval = 500 * time.Millisecond
)

var svcURLSuffix = fmt.Sprintf("svc.%s:%d", network.GetClusterDomainName(), autoscalerPort)

// statProcessor is a function to process a single StatMessage.
type statProcessor func(sm asmetrics.StatMessage)

type stat struct {
	sm    asmetrics.StatMessage
	retry int
}

// Forwarder does the following things:
// 1. Watches the change of Leases for Autoscaler buckets. Stores the
//    Lease -> IP mapping.
// 2. Creates/updates the corresponding K8S Service and Endpoints.
// 3. Can be used to forward the metrics owned by a bucket based on
//    the holder IP.
type Forwarder struct {
	selfIP          string
	logger          *zap.SugaredLogger
	kc              kubernetes.Interface
	endpointsLister corev1listers.EndpointsLister

	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor
	// bs is the BucketSet including all Autoscaler buckets.
	bs *hash.BucketSet
	// processorsLock is the lock for processors.
	processorsLock sync.RWMutex
	processors     map[string]*bucketProcessor
	// Used to capture asynchronous processes for re-enqueuing to be waited
	// on when shutting down.
	retryWg sync.WaitGroup
	// Used to capture asynchronous processe for stats to be waited
	// on when shutting down.
	processingWg sync.WaitGroup

	// id2ip stores the IP extracted from the holder identity to avoid
	// string split each time.
	id2ip map[string]string

	statCh chan stat
	stopCh chan struct{}
}

// New creates a new Forwarder.
func New(ctx context.Context, logger *zap.SugaredLogger, kc kubernetes.Interface, selfIP string, bs *hash.BucketSet, accept statProcessor) *Forwarder {
	ns := system.Namespace()
	bkts := bs.Buckets()
	endpointsInformer := endpointsinformer.Get(ctx)
	f := &Forwarder{
		selfIP:          selfIP,
		logger:          logger,
		kc:              kc,
		endpointsLister: endpointsInformer.Lister(),
		bs:              bs,
		processors:      make(map[string]*bucketProcessor, len(bkts)),
		accept:          accept,
		id2ip:           make(map[string]string),
		statCh:          make(chan stat, 1000),
		stopCh:          make(chan struct{}),
	}

	f.processingWg.Add(1)
	go f.process()

	leaseInformer := leaseinformer.Get(ctx)
	leaseInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: f.filterFunc(ns),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    f.leaseUpdated,
			UpdateFunc: controller.PassNew(f.leaseUpdated),
			// TODO(yanweiguo): Set up DeleteFunc.
		},
	})

	return f
}

func (f *Forwarder) extractIP(holder string) (string, error) {
	if ip, ok := f.id2ip[holder]; ok {
		return ip, nil
	}

	_, ip, err := bucket.ExtractPodNameAndIP(holder)
	if err == nil {
		f.id2ip[holder] = ip
	}
	return ip, err
}

func (f *Forwarder) filterFunc(namespace string) func(interface{}) bool {
	return func(obj interface{}) bool {
		l := obj.(*coordinationv1.Lease)

		if l.Namespace != namespace {
			return false
		}

		if !f.bs.HasBucket(l.Name) {
			// Not for Autoscaler.
			return false
		}

		if l.Spec.HolderIdentity == nil || *l.Spec.HolderIdentity == "" {
			// No holder yet.
			return false
		}

		// The holder identity is in format of <POD-NAME>_<POD-IP>.
		ip, err := f.extractIP(*l.Spec.HolderIdentity)
		if err != nil {
			f.logger.Warn("Found invalid Lease holder identify ", *l.Spec.HolderIdentity)
			return false
		}
		if p := f.getProcessor(l.Name); p != nil && p.ip == ip {
			// Already up-to-date.
			return false
		}

		return true
	}
}

func (f *Forwarder) leaseUpdated(obj interface{}) {
	l := obj.(*coordinationv1.Lease)
	ns, n := l.Namespace, l.Name
	ctx := context.Background()

	if l.Spec.HolderIdentity == nil {
		// This shouldn't happen as we filter it out when queueing.
		// Add a nil point check for safety.
		f.logger.Warnf("Lease %s/%s with nil HolderIdentity got enqueued", ns, n)
		return
	}

	// Close existing connection if there's one. The target address won't
	// be the same as the IP has changed.
	f.shutdown(f.getProcessor(n))
	// The map should have the IP because of the operations in the filter when queueing.
	ip, ok := f.id2ip[*l.Spec.HolderIdentity]
	if !ok {
		// This shouldn't happen because we store the value into the map in the filter
		// when queueing. Add a log here in case something unexpected happens.
		f.logger.Warn("IP not found in cached map for ", *l.Spec.HolderIdentity)
		return
	}
	f.setProcessor(n, f.createProcessor(ns, n, ip))

	if ip != f.selfIP {
		// Skip creating/updating Service and Endpoints if not the leader.
		return
	}

	if err := f.createService(ctx, ns, n); err != nil {
		f.logger.Fatalf("Failed to create Service for Lease %s/%s: %v", ns, n, err)
		return
	}
	f.logger.Infof("Created Service for Lease %s/%s", ns, n)

	if err := f.createOrUpdateEndpoints(ctx, ns, n); err != nil {
		f.logger.Fatalf("Failed to create Endpoints for Lease %s/%s: %v", ns, n, err)
		return
	}
	f.logger.Infof("Created Endpoints for Lease %s/%s", ns, n)
}

func (f *Forwarder) createService(ctx context.Context, ns, n string) error {
	var lastErr error
	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		_, lastErr = f.kc.CoreV1().Services(ns).Create(ctx, &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{{
					Name:       autoscalerPortName,
					Port:       autoscalerPort,
					TargetPort: intstr.FromInt(autoscalerPort),
				}},
			},
		}, metav1.CreateOptions{})

		if apierrs.IsAlreadyExists(lastErr) {
			return true, nil
		}

		// Do not return the error to cause a retry.
		return lastErr == nil, nil
	}); err != nil {
		return lastErr
	}

	return nil
}

// createOrUpdateEndpoints creates an Endpoints object with the given namespace and
// name, and the Forwarder.selfIP. If the Endpoints object already
// exists, it will update the Endpoints with the Forwarder.selfIP.
func (f *Forwarder) createOrUpdateEndpoints(ctx context.Context, ns, n string) error {
	wantSubsets := []v1.EndpointSubset{{
		Addresses: []v1.EndpointAddress{{
			IP: f.selfIP,
		}},
		Ports: []v1.EndpointPort{{
			Name:     autoscalerPortName,
			Port:     autoscalerPort,
			Protocol: v1.ProtocolTCP,
		}}},
	}

	exists := true
	var lastErr error
	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		e, err := f.endpointsLister.Endpoints(ns).Get(n)
		if apierrs.IsNotFound(err) {
			exists = false
			return true, nil
		}

		if err != nil {
			lastErr = err
			// Do not return the error to cause a retry.
			return false, nil
		}

		if equality.Semantic.DeepEqual(wantSubsets, e.Subsets) {
			return true, nil
		}

		want := e.DeepCopy()
		want.Subsets = wantSubsets
		if _, lastErr = f.kc.CoreV1().Endpoints(ns).Update(ctx, want, metav1.UpdateOptions{}); lastErr != nil {
			// Do not return the error to cause a retry.
			return false, nil
		}

		f.logger.Infof("Bucket Endpoints %s updated with IP %s", n, f.selfIP)
		return true, nil
	}); err != nil {
		return lastErr
	}

	if exists {
		return nil
	}

	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		_, lastErr = f.kc.CoreV1().Endpoints(ns).Create(ctx, &v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Subsets: wantSubsets,
		}, metav1.CreateOptions{})
		// Do not return the error to cause a retry.
		return lastErr == nil, nil
	}); err != nil {
		return lastErr
	}

	return nil
}

func (f *Forwarder) getProcessor(bkt string) *bucketProcessor {
	f.processorsLock.RLock()
	defer f.processorsLock.RUnlock()
	return f.processors[bkt]
}

func (f *Forwarder) setProcessor(bkt string, p *bucketProcessor) {
	f.processorsLock.Lock()
	defer f.processorsLock.Unlock()
	f.processors[bkt] = p
}

func (f *Forwarder) createProcessor(ns, bkt, ip string) *bucketProcessor {
	if ip == f.selfIP {
		return &bucketProcessor{
			bkt:    bkt,
			logger: f.logger.With(zap.String("bucket", bkt)),
			ip:     ip,
			accept: f.accept,
		}
	}

	return newForwardProcessor(f.logger.With(zap.String("bucket", bkt)), bkt, ip,
		fmt.Sprintf("ws://%s:%d", ip, autoscalerPort),
		fmt.Sprintf("ws://%s.%s.%s", bkt, ns, svcURLSuffix))
}

// Process enqueues the given Stat for processing asynchronously.
// It calls Forwarder.accept if the pod where this Forwarder is running is the owner
// of the given StatMessage. Otherwise it forwards the given StatMessage to the right
// owner pod. It will retry if any error happens during the processing.
func (f *Forwarder) Process(sm asmetrics.StatMessage) {
	f.statCh <- stat{sm: sm, retry: 0}
}

func (f *Forwarder) process() {
	defer func() {
		f.retryWg.Wait()
		f.processingWg.Done()
	}()

	for {
		select {
		case <-f.stopCh:
			return
		case s := <-f.statCh:
			rev := s.sm.Key.String()
			l := f.logger.With(zap.String("revision", rev))
			bkt := f.bs.Owner(rev)

			p := f.getProcessor(bkt)
			if p == nil {
				l.Warn("Can't find the owner for Revision.")
				f.maybeRetry(l, s)
				continue
			}

			if err := p.process(s.sm); err != nil {
				l.Errorw("Error while processing stat", zap.Error(err))
				f.maybeRetry(l, s)
			}
		}
	}
}

func (f *Forwarder) maybeRetry(logger *zap.SugaredLogger, s stat) {
	if s.retry > maxProcessingRetry {
		logger.Warn("Exceeding max retries. Dropping the stat.")
	}

	s.retry++
	f.retryWg.Add(1)
	go func() {
		defer f.retryWg.Done()
		// TODO(yanweiguo): Use RateLimitingInterface and NewItemFastSlowRateLimiter.
		time.Sleep(retryProcessingInterval)
		logger.Debug("Enqueuing stat for retry.")
		f.statCh <- s
	}()
}

func (f *Forwarder) shutdown(p *bucketProcessor) {
	if p != nil && p.accept == nil {
		p.shutdown()
	}
}

// Cancel is the function to call when terminating a Forwarder.
func (f *Forwarder) Cancel() {
	// Tell process go-runtine to stop.
	close(f.stopCh)

	f.processorsLock.RLock()
	defer f.processorsLock.RUnlock()
	for _, p := range f.processors {
		f.shutdown(p)
	}

	f.processingWg.Wait()
	close(f.statCh)
}

// IsBucketOwner returns true if this Autoscaler pod is the owner of the given bucket.
func (f *Forwarder) IsBucketOwner(bkt string) bool {
	p := f.getProcessor(bkt)
	// The accept func is not nil iif this Autoscaler is the owner.
	return p != nil && p.accept != nil
}
