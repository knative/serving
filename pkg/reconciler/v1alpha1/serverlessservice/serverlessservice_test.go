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

package serverlessservice

import (
	"fmt"
	"testing"
	"time"

	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/networking"
	nv1a1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	rpkg "github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/serverlessservice/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/serverlessservice/resources/names"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestNewController(t *testing.T) {
	defer ClearAllLoggers()

	kubeClient := fakekubeclientset.NewSimpleClientset()
	kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	servingClient := fakeclientset.NewSimpleClientset()
	servingInformer := informers.NewSharedInformerFactory(servingClient, 0)

	sksInformer := servingInformer.Networking().V1alpha1().ServerlessServices()
	endpointsInformer := kubeInformer.Core().V1().Endpoints()
	servicesInformer := kubeInformer.Core().V1().Services()

	opt := rpkg.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		Logger:           TestLogger(t),
	}
	c := NewController(opt, sksInformer, servicesInformer, endpointsInformer)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key, Part I",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "bad workqueue key, Part II",
		Key:                     "too-few-parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name: "steady state",
		Key:  "steady/state",
		Objects: []runtime.Object{
			sks("steady", "state", markHappy, withPubService),
			svcpub("steady", "state"),
			svcpriv("steady", "state"),
			endpointspub("steady", "state", WithSubsets),
			endpointspriv("steady", "state", WithSubsets),
		},
	}, {
		Name: "pod change",
		Key:  "pod/change",
		Objects: []runtime.Object{
			sks("pod", "change", markHappy, withPubService),
			svcpub("pod", "change"),
			svcpriv("pod", "change"),
			endpointspub("pod", "change", WithSubsets),
			endpointspriv("pod", "change", withOtherSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: endpointspub("pod", "change", withOtherSubsets),
		}},
	}, {
		Name: "user changes priv svc",
		Key:  "private/svc-change",
		Objects: []runtime.Object{
			sks("private", "svc-change", markHappy, withPubService),
			svcpub("private", "svc-change"),
			svcpriv("private", "svc-change", withTimeSelector),
			endpointspub("private", "svc-change", withOtherSubsets),
			endpointspriv("private", "svc-change", WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpriv("private", "svc-change")}, {
			Object: endpointspub("private", "svc-change", WithSubsets)}},
	}, {
		Name: "user changes public svc",
		Key:  "public/svc-change",
		Objects: []runtime.Object{
			sks("public", "svc-change", markHappy, withPubService),
			svcpub("public", "svc-change", withTimeSelector),
			svcpriv("public", "svc-change"),
			endpointspub("public", "svc-change", WithSubsets),
			endpointspriv("public", "svc-change", WithSubsets),
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: svcpub("public", "svc-change"),
		}},
	}, {
		Name: "OnCreate-deployment-exists",
		Key:  "on/cde",
		Objects: []runtime.Object{
			sks("on", "cde"),
			// This "has" to pre-exist, otherwise I can't populate it with subsets.
			endpointspriv("on", "cde", WithSubsets),
		},
		WantCreates: []metav1.Object{
			svcpriv("on", "cde"),
			svcpub("on", "cde"),
			endpointspub("on", "cde", WithSubsets),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks("on", "cde", markHappy, withPubService),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cde"`),
		},
	}, {
		Name: "OnCreate-no-eps",
		Key:  "on/cneps",
		Objects: []runtime.Object{
			sks("on", "cneps"),
			endpointspriv("on", "cneps"),
		},
		WantCreates: []metav1.Object{
			svcpriv("on", "cneps"),
			svcpub("on", "cneps"),
			endpointspub("on", "cneps"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks("on", "cneps", markNoEndpoints, withPubService),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", `Successfully updated ServerlessService "on/cneps"`),
		},
	}, {
		Name:    "svc-fail-priv",
		Key:     "svc/fail",
		WantErr: true,
		Objects: []runtime.Object{
			sks("svc", "fail"),
			endpointspriv("svc", "fail"),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantCreates: []metav1.Object{
			svcpriv("svc", "fail"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create services"),
		},
	}, {
		Name:    "svc-fail-pub",
		Key:     "svc/fail2",
		WantErr: true,
		Objects: []runtime.Object{
			sks("svc", "fail2"),
			svcpriv("svc", "fail2"),
			endpointspriv("svc", "fail2"),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "services"),
		},
		WantCreates: []metav1.Object{
			svcpub("svc", "fail2"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create services"),
		},
	}, {
		Name:    "eps-fail-pub",
		Key:     "eps/fail3",
		WantErr: true,
		Objects: []runtime.Object{
			sks("eps", "fail3"),
			svcpriv("eps", "fail3"),
			endpointspriv("eps", "fail3"),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "endpoints"),
		},
		WantCreates: []metav1.Object{
			svcpub("eps", "fail3"),
			endpointspub("eps", "fail3"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "InternalError: inducing failure for create endpoints"),
		},
	}, {
		Name:    "update-sks-fail",
		Key:     "update-sks/fail4",
		WantErr: true,
		Objects: []runtime.Object{
			sks("update-sks", "fail4", withPubService),
			svcpub("update-sks", "fail4"),
			svcpriv("update-sks", "fail4"),
			endpointspub("update-sks", "fail4", WithSubsets),
			endpointspriv("update-sks", "fail4", WithSubsets),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "serverlessservices"),
		},
		// We still record update, but it fails.
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: sks("update-sks", "fail4", markHappy, withPubService),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "UpdateFailed", "Failed to update status: inducing failure for update serverlessservices"),
		},
	}}

	defer ClearAllLoggers()
	table.Test(t, MakeFactory(func(listers *Listers, opt rpkg.Options) controller.Reconciler {
		return &reconciler{
			Base:            rpkg.NewBase(opt, controllerAgentName),
			sksLister:       listers.GetServerlessServiceLister(),
			serviceLister:   listers.GetK8sServiceLister(),
			endpointsLister: listers.GetEndpointsLister(),
		}
	}))
}

// TODO(vagababov): decide if should be moved to functional.go
type SKSOption func(sks *nv1a1.ServerlessService)

func withGeneration(g int64) SKSOption {
	return func(sks *nv1a1.ServerlessService) {
		sks.Generation = g
	}
}

// withOtherSubsets uses different IP set than functional::withSubsets.
func withOtherSubsets(ep *corev1.Endpoints) {
	ep.Subsets = []corev1.EndpointSubset{{
		Addresses: []corev1.EndpointAddress{{IP: "127.0.0.2"}},
	}}
}

func withPubService(sks *nv1a1.ServerlessService) {
	sks.Status.ServiceName = names.PublicService(sks)
}

func markHappy(sks *nv1a1.ServerlessService) {
	sks.Status.MarkEndpointsPopulated()
}

func markNoEndpoints(sks *nv1a1.ServerlessService) {
	sks.Status.MarkEndpointsUnknown("NoHealthyBackends")
}

func withSelector(sel map[string]string) SKSOption {
	return func(sks *nv1a1.ServerlessService) {
		sks.Spec.Selector = sel
	}
}

func withProtocol(p networking.ProtocolType) SKSOption {
	return func(sks *nv1a1.ServerlessService) {
		sks.Spec.ProtocolType = p
	}
}

func sks(ns, name string, so ...SKSOption) *nv1a1.ServerlessService {
	s := &nv1a1.ServerlessService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       "test-uid",
		},
		Spec: nv1a1.ServerlessServiceSpec{
			Mode:         nv1a1.SKSOperationModeServe,
			ProtocolType: networking.ProtocolHTTP1,
			Selector: map[string]string{
				"label": "value",
			},
		},
	}
	for _, opt := range so {
		opt(s)
	}
	return s
}

func svcpub(namespace, name string, so ...K8sServiceOption) *corev1.Service {
	sks := sks(namespace, name)
	s := resources.MakePublicService(sks)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func svcpriv(namespace, name string, so ...K8sServiceOption) *corev1.Service {
	sks := sks(namespace, name)
	s := resources.MakePrivateService(sks)
	for _, opt := range so {
		opt(s)
	}
	return s
}

func endpointspriv(namespace, name string, eo ...EndpointsOption) *corev1.Endpoints {
	service := svcpriv(namespace, name)
	ep := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: service.Namespace,
			Name:      service.Name,
		},
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

func endpointspub(namespace, name string, eo ...EndpointsOption) *corev1.Endpoints {
	service := svcpub(namespace, name)
	ep := &corev1.Endpoints{
		ObjectMeta: *service.ObjectMeta.DeepCopy(),
	}
	for _, opt := range eo {
		opt(ep)
	}
	return ep
}

func withTimeSelector(svc *corev1.Service) {
	svc.Spec.Selector = map[string]string{"pod-x": fmt.Sprintf("a-%d", time.Now().UnixNano())}
}
