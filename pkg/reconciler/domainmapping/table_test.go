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

package domainmapping

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgotesting "k8s.io/client-go/testing"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	networkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgnetwork "knative.dev/pkg/network"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	domainmappingreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1alpha1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
	"knative.dev/serving/pkg/reconciler/domainmapping/resources"

	"knative.dev/pkg/client/injection/ducks/duck/v1/addressable"
	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
)

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "first reconcile",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			ksvc("default", "target", "the-target-svc.url", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
			resources.MakeIngress(domainMapping("default", "first-reconcile.com", withRef("default", "target")), "the-target-svc.url", "the-ingress-class"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "first reconcile, ref does not exist",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceNotResolved(`services.serving.knative.dev "target" not found`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `resolving reference: services.serving.knative.dev "target" not found`),
		},
	}, {
		Name: "first reconcile, ref has a path",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			ksvc("default", "target", "the-target-svc.url", "path"),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceNotResolved(`resolved URI "//the-target-svc.url/path" contains a path`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `resolved URI "//the-target-svc.url/path" contains a path`),
		},
	}, {
		Name: "first reconcile, pre-owned domain claim",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			ksvc("default", "target", "the-target-svc.url", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withIngressNotConfigured,
				withDomainClaimed,
				withReferenceResolved,
			),
		}},
		WantCreates: []runtime.Object{
			resources.MakeIngress(domainMapping("default", "first-reconcile.com", withRef("default", "target")), "the-target-svc.url", "the-ingress-class"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "first reconcile, cant claim domain",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		SkipNamespaceValidation: true, // allow (attempted) creation of ClusterDomainClaim.
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "clusterdomainclaims"),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
			),
		}},
		WantCreates: []runtime.Object{
			// this is the clusterdomainclaim create that we induce failure on.
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to create ClusterDomainClaim: inducing failure for create clusterdomainclaims`),
		},
	}, {
		Name: "first reconcile, non-owned domainclaim",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(
				domainMapping("default", "first-reconcile.com", withRef("default", "target"),
					withUID("some-other-uid"),
				),
			),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimNotOwned,
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", `domain mapping: "first-reconcile.com" does not own matching cluster domain claim`),
		},
	}, {
		Name: "reconcile with ingressClass annotation",
		Key:  "default/ingressclass.first-reconcile.com",
		Objects: []runtime.Object{
			ksvc("default", "target", "the-target-svc.url", ""),
			domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"),
				withAnnotations(map[string]string{
					networking.IngressClassAnnotationKey: "overridden-ingress-class",
				}),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingressclass.first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "ingressclass.first-reconcile.com"),
				withAddress("http", "ingressclass.first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
				withAnnotations(map[string]string{
					networking.IngressClassAnnotationKey: "overridden-ingress-class",
				}),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"))),
			resources.MakeIngress(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target")),
				"the-target-svc.url", "overridden-ingress-class"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "ingressclass.first-reconcile.com"),
		},
	}, {
		Name: "reconcile changed ref",
		Key:  "default/ingress-exists.org",
		Objects: []runtime.Object{
			ksvc("default", "changed", "the-target-svc.url", ""),
			domainMapping("default", "ingress-exists.org", withRef("default", "changed")),
			resources.MakeIngress(domainMapping("default", "ingress-exists.org", withRef("default", "target")), "the-target-svc.url", "the-ingress-class"),
			resources.MakeDomainClaim(domainMapping("default", "ingress-exists.org", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-exists.org",
				withRef("default", "changed"),
				withURL("http", "ingress-exists.org"),
				withAddress("http", "ingress-exists.org"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingress(domainMapping("default", "ingress-exists.org", withRef("default", "changed")), "the-ingress-class"),
		}},
	}, {
		Name: "reconcile failed ingress",
		Key:  "default/ingress-failed.me",
		Objects: []runtime.Object{
			ksvc("default", "failed", "the-target-svc.url", ""),
			domainMapping("default", "ingress-failed.me",
				withRef("default", "failed"),
				withURL("http", "ingress-failed.me"),
				withAddress("http", "ingress-failed.me"),
			),
			ingress(domainMapping("default", "ingress-failed.me", withRef("default", "failed")), "the-ingress-class",
				WithLoadbalancerFailed("fell over", "hurt myself"),
			),
			resources.MakeDomainClaim(domainMapping("default", "ingress-failed.me", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-failed.me",
				withRef("default", "failed"),
				withURL("http", "ingress-failed.me"),
				withAddress("http", "ingress-failed.me"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-failed.me"), "", WithLoadbalancerFailed("fell over", "hurt myself")).Status),
			),
		}},
	}, {
		Name: "reconcile unknown ingress",
		Key:  "default/ingress-unknown.me",
		Objects: []runtime.Object{
			ksvc("default", "unknown", "the-target-svc.url", ""),
			domainMapping("default", "ingress-unknown.me", withRef("default", "unknown"),
				withRef("default", "unknown"),
				withURL("http", "ingress-unknown.me"),
				withAddress("http", "ingress-unknown.me"),
			),
			ingress(domainMapping("default", "ingress-unknown.me", withRef("default", "unknown")), "the-ingress-class",
				withIngressNotReady,
			),
			resources.MakeDomainClaim(domainMapping("default", "ingress-unknown.me", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-unknown.me",
				withRef("default", "unknown"),
				withURL("http", "ingress-unknown.me"),
				withAddress("http", "ingress-unknown.me"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-unknown.me"), "", withIngressNotReady).Status),
			),
		}},
	}, {
		Name: "reconcile ready ingress",
		Key:  "default/ingress-ready.me",
		Objects: []runtime.Object{
			ksvc("default", "ready", "the-target-svc.url", ""),
			domainMapping("default", "ingress-ready.me",
				withRef("default", "ready"),
				withURL("http", "ingress-ready.me"),
				withAddress("http", "ingress-ready.me"),
			),
			ingress(domainMapping("default", "ingress-ready.me", withRef("default", "ready")), "the-ingress-class",
				withIngressReady,
			),
			resources.MakeDomainClaim(domainMapping("default", "ingress-ready.me", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-ready.me",
				withRef("default", "ready"),
				withURL("http", "ingress-ready.me"),
				withAddress("http", "ingress-ready.me"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-ready.me"), "", withIngressReady).Status),
			),
		}},
	}, {
		Name: "fail ingress creation",
		Key:  "default/cantcreate.this",
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "ingresses"),
		},
		Objects: []runtime.Object{
			ksvc("default", "cantcreate", "the-target-svc.url", ""),
			domainMapping("default", "cantcreate.this",
				withRef("default", "cantcreate"),
				withURL("http", "cantcreate.this"),
				withAddress("http", "cantcreate.this"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withGeneration(1),
			),
			resources.MakeDomainClaim(domainMapping("default", "cantcreate.this", withRef("default", "target"))),
		},
		WantErr: true,
		WantCreates: []runtime.Object{
			// This is the create which we will cause to fail.
			ingress(domainMapping("default", "cantcreate.this", withRef("default", "cantcreate")), "the-ingress-class"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "cantcreate.this",
				withRef("default", "cantcreate"),
				withURL("http", "cantcreate.this"),
				withAddress("http", "cantcreate.this"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withIngressNotConfigured,
				withGeneration(1),
				withObservedGeneration,
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Ingress: inducing failure for create ingresses"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create Ingress: inducing failure for create ingresses"),
		},
	}, {
		Name: "fail ingress update",
		Key:  "default/cantupdate.this",
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("update", "ingresses"),
		},
		Objects: []runtime.Object{
			ksvc("default", "cantupdate", "the-target-svc.url", ""),
			domainMapping("default", "cantupdate.this",
				withRef("default", "cantupdate"),
				withURL("http", "cantupdate.this"),
				withAddress("http", "cantupdate.this"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withGeneration(1),
			),
			ingress(domainMapping("default", "cantupdate.this", withRef("default", "previous-value")), "the-ingress-class"),
			resources.MakeDomainClaim(domainMapping("default", "cantupdate.this", withRef("default", "target"))),
		},
		WantErr: true,
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// This is the update which we will cause to fail.
			Object: ingress(domainMapping("default", "cantupdate.this", withRef("default", "cantupdate")), "the-ingress-class"),
		}},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "cantupdate.this",
				withRef("default", "cantupdate"),
				withURL("http", "cantupdate.this"),
				withAddress("http", "cantupdate.this"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withIngressNotConfigured,
				withGeneration(1),
				withObservedGeneration,
			),
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update Ingress: inducing failure for update ingresses"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		r := &Reconciler{
			netclient:     networkingclient.Get(ctx),
			ingressLister: listers.GetIngressLister(),
			resolver:      resolver.NewURIResolver(ctx, func(types.NamespacedName) {}),
		}

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: &config.Config{
					Network: &network.Config{
						DefaultIngressClass: "the-ingress-class",
					},
				},
			}},
		)
	}))
}

type domainMappingOption func(dm *v1alpha1.DomainMapping)

func domainMapping(namespace, name string, opt ...domainMappingOption) *v1alpha1.DomainMapping {
	dm := &v1alpha1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	for _, o := range opt {
		o(dm)
	}
	return dm
}

func withAnnotations(ans map[string]string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Annotations = ans
	}
}

func withUID(uid types.UID) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.UID = uid
	}
}

func withRef(namespace, name string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Spec.Ref.Namespace = namespace
		dm.Spec.Ref.Name = name
		dm.Spec.Ref.APIVersion = "serving.knative.dev/v1"
		dm.Spec.Ref.Kind = "Service"
	}
}

func withURL(scheme, host string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Status.URL = &apis.URL{Scheme: scheme, Host: host}
	}
}

func withAddress(scheme, host string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Status.Address = &duckv1.Addressable{URL: &apis.URL{
			Scheme: scheme,
			Host:   host,
		}}
	}
}

func withIngressNotConfigured(dm *v1alpha1.DomainMapping) {
	dm.Status.MarkIngressNotConfigured()
}

func withPropagatedStatus(status netv1alpha1.IngressStatus) domainMappingOption {
	return func(r *v1alpha1.DomainMapping) {
		r.Status.PropagateIngressStatus(status)
	}
}

func withInitDomainMappingConditions(dm *v1alpha1.DomainMapping) {
	dm.Status.InitializeConditions()
}

func withDomainClaimNotOwned(dm *v1alpha1.DomainMapping) {
	dm.Status.MarkDomainClaimNotOwned()
}

func withDomainClaimed(dm *v1alpha1.DomainMapping) {
	dm.Status.MarkDomainClaimed()
}

func withReferenceResolved(dm *v1alpha1.DomainMapping) {
	dm.Status.MarkReferenceResolved()
}

func withReferenceNotResolved(message string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Status.MarkReferenceNotResolved(message)
	}
}

func withGeneration(generation int64) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Generation = generation
	}
}

func withObservedGeneration(dm *v1alpha1.DomainMapping) {
	dm.Status.ObservedGeneration = dm.Generation
}

func ingress(dm *v1alpha1.DomainMapping, ingressClass string, opt ...IngressOption) *netv1alpha1.Ingress {
	ing := resources.MakeIngress(dm, "the-target-svc.url", ingressClass)
	for _, o := range opt {
		o(ing)
	}

	return ing
}

func withIngressReady(ing *netv1alpha1.Ingress) {
	status := netv1alpha1.IngressStatus{}
	status.InitializeConditions()
	status.MarkNetworkConfigured()
	status.MarkLoadBalancerReady(
		[]netv1alpha1.LoadBalancerIngressStatus{{
			DomainInternal: pkgnetwork.GetServiceHostname("istio-ingressgateway", "istio-system"),
		}},
		[]netv1alpha1.LoadBalancerIngressStatus{{
			DomainInternal: pkgnetwork.GetServiceHostname("private-istio-ingressgateway", "istio-system"),
		}},
	)

	ing.Status = status
}

func ksvc(ns, name, host, path string) *servingv1.Service {
	return &servingv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Status: servingv1.ServiceStatus{
			RouteStatusFields: servingv1.RouteStatusFields{
				Address: &duckv1.Addressable{
					URL: &apis.URL{
						Host: host,
						Path: path,
					},
				},
			},
		},
	}
}

func withIngressNotReady(ing *netv1alpha1.Ingress) {
	ing.Status.MarkIngressNotReady("progressing", "hold your horses")
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgreconciler.ConfigStore = (*testConfigStore)(nil)
