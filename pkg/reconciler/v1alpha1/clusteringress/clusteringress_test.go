/*
Copyright 2018 The Knative Authors

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

package clusteringress

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/apis/istio/v1alpha3"
	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	sharedinformers "github.com/knative/pkg/client/informers/externalversions"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/resources"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
	"github.com/knative/serving/pkg/system"
	_ "github.com/knative/serving/pkg/system/testing"
)

const (
	originDomainInternal = "origin.ns.svc.cluster.local"
	newDomainInternal    = "custom.ns.svc.cluster.local"
)

var (
	originGateways = map[string]string{
		"gateway.knative-shared-gateway": originDomainInternal,
	}
	newGateways = map[string]string{
		"gateway.knative-ingress-gateway": newDomainInternal,
		"gateway.knative-shared-gateway":  originDomainInternal,
	}
)

var (
	ingressRules = []v1alpha1.ClusterIngressRule{{
		Hosts: []string{
			"domain.com",
			"test-route.test-ns.svc.cluster.local",
			"test-route.test-ns.svc",
			"test-route.test-ns",
		},
		HTTP: &v1alpha1.HTTPClusterIngressRuleValue{
			Paths: []v1alpha1.HTTPClusterIngressPath{{
				Splits: []v1alpha1.ClusterIngressBackendSplit{{
					ClusterIngressBackend: v1alpha1.ClusterIngressBackend{
						ServiceNamespace: "test-ns",
						ServiceName:      "test-service",
						ServicePort:      intstr.FromInt(80),
					},
					Percent: 100,
				}},
				Timeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
				Retries: &v1alpha1.HTTPRetry{
					PerTryTimeout: &metav1.Duration{Duration: v1alpha1.DefaultTimeout},
					Attempts:      v1alpha1.DefaultRetryCount,
				}},
			},
		},
	}}
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "key not found",
		Key:                     "foo/not-found",
		SkipNamespaceValidation: true,
	}, {
		Name:                    "skip ingress not matching class key",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			addAnnotations(ingress("no-virtualservice-yet", 1234),
				map[string]string{networking.IngressClassAnnotationKey: "fake-controller"}),
		},
	}, {
		Name:                    "create VirtualService matching ClusterIngress",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingress("no-virtualservice-yet", 1234),
		},
		WantCreates: []metav1.Object{
			resources.MakeVirtualService(ingress("no-virtualservice-yet", 1234),
				[]string{"knative-shared-gateway", "knative-ingress-gateway"}),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("no-virtualservice-yet", 1234,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: reconciler.GetK8sServiceFullname("knative-ingressgateway", "istio-system")},
						},
					},
					Conditions: duckv1alpha1.Conditions{{
						Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}, {
						Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}, {
						Type:     v1alpha1.ClusterIngressConditionReady,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created VirtualService %q", "no-virtualservice-yet"),
		},
		Key: "no-virtualservice-yet",
	}, {
		Name:                    "reconcile VirtualService to match desired one",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			ingress("reconcile-virtualservice", 1234),
			&v1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "reconcile-virtualservice",
					Namespace: system.Namespace(),
					Labels: map[string]string{
						networking.IngressLabelKey:     "reconcile-virtualservice",
						serving.RouteLabelKey:          "test-route",
						serving.RouteNamespaceLabelKey: "test-ns",
					},
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ingress("reconcile-virtualservice", 1234))},
				},
				Spec: v1alpha3.VirtualServiceSpec{},
			},
		},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeVirtualService(ingress("reconcile-virtualservice", 1234),
				[]string{"knative-shared-gateway", "knative-ingress-gateway"}),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingressWithStatus("reconcile-virtualservice", 1234,
				v1alpha1.IngressStatus{
					LoadBalancer: &v1alpha1.LoadBalancerStatus{
						Ingress: []v1alpha1.LoadBalancerIngressStatus{
							{DomainInternal: reconciler.GetK8sServiceFullname("knative-ingressgateway", "istio-system")},
						},
					},
					Conditions: duckv1alpha1.Conditions{{
						Type:     v1alpha1.ClusterIngressConditionLoadBalancerReady,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}, {
						Type:     v1alpha1.ClusterIngressConditionNetworkConfigured,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}, {
						Type:     v1alpha1.ClusterIngressConditionReady,
						Status:   corev1.ConditionTrue,
						Severity: "Error",
					}},
				},
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Updated", "Updated status for VirtualService %q/%q",
				system.Namespace(), "reconcile-virtualservice"),
		},
		Key: "reconcile-virtualservice",
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                 reconciler.NewBase(opt, controllerAgentName),
			virtualServiceLister: listers.GetVirtualServiceLister(),
			clusterIngressLister: listers.GetClusterIngressLister(),
			configStore: &testConfigStore{
				config: ReconcilerTestConfig(),
			},
		}
	}))
}

func addAnnotations(ing *v1alpha1.ClusterIngress, annos map[string]string) *v1alpha1.ClusterIngress {
	if ing.ObjectMeta.Annotations == nil {
		ing.ObjectMeta.Annotations = make(map[string]string)
	}

	for k, v := range annos {
		ing.ObjectMeta.Annotations[k] = v
	}
	return ing
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func ReconcilerTestConfig() *config.Config {
	return &config.Config{
		Istio: &config.Istio{
			IngressGateways: []config.Gateway{{
				GatewayName: "knative-shared-gateway",
				ServiceURL:  reconciler.GetK8sServiceFullname("knative-ingressgateway", "istio-system"),
			}, {
				GatewayName: "knative-ingress-gateway",
				ServiceURL:  reconciler.GetK8sServiceFullname("istio-ingressgateway", "istio-system"),
			}},
		},
	}
}

func ingressWithStatus(name string, generation int64, status v1alpha1.IngressStatus) *v1alpha1.ClusterIngress {
	return &v1alpha1.ClusterIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				serving.RouteLabelKey:          "test-route",
				serving.RouteNamespaceLabelKey: "test-ns",
			},
		},
		Spec: v1alpha1.IngressSpec{
			DeprecatedGeneration: generation,
			Rules:                ingressRules,
		},
		Status: status,
	}
}

func ingress(name string, generation int64) *v1alpha1.ClusterIngress {
	return ingressWithStatus(name, generation, v1alpha1.IngressStatus{})
}

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	kubeClient *fakekubeclientset.Clientset,
	sharedClient *fakesharedclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *controller.Impl,
	rclr *Reconciler,
	kubeInformer kubeinformers.SharedInformerFactory,
	sharedInformer sharedinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory,
	configMapWatcher *configmap.ManualWatcher) {

	kubeClient = fakekubeclientset.NewSimpleClientset()
	cms := []*corev1.ConfigMap{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.IstioConfigName,
				Namespace: system.Namespace(),
			},
			Data: originGateways,
		},
	}
	for _, cm := range configs {
		cms = append(cms, cm)
	}

	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}
	sharedClient = fakesharedclientset.NewSimpleClientset()
	servingClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	sharedInformer = sharedinformers.NewSharedInformerFactory(sharedClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)

	controller = NewController(
		reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		servingInformer.Networking().V1alpha1().ClusterIngresses(),
		sharedInformer.Networking().V1alpha3().VirtualServices(),
	)

	rclr = controller.Reconciler.(*Reconciler)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}

	return
}

func TestGlobalResyncOnUpdateGatewayConfigMap(t *testing.T) {
	_, _, servingClient, controller, _, _, sharedInformer, servingInformer, watcher := newTestSetup(t)

	stopCh := make(chan struct{})
	defer close(stopCh)

	h := NewHooks()

	// Check for ClusterIngress created as a signal that syncHandler ran
	h.OnUpdate(&servingClient.Fake, "clusteringresses", func(obj runtime.Object) HookResult {
		ci := obj.(*v1alpha1.ClusterIngress)
		t.Logf("clusteringress updated: %q", ci.Name)

		gateways := ci.Status.LoadBalancer.Ingress
		if len(gateways) != 1 {
			t.Logf("Unexpected gateways: %v", gateways)
			return HookIncomplete
		}
		expectedDomainInternal := newDomainInternal
		if gateways[0].DomainInternal != expectedDomainInternal {
			t.Logf("Expected gateway %q but got %q", expectedDomainInternal, gateways[0].DomainInternal)
			return HookIncomplete
		}

		return HookComplete
	})

	servingInformer.Start(stopCh)
	sharedInformer.Start(stopCh)
	if err := watcher.Start(stopCh); err != nil {
		t.Fatalf("failed to start cluster ingress manager: %v", err)
	}

	servingInformer.WaitForCacheSync(stopCh)
	sharedInformer.WaitForCacheSync(stopCh)

	go controller.Run(1, stopCh)

	ingress := ingressWithStatus("config-update", 1234,
		v1alpha1.IngressStatus{
			LoadBalancer: &v1alpha1.LoadBalancerStatus{
				Ingress: []v1alpha1.LoadBalancerIngressStatus{
					{DomainInternal: ""},
				},
			},
			Conditions: duckv1alpha1.Conditions{{
				Type:   v1alpha1.ClusterIngressConditionLoadBalancerReady,
				Status: corev1.ConditionTrue,
			}, {
				Type:   v1alpha1.ClusterIngressConditionNetworkConfigured,
				Status: corev1.ConditionTrue,
			}, {
				Type:   v1alpha1.ClusterIngressConditionReady,
				Status: corev1.ConditionTrue,
			}},
		},
	)
	ingressClient := servingClient.NetworkingV1alpha1().ClusterIngresses()

	// Create a ingress.
	ingressClient.Create(ingress)

	// Test changes in gateway config map. ClusterIngress should get updated appropriately.
	domainConfig := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.IstioConfigName,
			Namespace: system.Namespace(),
		},
		Data: newGateways,
	}
	watcher.OnChange(&domainConfig)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}
