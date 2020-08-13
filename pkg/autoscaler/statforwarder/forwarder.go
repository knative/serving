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
	"encoding/json"
	"fmt"
	"log"
	"sync"

	gorillawebsocket "github.com/gorilla/websocket"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/pkg/websocket"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
)

const (
	// The port on which autoscaler WebSocket server listens.
	autoscalerPort     = 8080
	autoscalerPortName = "http"
)

// statProcessor is a function to process a single StatMessage.
type statProcessor func(sm asmetrics.StatMessage)

type record struct {
	HolderIdentity string `json:"holderIdentity"`
}

// Forwarder is
type Forwarder struct {
	selfIP        string
	logger        *zap.SugaredLogger
	kc            kubernetes.Interface
	serviceLister corev1listers.ServiceLister
	bs            *hash.BucketSet
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
		selfIP:        selfIP,
		logger:        logger,
		kc:            kc,
		serviceLister: serviceinformer.Get(ctx).Lister(),
		bs:            bs,
		accept:        accept,
		b2IP:          make(map[string]string),
	}

	bkts := bs.Buckets()
	f.wsMap = make(map[string]*websocket.ManagedConnection, len(bkts))
	for _, b := range bkts {
		dns := fmt.Sprintf("ws://%s.%s.svc.%s:%d", b.Name(), ns, network.GetClusterDomainName(), autoscalerPort)
		logger.Info("Connecting to Autoscaler bucket at ", dns)
		statSink := websocket.NewDurableSendingConnection(dns, logger)
		f.wsMap[b.Name()] = statSink
	}

	endpointsInformer := endpointsinformer.Get(ctx)
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.NamespaceFilterFunc(ns),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    f.endpointsUpdated,
			UpdateFunc: controller.PassNew(f.endpointsUpdated),
		},
	})
	log.Println("## return")
	return &f
}

func (f *Forwarder) endpointsUpdated(obj interface{}) {
	log.Println("## update")
	e := obj.(*v1.Endpoints)

	if !f.isBucketEndpoints(e.Name) {
		f.logger.Debugf("Skip Endpoints %s, not a valid one", e.Name)
		return
	}

	v, ok := e.Annotations[resourcelock.LeaderElectionRecordAnnotationKey]
	if !ok {
		f.logger.Debugf("Annotation %s not found for Endpoints %s", resourcelock.LeaderElectionRecordAnnotationKey, e.Name)
		return
	}

	r := &record{}
	if err := json.Unmarshal([]byte(v), r); err != nil {
		f.logger.Errorw("Failed to parse annotation", zap.Error(err))
		return
	}

	f.setIP(e.Name, r.HolderIdentity)
	if r.HolderIdentity != f.selfIP {
		f.logger.Debugf("Skip updating Endpoints %s, not the leader", e.Name)
		return
	}

	if err := f.createService(e.Namespace, e.Name); err != nil {
		log.Printf("err %v\n", err)
		f.logger.Errorf("Failed to create Service for Endpoints %s: %v", e.Name, err)
	}

	// We expected there will be only one IP for the bucket Endpoints.
	if len(e.Subsets) > 0 && len(e.Subsets[0].Addresses) > 0 && e.Subsets[0].Addresses[0].IP == f.selfIP {
		f.logger.Debugf("Skip updating Endpoints %s, already has the desired IP", e.Name)
		return
	}

	wantSubsets := []v1.EndpointSubset{{
		Addresses: []v1.EndpointAddress{{
			IP: f.selfIP,
		}},
		Ports: []v1.EndpointPort{{
			Name: autoscalerPortName,
			Port: autoscalerPort,
		}}},
	}
	if !equality.Semantic.DeepEqual(wantSubsets, e.Subsets) {
		want := e.DeepCopy()
		want.Subsets = wantSubsets

		if _, err := f.kc.CoreV1().Endpoints(e.Namespace).Update(want); err != nil {
			f.logger.Errorf("Failed to update Endpoints %s: %v", e.Name, err)
		} else {
			f.logger.Info("Bucket Endpoints updated: ", e.Name)
		}
	}
}

func (f *Forwarder) isBucketEndpoints(n string) bool {
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

func (f *Forwarder) Process(sm asmetrics.StatMessage) {
	owner := f.bs.Owner(sm.Key.String())
	if f.getIP(owner) == f.selfIP {
		log.Printf("## accept rev %s\n", sm.Key.String())
		f.accept(sm)
		return
	}

	if ws, ok := f.wsMap[owner]; ok {
		log.Printf("## forward rev %s to %s\n", sm.Key.String(), owner)
		wsms := asmetrics.ToWireStatMessages([]asmetrics.StatMessage{sm})
		b, err := wsms.Marshal()
		if err != nil {
			f.logger.Errorw("Error while marshaling stats", zap.Error(err))
			return
		}

		if err := ws.SendRaw(gorillawebsocket.BinaryMessage, b); err != nil {
			f.logger.Errorw("Error while sending stats", zap.Error(err))
		}
	} else {
		f.logger.Warnf("Can't find the owner of key %s, dropping the stat.", sm.Key.String())
	}
}

func (f *Forwarder) Cancel() {
	for _, ws := range f.wsMap {
		ws.Shutdown()
	}
}

func (f *Forwarder) getIP(name string) string {
	f.lock.RLock()
	defer f.lock.RUnlock()
	return f.b2IP[name]
}

func (f *Forwarder) setIP(name, IP string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.b2IP[name] = IP
}
