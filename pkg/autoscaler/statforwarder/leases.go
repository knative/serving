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

	"go.uber.org/zap"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	leaseinformer "knative.dev/pkg/client/injection/kube/informers/coordination/v1/lease"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/autoscaler/bucket"
)

// LeaseBasedProcessor tracks leases and decodes the holder's identity in order to set the
// appropriate "processor" on the Forwarder.
func LeaseBasedProcessor(ctx context.Context, f *Forwarder, accept statProcessor) error {
	selfIP, err := bucket.PodIP()
	if err != nil {
		return err
	}
	endpointsInformer := endpointsinformer.Get(ctx)
	lt := &leaseTracker{
		logger:          logging.FromContext(ctx),
		selfIP:          selfIP,
		bs:              f.bs,
		kc:              kubeclient.Get(ctx),
		endpointsLister: endpointsInformer.Lister(),
		id2ip:           make(map[string]string),
		accept:          accept,
		fwd:             f,
	}

	leaseInformer := leaseinformer.Get(ctx)
	leaseInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: lt.filterFunc(system.Namespace()),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    lt.leaseUpdated,
			UpdateFunc: controller.PassNew(lt.leaseUpdated),
			// TODO(yanweiguo): Set up DeleteFunc.
		},
	})

	return nil
}

// leaseTracker monitors lease resources to update the Forwarder's processor configuration(s)
// with the appropriate owner's stats endpoint.  When we own the lease, a localProcessor is
// used, and when someone else owns the lease a remoteProcessor is used with their stats
// endpoint.
type leaseTracker struct {
	logger *zap.SugaredLogger
	selfIP string
	// bs is the BucketSet including all Autoscaler buckets.
	bs *hash.BucketSet

	kc              kubernetes.Interface
	endpointsLister corev1listers.EndpointsLister

	// id2ip stores the IP extracted from the holder identity to avoid
	// string split each time.
	id2ip map[string]string

	// `accept` is the function to process a StatMessage which doesn't need
	// to be forwarded.
	accept statProcessor

	fwd *Forwarder
}

func (f *leaseTracker) extractIP(holder string) (string, error) {
	if ip, ok := f.id2ip[holder]; ok {
		return ip, nil
	}

	_, ip, err := bucket.ExtractPodNameAndIP(holder)
	if err == nil {
		f.id2ip[holder] = ip
	}
	return ip, err
}

func (f *leaseTracker) filterFunc(namespace string) func(interface{}) bool {
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
		holder := *l.Spec.HolderIdentity
		_, err := f.extractIP(holder)
		if err != nil {
			f.logger.Warn("Found invalid Lease holder identify ", holder)
			return false
		}
		if p := f.fwd.getProcessor(l.Name); p != nil && p.is(holder) {
			return false
		}

		return true
	}
}

func (f *leaseTracker) leaseUpdated(obj interface{}) {
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
	if p := f.fwd.getProcessor(n); p != nil {
		p.shutdown()
	}

	holder := *l.Spec.HolderIdentity
	// The map should have the IP because of the operations in the filter when queueing.
	ip, ok := f.id2ip[holder]
	if !ok {
		// This shouldn't happen because we store the value into the map in the filter
		// when queueing. Add a log here in case something unexpected happens.
		f.logger.Warn("IP not found in cached map for ", holder)
		return
	}

	if ip != f.selfIP {
		f.fwd.setProcessor(n, newForwardProcessor(f.logger.With(zap.String("bucket", n)), n, holder,
			fmt.Sprintf("ws://%s:%d", ip, autoscalerPort),
			fmt.Sprintf("ws://%s.%s.%s", n, ns, svcURLSuffix)))

		// Skip creating/updating Service and Endpoints if not the leader.
		return
	}

	f.fwd.setProcessor(n, &localProcessor{
		bkt:    n,
		holder: holder,
		logger: f.logger.With(zap.String("bucket", n)),
		accept: f.accept,
	})

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

func (f *leaseTracker) createService(ctx context.Context, ns, n string) error {
	var lastErr error
	if err := wait.PollImmediate(retryInterval, retryTimeout, func() (bool, error) {
		_, lastErr = f.kc.CoreV1().Services(ns).Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{
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
func (f *leaseTracker) createOrUpdateEndpoints(ctx context.Context, ns, n string) error {
	wantSubsets := []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{
			IP: f.selfIP,
		}},
		Ports: []corev1.EndpointPort{{
			Name:     autoscalerPortName,
			Port:     autoscalerPort,
			Protocol: corev1.ProtocolTCP,
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
		_, lastErr = f.kc.CoreV1().Endpoints(ns).Create(ctx, &corev1.Endpoints{
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
