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
	"sync"

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
	"k8s.io/client-go/util/retry"
	
	leaseinformer "knative.dev/pkg/client/injection/kube/informers/coordination/v1/lease"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
)

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
)

// Forwarder does the following things:
// 1. Watches the change of Leases for Autoscaler buckets. Stores the
//    Lease -> IP mapping.
// 2. Creates/updates the corresponding K8S Service and Endpoints.
// 3. Can be used to forward the metrics falling in a bucket based on
//    the holder IP. (This is a TODO)
type Forwarder struct {
	selfIP          string
	logger          *zap.SugaredLogger
	kc              kubernetes.Interface
	endpointsLister corev1listers.EndpointsLister
	// bs is the BucketSet including all Autoscaler buckets.
	bs *hash.BucketSet
	// Lock for leaseHolders.
	leaseLock sync.RWMutex
	// leaseHolders stores the Lease -> IP relationships.
	leaseHolders map[string]string
}

// New creates a new Forwarder.
func New(ctx context.Context, logger *zap.SugaredLogger, kc kubernetes.Interface, selfIP string, bs *hash.BucketSet) *Forwarder {
	ns := system.Namespace()
	bkts := bs.Buckets()
	endpointsInformer := endpointsinformer.Get(ctx)
	f := Forwarder{
		selfIP:          selfIP,
		logger:          logger,
		kc:              kc,
		endpointsLister: endpointsInformer.Lister(),
		bs:              bs,
		leaseHolders:    make(map[string]string, len(bkts)),
	}

	leaseInformer := leaseinformer.Get(ctx)
	leaseInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.NamespaceFilterFunc(ns),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    f.leaseUpdated,
			UpdateFunc: controller.PassNew(f.leaseUpdated),
			// TODO(yanweiguo): Set up DeleteFunc.
		},
	})

	return &f
}

func (f *Forwarder) leaseUpdated(obj interface{}) {
	l := obj.(*coordinationv1.Lease)
	ns, n := l.Namespace, l.Name

	if !f.bs.HasBucket(n) {
		// Skip this Lease as it is not for Autoscaler.
		return
	}

	if l.Spec.HolderIdentity == nil || *l.Spec.HolderIdentity == "" {
		f.logger.Debugf("HolderIdentity not found for Lease %s/%s", ns, n)
		return
	}

	identity := *l.Spec.HolderIdentity
	if h, ok := f.getHolder(n); ok && h == identity {
		// Already up-to-date.
		return
	}

	f.setHolder(n, identity)

	if identity != f.selfIP {
		// Skip creating/updating Service and Endpoints as not the holder.
		return
	}

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
	return wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
		_, err := f.kc.CoreV1().Services(ns).Create(&v1.Service{
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

		if apierrs.IsAlreadyExists(err) {
			return true, nil
		}

		return err == nil, err
	})
}

// createOrUpdateEndpoints creates a Endpoints object with the given namespace and
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
	if err := wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
		e, err := f.endpointsLister.Endpoints(ns).Get(n)

		if apierrs.IsNotFound(err) {
			exists = false
			return true, nil
		}

		if err != nil {
			return false, err
		}

		if equality.Semantic.DeepEqual(wantSubsets, e.Subsets) {
			return true, nil
		}

		want := e.DeepCopy()
		want.Subsets = wantSubsets
		if _, err := f.kc.CoreV1().Endpoints(ns).Update(want); err != nil {
			return false, err
		}

		f.logger.Infof("Bucket Endpoints %s updated with IP %s", n, f.selfIP)
		return true, nil
	}); err != nil {
		return err
	}

	if exists {
		return nil
	}
	return wait.ExponentialBackoff(retry.DefaultRetry, func() (bool, error) {
		_, err := f.kc.CoreV1().Endpoints(ns).Create(&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      n,
				Namespace: ns,
			},
			Subsets: wantSubsets,
		})
		return err == nil, err
	})
}

func (f *Forwarder) getHolder(name string) (string, bool) {
	f.leaseLock.RLock()
	defer f.leaseLock.RUnlock()
	result, ok := f.leaseHolders[name]
	return result, ok
}

func (f *Forwarder) setHolder(name, holder string) {
	f.leaseLock.Lock()
	defer f.leaseLock.Unlock()
	f.leaseHolders[name] = holder
}
