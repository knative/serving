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
	"strings"
	"sync"

	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"
	coordinationv1 "k8s.io/api/coordination/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	coordinationinformerv1 "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/client/injection/kube/informers/factory"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

func init() {
	injection.Default.RegisterInformer(withInformer)
}

// Key is used for associating the Informer inside the context.Context.
type Key struct{}

func withInformer(ctx context.Context) (context.Context, controller.Informer) {
	f := factory.Get(ctx)
	inf := f.Coordination().V1().Leases()
	return context.WithValue(ctx, Key{}, inf), inf.Informer()
}

// Get extracts the typed informer from the context.
func get(ctx context.Context) coordinationinformerv1.LeaseInformer {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		logging.FromContext(ctx).Panic(
			"Unable to fetch k8s.io/client-go/informers/coordination/v1.LeaseInformer from context.")
	}
	return untyped.(coordinationinformerv1.LeaseInformer)
}

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
)

// statProcessor is a function to process a single StatMessage.
type statProcessor func(sm asmetrics.StatMessage)

// Forwarder is
type Forwarder struct {
	selfIP          string
	logger          *zap.SugaredLogger
	kc              kubernetes.Interface
	serviceLister   corev1listers.ServiceLister
	endpointsLister corev1listers.EndpointsLister
	bs              *hash.BucketSet
	// accept is the function to process a StatMessage if it doesn't need
	// to be forwarded.
	accept statProcessor
	// Lock for b2IP.
	lock  sync.RWMutex
	b2IP  map[string]string
	wsMap map[string]*websocket.ManagedConnection
}

func New(ctx context.Context, kc kubernetes.Interface, selfIP string, bs *hash.BucketSet, accept statProcessor, logger *zap.SugaredLogger) *Forwarder {
	ns := system.Namespace()
	f := Forwarder{
		selfIP:          selfIP,
		logger:          logger,
		kc:              kc,
		serviceLister:   serviceinformer.Get(ctx).Lister(),
		endpointsLister: endpointsinformer.Get(ctx).Lister(),
		bs:              bs,
		accept:          accept,
		b2IP:            make(map[string]string),
	}

	bkts := bs.Buckets()
	f.wsMap = make(map[string]*websocket.ManagedConnection, len(bkts))
	for _, b := range bkts {
		dns := fmt.Sprintf("ws://%s.%s.svc.%s:%d", b.Name(), ns, network.GetClusterDomainName(), autoscalerPort)
		logger.Info("Connecting to Autoscaler bucket at ", dns)
		statSink := websocket.NewDurableSendingConnection(dns, logger)
		f.wsMap[b.Name()] = statSink
	}

	leaseInformer := get(ctx)
	leaseInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.NamespaceFilterFunc(ns),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    f.leaseUpdated,
			UpdateFunc: controller.PassNew(f.leaseUpdated),
			DeleteFunc: f.leaseDeleted,
		},
	})
	return &f
}

func (f *Forwarder) leaseUpdated(obj interface{}) {
	l := obj.(*coordinationv1.Lease)
	ns, n := l.Namespace, l.Name

	if !f.isBucketLease(n) {
		// f.logger.Debugf("Skip Lease %s, not a valid one", n)
		return
	}

	identity := *l.Spec.HolderIdentity
	if identity == "" {
		f.logger.Debugf("HolderIdentity not found for Lease %s/%s", ns, n)
		return
	}
	arr := strings.Split(identity, "/")
	leader := arr[0]
	if f.getIP(l.Name)[0] == leader {
		// Already up-to-date.
		return
	}

	f.setIP(l.Name, identity)

	if leader != f.selfIP {
		f.logger.Debugf("Skip updating Endpoints %s, not the leader", n)
		return
	}

	if err := f.createService(ns, n); err != nil {
		f.logger.Errorf("Failed to create Service for Lease %s/%s: %v", ns, n, err)
	}

	if err := f.createOrUpdateEndpoints(ns, n); err != nil {
		f.logger.Errorf("Failed to create Endpoints for Lease %s/%s: %v", ns, n, err)
	}
}

func (f *Forwarder) isBucketLease(n string) bool {
	for _, bn := range f.bs.BucketList() {
		if bn == n {
			return true
		}
	}
	return false
}

func (f *Forwarder) createService(ns, name string) error {
	_, err := f.serviceLister.Services(ns).Get(name)
	if err == nil {
		// Already created.
		return nil
	}

	if !apierrs.IsNotFound(err) {
		return err
	}

	_, err = f.kc.CoreV1().Services(ns).Create(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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

	return err
}

func (f *Forwarder) createOrUpdateEndpoints(ns, name string) error {
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

	e, err := f.endpointsLister.Endpoints(ns).Get(name)
	if err == nil {
		reconciler.RetryUpdateConflicts(func(attempts int) (err error) {
			if attempts > 0 {
				e, err = f.endpointsLister.Endpoints(ns).Get(name)
				if err != nil {
					return err
				}
			}

			if equality.Semantic.DeepEqual(wantSubsets, e.Subsets) {
				return nil
			}

			want := e.DeepCopy()
			want.Subsets = wantSubsets
			if _, err := f.kc.CoreV1().Endpoints(ns).Update(want); err != nil {
				return err
			}

			f.logger.Infof("Bucket Endpoints %s updated with IP %s", name, f.selfIP)
			return nil
		})
		return nil
	}

	if !apierrs.IsNotFound(err) {
		return err
	}

	_, err = f.kc.CoreV1().Endpoints(ns).Create(&v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Subsets: wantSubsets,
	})

	return err
}

func (f *Forwarder) leaseDeleted(obj interface{}) {
	l := obj.(*coordinationv1.Lease)
	ns, n := l.Namespace, l.Name

	if !f.isBucketLease(l.Name) {
		f.logger.Debugf("Skip Lease %s/%s, not a valid one", ns, n)
		return
	}

	f.deleteIP(n)
	if *l.Spec.HolderIdentity != f.selfIP {
		f.logger.Debugf("Skip updating Lease %s/%s, not the leader", ns, n)
		return
	}

	if err := f.deleteService(ns, n); err != nil {
		f.logger.Errorf("Failed to delete Service for Endpoints %s: %v", n, err)
	}
}

func (f *Forwarder) deleteService(ns, name string) error {
	if _, err := f.serviceLister.Services(ns).Get(name); err != nil {
		if apierrs.IsNotFound(err) {
			// Not exist.
			return nil
		}

		return err
	}

	return f.kc.CoreV1().Services(ns).Delete(name, &metav1.DeleteOptions{})
}

func (f *Forwarder) Process(sm asmetrics.StatMessage) {
	l := f.logger.With(zap.String("rev", sm.Key.String()))
	owner := f.bs.Owner(sm.Key.String())
	arr := f.getIP(owner)
	if arr[0] == f.selfIP {
		l.Infof("## accept rev %s\n", sm.Key.String())
		f.accept(sm)
		return
	}

	if ws, ok := f.wsMap[owner]; ok {
		l.Infof("## forward rev %s to %s, owned by %s", sm.Key.String(), owner, arr[1])
		wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
		b, err := wsms.Marshal()
		if err != nil {
			l.Errorw("Error while marshaling stats", zap.Error(err))
			return
		}

		if err := ws.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
			l.Errorw("Error while sending stats", zap.Error(err))
		}
	} else {
		l.Warnf("Can't find the owner of key %s, dropping the stat.", sm.Key.String())
	}
}

func (f *Forwarder) Cancel() {
	for _, ws := range f.wsMap {
		ws.Shutdown()
	}
}

func (f *Forwarder) getIP(name string) []string {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return strings.Split(f.b2IP[name], "/")
}

func (f *Forwarder) setIP(name, IP string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.b2IP[name] = IP
	f.logger.Infof("B2IP mapping changed: %v %v %v", name, IP, f.b2IP)
}

func (f *Forwarder) deleteIP(name string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	delete(f.b2IP, name)
}
