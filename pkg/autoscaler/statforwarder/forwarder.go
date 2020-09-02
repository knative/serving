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

	gorillawebsocket "github.com/gorilla/websocket"
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
	"knative.dev/pkg/system"
	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
	retryTimeout       = 3 * time.Second
	retryInterval      = 100 * time.Millisecond
)

// statProcessor is a function to process a single StatMessage.
type statProcessor func(sm asmetrics.StatMessage)

// bucketProcessor includes the information about how to process
// the StatMessage owned by a bucket.
type bucketProcessor struct {
	// holder is the HolderIdentity for a bucket from the Lease.
	holder string
	// conn is the WebSocket connection to the holder pod.
	conn *websocket.ManagedConnection
	// proc is the function to process the StatMessage owned by the bucket.
	proc statProcessor
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
	}

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

		if p := f.getProcessor(l.Name); p != nil && p.holder == *l.Spec.HolderIdentity {
			// Already up-to-date.
			return false
		}

		return true
	}
}

func (f *Forwarder) leaseUpdated(obj interface{}) {
	l := obj.(*coordinationv1.Lease)
	ns, n := l.Namespace, l.Name

	if l.Spec.HolderIdentity == nil {
		// This shouldn't happen as we filter it out when queueing.
		// Add a nil point check for safety.
		f.logger.Warnf("Lease %s/%s with nil HolderIdentity got enqueued", ns, n)
		return
	}

	holder := *l.Spec.HolderIdentity
	f.setProcessor(n, f.createProcessor(n, holder))

	if holder != f.selfIP {
		// Skip creating/updating Service and Endpoints if not the leader.
		return
	}

	// TODO(yanweiguo): Better handling these errors instead of just logging.
	if err := f.createService(ns, n); err != nil {
		f.logger.Errorf("Failed to create Service for Lease %s/%s: %v", ns, n, err)
		return
	}
	f.logger.Infof("Created Service for Lease %s/%s", ns, n)

	if err := f.createOrUpdateEndpoints(ns, n); err != nil {
		f.logger.Errorf("Failed to create Endpoints for Lease %s/%s: %v", ns, n, err)
		return
	}
	f.logger.Infof("Created Endpoints for Lease %s/%s", ns, n)
}

func (f *Forwarder) createService(ns, n string) error {
	var lastErr error
	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		_, lastErr = f.kc.CoreV1().Services(ns).Create(&v1.Service{
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
		})

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
func (f *Forwarder) createOrUpdateEndpoints(ns, n string) error {
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
		if _, lastErr = f.kc.CoreV1().Endpoints(ns).Update(want); lastErr != nil {
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
		_, lastErr = f.kc.CoreV1().Endpoints(ns).Create(&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Subsets: wantSubsets,
		})
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

func (f *Forwarder) createProcessor(bkt, holder string) *bucketProcessor {
	if holder == f.selfIP {
		return &bucketProcessor{
			holder: holder,
			proc: func(sm asmetrics.StatMessage) {
				l := f.logger.With(zap.String("revision", sm.Key.String()))
				l.Debug("Accept stat as owner of bucket ", bkt)
				if f.accept != nil {
					f.accept(sm)
				}
			},
		}
	}

	// TODO(9235): Currently we use IP directly. This won't work if there
	// is mesh. Need to fall back to connection via Service.
	dns := fmt.Sprintf("ws://%s:%d", holder, autoscalerPort)
	f.logger.Info("Connecting to Autoscaler bucket at ", dns)
	c := websocket.NewDurableSendingConnection(dns, f.logger)
	return &bucketProcessor{
		holder: holder,
		conn:   c,
		proc: func(sm asmetrics.StatMessage) {
			l := f.logger.With(zap.String("revision", sm.Key.String()))
			l.Debugf("Forward stat of bucket %s to the holder %s", bkt, holder)
			wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
			b, err := wsms.Marshal()
			if err != nil {
				l.Errorw("Error while marshaling stats", zap.Error(err))
				return
			}

			if err := c.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
				l.Errorw("Error while sending stats", zap.Error(err))
			}
		},
	}
}

// Process calls Forwarder.accept if the pod where this Forwarder is running is the owner
// of the given StatMessage. Otherwise it forwards the given StatMessage to the right
// owner pod. If it can't find the owner, it drops the StatMessage.
func (f *Forwarder) Process(sm asmetrics.StatMessage) {
	rev := sm.Key.String()
	l := f.logger.With(zap.String("revision", rev))
	bkt := f.bs.Owner(rev)

	p := f.getProcessor(bkt)
	if p == nil {
		l.Warnf("Can't find the owner for Rev %s. Dropping its stat.", rev)
		return
	}

	p.proc(sm)
}

// Cancel is the function to call when terminating a Forwarder.
func (f *Forwarder) Cancel() {
	for _, p := range f.processors {
		if p.conn != nil {
			p.conn.Shutdown()
		}
	}
}
