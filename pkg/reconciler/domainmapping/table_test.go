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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgotesting "k8s.io/client-go/testing"

	netapi "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	networkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	pkgnetwork "knative.dev/pkg/network"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"
	"knative.dev/pkg/tracker"

	"knative.dev/serving/pkg/apis/serving"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	domainmappingreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1beta1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/config"
	"knative.dev/serving/pkg/reconciler/domainmapping/resources"
	routenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	servicenames "knative.dev/serving/pkg/reconciler/service/resources/names"
	testingv1 "knative.dev/serving/pkg/testing/v1"

	"knative.dev/pkg/client/injection/ducks/duck/v1/addressable"
	. "knative.dev/pkg/reconciler/testing"

	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing"
)

type key int

const externalSchemeKey key = iota

func TestReconcile(t *testing.T) {
	now := metav1.Now()
	const longDomainMappingName = "this-is-a-very-long-domain-mapping-name.for-a-subdomain.example.com"

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
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
			resources.MakeIngress(domainMapping("default", "first-reconcile.com", withRef("default", "target")), "the-target-svc", "the-target-svc.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "Knative Service uses HTTP paths from its Route Ingress",
		Key:  "default/service.example.com",
		Objects: func() []runtime.Object {
			svc := knativeService("default", "website")
			route := serviceRoute(svc)
			return []runtime.Object{
				svc,
				route,
				routeIngress(route),
				domainMapping("default", "service.example.com", withRef("default", "website", asKnativeServiceRef)),
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "service.example.com",
				withRef("default", "website", asKnativeServiceRef),
				withURL("http", "service.example.com"),
				withAddress("http", "service.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "service.example.com")),
			resources.MakeIngressWithHTTPPaths(
				domainMapping("default", "service.example.com", withRef("default", "website", asKnativeServiceRef)),
				routeIngressHTTPPaths(), "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "service.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "service.example.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "service.example.com"),
		},
		PostConditions: []func(*testing.T, *TableRow){
			AssertTrackingObject(servingv1.SchemeGroupVersion.WithKind("Route"), "default", "website"),
			AssertTrackingObject(netv1alpha1.SchemeGroupVersion.WithKind("Ingress"), "default", "website"),
		},
	}, {
		Name: "Knative Route uses HTTP paths from its Ingress",
		Key:  "default/route.example.com",
		Objects: func() []runtime.Object {
			route := standaloneRoute("default", "website")
			return []runtime.Object{
				route,
				routeIngress(route),
				domainMapping("default", "route.example.com", withRef("default", "website", asKnativeRouteRef)),
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "route.example.com",
				withRef("default", "website", asKnativeRouteRef),
				withURL("http", "route.example.com"),
				withAddress("http", "route.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "route.example.com")),
			resources.MakeIngressWithHTTPPaths(
				domainMapping("default", "route.example.com", withRef("default", "website", asKnativeRouteRef)),
				routeIngressHTTPPaths(), "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "route.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "route.example.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "route.example.com"),
		},
	}, {
		Name: "Knative Service waits for Route Ingress",
		Key:  "default/waiting.example.com",
		Objects: func() []runtime.Object {
			svc := knativeService("default", "website")
			dm := domainMapping("default", "waiting.example.com", withRef("default", "website", asKnativeServiceRef))
			return []runtime.Object{
				svc,
				serviceRoute(svc),
				resources.MakeIngress(dm, "old-revision", "website.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
				dm,
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "waiting.example.com",
				withRef("default", "website", asKnativeServiceRef),
				withURL("http", "waiting.example.com"),
				withAddress("http", "waiting.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withTargetIngressNotConfigured("Waiting for target Route default/website to configure Ingress default/website."),
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "waiting.example.com")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "waiting.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "waiting.example.com"),
		},
		PostConditions: []func(*testing.T, *TableRow){
			AssertTrackingObject(netv1alpha1.SchemeGroupVersion.WithKind("Ingress"), "default", "website"),
		},
	}, {
		Name: "Knative Route rejects an Ingress owned by another Route",
		Key:  "default/not-routable.example.com",
		Objects: func() []runtime.Object {
			route := standaloneRoute("default", "website")
			targetIngress := routeIngress(route)
			targetIngress.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(standaloneRoute("default", "another-route"))}
			dm := domainMapping("default", "not-routable.example.com", withRef("default", "website", asKnativeRouteRef))
			return []runtime.Object{
				route,
				targetIngress,
				resources.MakeIngress(dm, "old-revision", "website.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
				dm,
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "not-routable.example.com",
				withRef("default", "website", asKnativeRouteRef),
				withURL("http", "not-routable.example.com"),
				withAddress("http", "not-routable.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withTargetNotOwned("Route default/website does not own Ingress default/website."),
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "not-routable.example.com")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "not-routable.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "not-routable.example.com"),
		},
	}, {
		Name:    "Knative Service resolution failure preserves an existing Ingress with a long mapping name",
		Key:     "default/" + longDomainMappingName,
		WantErr: true,
		Objects: func() []runtime.Object {
			dm := domainMapping("default", longDomainMappingName, withRef("default", "missing", asKnativeServiceRef))
			return []runtime.Object{
				resources.MakeIngress(dm, "old-revision", "missing.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
				dm,
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", longDomainMappingName,
				withRef("default", "missing", asKnativeServiceRef),
				withURL("http", longDomainMappingName),
				withAddress("http", longDomainMappingName),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceNotResolved(`failed to get object default/missing: services.serving.knative.dev "missing" not found`),
				withIngressNotConfigured,
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", longDomainMappingName)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", longDomainMappingName),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", longDomainMappingName),
			Eventf(corev1.EventTypeWarning, "InternalError", `resolving reference: failed to get object default/missing: services.serving.knative.dev "missing" not found`),
		},
	}, {
		Name: "finalize cleans up claim",
		Key:  "default/cleanup.on.aisle-three",
		Objects: []runtime.Object{
			domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"), withFinalizer, withDeletionTimestamp(&now)),
			resources.MakeDomainClaim(domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"))),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Verb:     "delete",
				Resource: v1beta1.SchemeGroupVersion.WithResource("clusterdomainclaims"),
			},
			Name: "cleanup.on.aisle-three",
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveFinalizerAction("default", "cleanup.on.aisle-three"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cleanup.on.aisle-three"),
		},
	}, {
		Name: "finalize does not clean up unowned claim",
		Key:  "default/cleanup.on.aisle-three",
		Objects: []runtime.Object{
			domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"), withFinalizer, withDeletionTimestamp(&now)),
			resources.MakeDomainClaim(domainMapping("another-namespace", "cleanup.on.aisle-three", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveFinalizerAction("default", "cleanup.on.aisle-three"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cleanup.on.aisle-three"),
		},
	}, {
		Name: "finalize claim already gone or never claimed",
		Key:  "default/cleanup.on.aisle-three",
		Objects: []runtime.Object{
			domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"), withFinalizer, withDeletionTimestamp(&now)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveFinalizerAction("default", "cleanup.on.aisle-three"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cleanup.on.aisle-three"),
		},
	}, {
		Name: "finalize claim deletion fails",
		Key:  "default/cleanup.on.aisle-three",
		Objects: []runtime.Object{
			domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"), withFinalizer, withDeletionTimestamp(&now)),
			resources.MakeDomainClaim(domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"))),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("delete", "clusterdomainclaims"),
		},
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			// This is the delete we induce failure on.
			ActionImpl: clientgotesting.ActionImpl{
				Verb:     "delete",
				Resource: v1alpha1.SchemeGroupVersion.WithResource("clusterdomainclaims"),
			},
			Name: "cleanup.on.aisle-three",
		}},
		WantErr: true,
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for delete clusterdomainclaims"),
		},
	}, {
		Name: "first reconcile, corev1 service",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			service("default", "target"),
			domainMapping("default", "first-reconcile.com", withRef("default", "target", withAPIVersionKind("v1", "Service"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target", withAPIVersionKind("v1", "Service")),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target", withAPIVersionKind("v1", "Service")))),
			resources.MakeIngress(
				domainMapping("default", "first-reconcile.com", withRef("default", "target", withAPIVersionKind("v1", "Service"))),
				"target", "target.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
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
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceNotResolved(`failed to get object default/target: domainmappings.serving.knative.dev "target" not found`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `resolving reference: failed to get object default/target: domainmappings.serving.knative.dev "target" not found`),
		},
	}, {
		Name: "first reconcile, ref has a path",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.svc.cluster.local", "path"),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceNotResolved(`resolved URI "http://the-target-svc.svc.cluster.local/path" contains a path`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `resolved URI "http://the-target-svc.svc.cluster.local/path" contains a path`),
		},
	}, {
		Name: "first reconcile, ref doesn't end in cluster suffix",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "notasvc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceNotResolved(`resolved URI "http://notasvc.cluster.local" must be of the form {name}.{namespace}.svc.cluster.local`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `resolved URI "http://notasvc.cluster.local" must be of the form {name}.{namespace}.svc.cluster.local`),
		},
	}, {
		Name: "first reconcile, resolved URL in wrong namespace",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "name.anothernamespace.svc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantErr: true,
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceNotResolved(`resolved URI "http://name.anothernamespace.svc.cluster.local" must be in same namespace as DomainMapping`),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `resolved URI "http://name.anothernamespace.svc.cluster.local" must be in same namespace as DomainMapping`),
		},
	}, {
		Name: "first reconcile, pre-owned domain claim",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withIngressNotConfigured,
				withDomainClaimed,
				withReferenceResolved,
			),
		}},
		WantCreates: []runtime.Object{
			resources.MakeIngress(domainMapping("default", "first-reconcile.com", withRef("default", "target")), "the-target-svc", "the-target-svc.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "first reconcile, can't claim domain",
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
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to create ClusterDomainClaim: inducing failure for create clusterdomainclaims`),
		},
	}, {
		Name: "first reconcile, non-owned domainclaim",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(
				domainMapping("wrong-namespace", "first-reconcile.com", withRef("default", "target"),
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
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `namespace "default" does not own ClusterDomainClaim for "first-reconcile.com"`),
		},
	}, {
		Name: "reconcile with ingressClass annotation",
		Key:  "default/ingressclass.first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"),
				withAnnotations(map[string]string{
					netapi.IngressClassAnnotationKey: "overridden-ingress-class",
				}),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingressclass.first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "ingressclass.first-reconcile.com"),
				withAddress("http", "ingressclass.first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
				withAnnotations(map[string]string{
					netapi.IngressClassAnnotationKey: "overridden-ingress-class",
				}),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"))),
			resources.MakeIngress(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target")),
				"the-target-svc", "the-target-svc.default.svc.cluster.local", "overridden-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingressclass.first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingressclass.first-reconcile.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "ingressclass.first-reconcile.com"),
		},
	}, {
		Name: "reconcile with new label",
		Key:  "default/ingressclass.first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"),
				withLabels(map[string]string{
					netapi.IngressLabelKey: "new-label",
				}),
			),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingressclass.first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "ingressclass.first-reconcile.com"),
				withAddress("http", "ingressclass.first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
				withLabels(map[string]string{
					netapi.IngressLabelKey: "new-label",
				}),
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"))),
			resources.MakeIngress(domainMapping("default", "ingressclass.first-reconcile.com", withRef("default", "target"), withLabels(map[string]string{netapi.IngressLabelKey: "new-label"})),
				"the-target-svc", "the-target-svc.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingressclass.first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingressclass.first-reconcile.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "ingressclass.first-reconcile.com"),
		},
	}, {
		Name: "reconcile changed ref",
		Key:  "default/ingress-exists.org",
		Objects: []runtime.Object{
			addressableMapping("default", "changed", "changed.default.svc.cluster.local", ""),
			domainMapping("default", "ingress-exists.org", withRef("default", "changed")),
			resources.MakeIngress(domainMapping("default", "ingress-exists.org", withRef("default", "changed")), "previous", "previous.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
			resources.MakeDomainClaim(domainMapping("default", "ingress-exists.org", withRef("default", "changed"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-exists.org",
				withRef("default", "changed"),
				withURL("http", "ingress-exists.org"),
				withAddress("http", "ingress-exists.org"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingress(domainMapping("default", "ingress-exists.org", withRef("default", "changed")), "the-ingress-class"),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingress-exists.org"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingress-exists.org"),
		},
	}, {
		Name: "reconcile failed ingress",
		Key:  "default/ingress-failed.me",
		Objects: []runtime.Object{
			addressableMapping("default", "failed", "failed.default.svc.cluster.local", ""),
			domainMapping("default", "ingress-failed.me",
				withRef("default", "failed"),
				withURL("http", "ingress-failed.me"),
				withAddress("http", "ingress-failed.me"),
			),
			ingress(domainMapping("default", "ingress-failed.me", withRef("default", "failed")), "the-ingress-class",
				WithLoadbalancerFailed("fell over", "hurt myself"),
			),
			resources.MakeDomainClaim(domainMapping("default", "ingress-failed.me", withRef("default", "failed"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "ingress-failed.me",
				withRef("default", "failed"),
				withURL("http", "ingress-failed.me"),
				withAddress("http", "ingress-failed.me"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "failed.default.svc.cluster.local"), "", WithLoadbalancerFailed("fell over", "hurt myself")).Status),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingress-failed.me"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingress-failed.me"),
		},
	}, {
		Name: "reconcile unknown ingress",
		Key:  "default/ingress-unknown.me",
		Objects: []runtime.Object{
			addressableMapping("default", "unknown", "unknown.default.svc.cluster.local", ""),
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
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-unknown.me"), "", withIngressNotReady).Status),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingress-unknown.me"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingress-unknown.me"),
		},
	}, {
		Name: "reconcile ready ingress",
		Key:  "default/ingress-ready.me",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
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
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-ready.me"), "", withIngressReady).Status),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingress-ready.me"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingress-ready.me"),
		},
	}, {
		Name: "fail ingress creation",
		Key:  "default/cantcreate.this",
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "ingresses"),
		},
		Objects: []runtime.Object{
			addressableMapping("default", "cantcreate", "cantcreate.default.svc.cluster.local", ""),
			domainMapping("default", "cantcreate.this",
				withRef("default", "cantcreate"),
				withURL("http", "cantcreate.this"),
				withAddress("http", "cantcreate.this"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
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
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withIngressNotConfigured,
				withGeneration(1),
				withObservedGeneration,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "cantcreate.this"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cantcreate.this"),
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
			addressableMapping("default", "cantupdate", "cantupdate.default.svc.cluster.local", ""),
			domainMapping("default", "cantupdate.this",
				withRef("default", "cantupdate"),
				withURL("http", "cantupdate.this"),
				withAddress("http", "cantupdate.this"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
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
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withIngressNotConfigured,
				withGeneration(1),
				withObservedGeneration,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "cantupdate.this"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cantupdate.this"),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to update Ingress: inducing failure for update ingresses"),
		},
	}, {
		Name: "overridden external scheme",
		Key:  "default/ingress-ready.me",
		Ctx:  context.WithValue(context.Background(), externalSchemeKey, "ws"),
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "ingress-ready.me",
				withRef("default", "ready"),
				withURL("ws", "ingress-ready.me"),
				withAddress("ws", "ingress-ready.me"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "ingress-ready.me"), "", withIngressReady).Status),
			),
			ingress(domainMapping("default", "ingress-ready.me", withRef("default", "ready")), "the-ingress-class",
				withIngressReady,
			),
			resources.MakeDomainClaim(domainMapping("default", "ingress-ready.me", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "ingress-ready.me"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "ingress-ready.me"),
		},
	}}

	table = append(table, routeIngressEdgeCaseRows()...)

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		r := makeTestReconciler(ctx, listers)

		cfg := &config.Config{
			Network: &netcfg.Config{
				DefaultIngressClass:           "the-ingress-class",
				AutocreateClusterDomainClaims: true,
				HTTPProtocol:                  netcfg.HTTPEnabled,
				DefaultExternalScheme:         "http",
			},
		}
		if v := ctx.Value(externalSchemeKey); v != nil {
			cfg.Network.DefaultExternalScheme = v.(string)
		}

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: cfg,
			}},
		)
	}))
}

func TestReconcileAutocreateClaimsDisabled(t *testing.T) {
	now := metav1.Now()

	table := TableTest{{
		Name: "first reconcile, no existing claim",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withDomainClaimNotOwned,
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantErr:                 true,
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `no ClusterDomainClaim found for domain "first-reconcile.com" (and autocreate-cluster-domain-claims property is not true)`),
		},
	}, {
		Name: "first reconcile, claim exists and is owned",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			addressableMapping("default", "target", "the-target-svc.default.svc.cluster.local", ""),
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(domainMapping("default", "first-reconcile.com", withRef("default", "target"))),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true, // allow creation of ClusterDomainClaim.
		WantCreates: []runtime.Object{
			resources.MakeIngress(domainMapping("default", "first-reconcile.com", withRef("default", "target")), "the-target-svc", "the-target-svc.default.svc.cluster.local", "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil /* tls */),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "first reconcile, claim exists but isn't owned",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			domainMapping("default", "first-reconcile.com", withRef("default", "target")),
			resources.MakeDomainClaim(
				domainMapping("wrong-namespace", "first-reconcile.com", withRef("default", "target"),
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
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", `namespace "default" does not own ClusterDomainClaim for "first-reconcile.com"`),
		},
	}, {
		Name: "finalize does not clean up claim, even if owned by namespace",
		Key:  "default/cleanup.on.aisle-three",
		Objects: []runtime.Object{
			domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"), withFinalizer, withDeletionTimestamp(&now)),
			resources.MakeDomainClaim(domainMapping("default", "cleanup.on.aisle-three", withRef("default", "target"))),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveFinalizerAction("default", "cleanup.on.aisle-three"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cleanup.on.aisle-three"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		r := makeTestReconciler(ctx, listers)

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: &config.Config{
					Network: &netcfg.Config{
						DefaultIngressClass:           "the-ingress-class",
						AutocreateClusterDomainClaims: false,
						HTTPProtocol:                  netcfg.HTTPEnabled,
						DefaultExternalScheme:         "http",
					},
				},
			}},
		)
	}))
}

func TestReconcileTLSEnabled(t *testing.T) {
	table := TableTest{{
		Name: "first reconcile",
		Key:  "default/first.reconcile.io",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "first.reconcile.io",
				withRef("default", "ready"),
				withURL("http", "first.reconcile.io"),
				withAddress("http", "first.reconcile.io"),
			),
			resources.MakeDomainClaim(domainMapping("default", "first.reconcile.io", withRef("default", "ready"))),
		},
		WantCreates: []runtime.Object{
			resources.MakeCertificate(domainMapping("default", "first.reconcile.io",
				withRef("default", "ready"),
				withURL("http", "first.reconcile.io"),
				withAddress("http", "first.reconcile.io"),
			), "the-cert-class"),
			ingress(domainMapping("default", "first.reconcile.io", withRef("default", "ready")), "the-ingress-class", withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "first.reconcile.io",
				withRef("default", "ready"),
				withURL("https", "first.reconcile.io"),
				withAddress("https", "first.reconcile.io"),
				withCertificateNotReady,
				withInitDomainMappingConditions,
				withIngressNotConfigured,
				withDomainClaimed,
				withReferenceResolved,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first.reconcile.io"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first.reconcile.io"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Certificate %s/%s", "default", "first.reconcile.io"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first.reconcile.io"),
		},
	}, {
		Name: "becomes ready",
		Key:  "default/becomes.ready.run",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "becomes.ready.run",
				withRef("default", "ready"),
				withURL("http", "becomes.ready.run"),
				withAddress("http", "becomes.ready.run"),
			),
			resources.MakeDomainClaim(domainMapping("default", "becomes.ready.run", withRef("default", "ready"))),
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "becomes.ready.run",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						domainMapping("default", "becomes.ready.run",
							withRef("default", "ready"),
							withURL("http", "becomes.ready.run"),
							withAddress("http", "becomes.ready.run")))},
					Annotations: map[string]string{
						netapi.CertificateClassAnnotationKey: "the-cert-class",
					},
					Labels: map[string]string{
						serving.DomainMappingUIDLabelKey: "becomes.ready.run",
						netapi.CertificateTypeLabelKey:   string(netcfg.CertificateExternalDomain),
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					Domain:     "becomes.ready.run",
					DNSNames:   []string{"becomes.ready.run"},
					SecretName: "becomes.ready.run",
				},
				Status: readyCertStatus(),
			},
			ingress(domainMapping("default", "becomes.ready.run", withRef("default", "ready")), "the-ingress-class", withIngressReady, withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected)),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "becomes.ready.run",
				withRef("default", "ready"),
				withURL("https", "becomes.ready.run"),
				withAddress("https", "becomes.ready.run"),
				withCertificateReady,
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "becomes.ready.run"), "", withIngressReady).Status),
			),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: ingress(domainMapping("default", "becomes.ready.run", withRef("default", "ready")), "the-ingress-class", withIngressReady,
				withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected),
				withIngressTLS(netv1alpha1.IngressTLS{
					Hosts:           []string{"becomes.ready.run"},
					SecretName:      "becomes.ready.run",
					SecretNamespace: "default",
				})),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "becomes.ready.run"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "becomes.ready.run"),
		},
	}, {
		Name:    "cert not owned",
		WantErr: true,
		Key:     "default/cert.not.owned.ru",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "cert.not.owned.ru",
				withRef("default", "ready"),
				withURL("http", "cert.not.owned.ru"),
				withAddress("http", "cert.not.owned.ru"),
			),
			resources.MakeDomainClaim(domainMapping("default", "cert.not.owned.ru", withRef("default", "ready"))),
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert.not.owned.ru",
					Namespace: "default",
					Annotations: map[string]string{
						netapi.CertificateClassAnnotationKey: "the-cert-class",
					},
				},
			},
			ingress(domainMapping("default", "cert.not.owned.ru", withRef("default", "ready")), "the-ingress-class", withIngressReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "cert.not.owned.ru",
				withRef("default", "ready"),
				withURL("http", "cert.not.owned.ru"),
				withAddress("http", "cert.not.owned.ru"),
				withDomainClaimed,
				withCertificateNotOwned,
				withInitDomainMappingConditions,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "cert.not.owned.ru"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cert.not.owned.ru"),
			Eventf(corev1.EventTypeWarning, "InternalError", `notowned: owner: cert.not.owned.ru with Type *v1beta1.DomainMapping does not own Certificate: "cert.not.owned.ru"`),
		},
	}, {
		Name:    "cert creation failed",
		WantErr: true,
		Key:     "default/cert.creation.failed.ly",
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "certificates"),
		},
		WantCreates: []runtime.Object{
			resources.MakeCertificate(domainMapping("default", "cert.creation.failed.ly",
				withRef("default", "ready"),
				withURL("http", "cert.creation.failed.ly"),
				withAddress("http", "cert.creation.failed.ly"),
			), "the-cert-class"),
		},
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "cert.creation.failed.ly",
				withRef("default", "ready"),
				withURL("http", "cert.creation.failed.ly"),
				withAddress("http", "cert.creation.failed.ly"),
			),
			resources.MakeDomainClaim(domainMapping("default", "cert.creation.failed.ly", withRef("default", "ready"))),
			ingress(domainMapping("default", "cert.creation.failed.ly", withRef("default", "ready")), "the-ingress-class", withIngressReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "cert.creation.failed.ly",
				withRef("default", "ready"),
				withURL("http", "cert.creation.failed.ly"),
				withAddress("http", "cert.creation.failed.ly"),
				withDomainClaimed,
				withCertificateFail,
				withInitDomainMappingConditions,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "cert.creation.failed.ly"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "cert.creation.failed.ly"),
			Eventf(corev1.EventTypeWarning, "CreationFailed", `Failed to create Certificate default/cert.creation.failed.ly: inducing failure for create certificates`),
			Eventf(corev1.EventTypeWarning, "InternalError", `failed to create Certificate: inducing failure for create certificates`),
		},
	}, {
		Name: "with challenges",
		Key:  "default/challenged.com",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "challenged.com",
				withRef("default", "ready"),
				withURL("http", "challenged.com"),
				withAddress("http", "challenged.com"),
			),
			resources.MakeDomainClaim(domainMapping("default", "challenged.com", withRef("default", "ready"))),
			ingress(domainMapping("default", "challenged.com", withRef("default", "ready")), "the-ingress-class", withIngressReady, withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected)),
			&netv1alpha1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "challenged.com",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(
						domainMapping("default", "challenged.com",
							withRef("default", "ready"),
							withURL("http", "challenged.com"),
							withAddress("http", "challenged.com"),
						))},
					Annotations: map[string]string{
						netapi.CertificateClassAnnotationKey: "the-cert-class",
					},
					Labels: map[string]string{
						serving.DomainMappingUIDLabelKey: "challenged.com",
						netapi.CertificateTypeLabelKey:   string(netcfg.CertificateExternalDomain),
					},
				},
				Spec: netv1alpha1.CertificateSpec{
					Domain:     "challenged.com",
					DNSNames:   []string{"challenged.com"},
					SecretName: "challenged.com",
				},
				Status: netv1alpha1.CertificateStatus{
					HTTP01Challenges: []netv1alpha1.HTTP01Challenge{{
						URL: &apis.URL{
							Scheme: "http",
							Host:   "challenged.com",
							Path:   "/.well-known/acme-challenge/challengeToken",
						},
						ServiceName:      "cm-solver",
						ServicePort:      intstr.FromInt(8090),
						ServiceNamespace: "default",
					}, {
						URL: &apis.URL{
							Scheme: "http",
							Host:   "challenged.com",
							Path:   "/.well-known/acme-challenge/challengeToken-two",
						},
						ServiceName:      "cm-solver",
						ServicePort:      intstr.FromInt(8090),
						ServiceNamespace: "default",
					}},
				},
			},
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "challenged.com",
				withRef("default", "ready"),
				withURL("https", "challenged.com"),
				withAddress("https", "challenged.com"),
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withCertificateNotReady,
				withPropagatedStatus(ingress(domainMapping("default", "challenged.com"), "", withIngressReady).Status),
			),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			// Ingress should be updated with correct challenges
			Object: ingressWithChallenges(domainMapping("default", "challenged.com",
				withRef("default", "ready"),
				withURL("https", "challenged.com"),
				withAddress("https", "challenged.com")),
				"the-ingress-class",
				[]netv1alpha1.HTTP01Challenge{{
					URL: &apis.URL{
						Scheme: "http",
						Host:   "challenged.com",
						Path:   "/.well-known/acme-challenge/challengeToken",
					},
					ServiceName:      "cm-solver",
					ServicePort:      intstr.FromInt(8090),
					ServiceNamespace: "default",
				}, {
					URL: &apis.URL{
						Scheme: "http",
						Host:   "challenged.com",
						Path:   "/.well-known/acme-challenge/challengeToken-two",
					},
					ServiceName:      "cm-solver",
					ServicePort:      intstr.FromInt(8090),
					ServiceNamespace: "default",
				}}, withIngressReady, withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected)),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "challenged.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "challenged.com"),
		},
	}, {
		Name: "TLS secret provided",
		Key:  "default/certificateless.com",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "certificateless.com",
				withTLSSecret("tls-secret"),
				withRef("default", "ready"),
				withURL("https", "certificateless.com"),
				withAddress("https", "certificateless.com"),
			),
			resources.MakeDomainClaim(domainMapping("default", "certificateless.com", withRef("default", "ready"))),
		},
		WantCreates: []runtime.Object{
			ingress(domainMapping("default", "certificateless.com", withRef("default", "ready")), "the-ingress-class",
				withIngressHTTPOption(netv1alpha1.HTTPOptionRedirected),
				withIngressTLS(netv1alpha1.IngressTLS{
					Hosts:           []string{"certificateless.com"},
					SecretName:      "tls-secret",
					SecretNamespace: "default",
				})),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "certificateless.com",
				withRef("default", "ready"),
				withURL("https", "certificateless.com"),
				withAddress("https", "certificateless.com"),
				withTLSSecret("tls-secret"),
				withInitDomainMappingConditions,
				withCertificateReady,
				withDomainClaimed,
				withReferenceResolved,
				withCertificateNotRequired,
				withIngressNotConfigured,
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "certificateless.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "certificateless.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "certificateless.com"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		r := makeTestReconciler(ctx, listers)

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: &config.Config{
					Network: &netcfg.Config{
						DefaultIngressClass:     "the-ingress-class",
						DefaultCertificateClass: "the-cert-class",
						ExternalDomainTLS:       true,
						HTTPProtocol:            netcfg.HTTPRedirected,
						DefaultExternalScheme:   "http",
					},
				},
			}},
		)
	}))
}

func TestReconcileTLSEnabledButDowngraded(t *testing.T) {
	table := TableTest{{
		Name: "ingress ready, cert not ready, downgraded to HTTP",
		Key:  "default/http.downgraded.com",
		Objects: []runtime.Object{
			addressableMapping("default", "ready", "ready.default.svc.cluster.local", ""),
			domainMapping("default", "http.downgraded.com",
				withRef("default", "ready"),
				withURL("http", "http.downgraded.com"),
				withAddress("http", "http.downgraded.com"),
			),
			resources.MakeDomainClaim(domainMapping("default", "http.downgraded.com", withRef("default", "ready"))),
			resources.MakeCertificate(domainMapping("default", "http.downgraded.com",
				withRef("default", "ready"),
				withURL("http", "http.downgraded.com"),
				withAddress("http", "http.downgraded.com"),
			), "the-cert-class"),
			ingress(domainMapping("default", "http.downgraded.com", withRef("default", "ready")), "the-ingress-class",
				withIngressReady),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "http.downgraded.com",
				withRef("default", "ready"),
				withURL("http", "http.downgraded.com"),
				withAddress("http", "http.downgraded.com"),
				withHTTPDowngraded,
				withInitDomainMappingConditions,
				withDomainClaimed,
				withReferenceResolved,
				withPropagatedStatus(ingress(domainMapping("default", "http.downgraded.com"), "", withIngressReady).Status),
			),
		}},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "http.downgraded.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "http.downgraded.com"),
		},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = addressable.WithDuck(ctx)
		r := makeTestReconciler(ctx, listers)

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx),
			servingclient.Get(ctx), listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: &config.Config{
					Network: &netcfg.Config{
						DefaultIngressClass:     "the-ingress-class",
						DefaultCertificateClass: "the-cert-class",
						ExternalDomainTLS:       true,
						HTTPProtocol:            netcfg.HTTPEnabled,
						DefaultExternalScheme:   "http",
					},
				},
			}},
		)
	}))
}

type domainMappingOption func(dm *v1beta1.DomainMapping)

func domainMapping(namespace, name string, opt ...domainMappingOption) *v1beta1.DomainMapping {
	dm := &v1beta1.DomainMapping{
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
	return func(dm *v1beta1.DomainMapping) {
		dm.Annotations = ans
	}
}

func withLabels(label map[string]string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Labels = label
	}
}

func withUID(uid types.UID) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.UID = uid
	}
}

type refOption func(*duckv1.KReference)

func withRef(namespace, name string, opt ...refOption) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Spec.Ref.Namespace = namespace
		dm.Spec.Ref.Name = name
		dm.Spec.Ref.APIVersion = v1beta1.SchemeGroupVersion.String()
		dm.Spec.Ref.Kind = "DomainMapping"

		for _, o := range opt {
			o(&dm.Spec.Ref)
		}
	}
}

func withAPIVersionKind(apiVersion, kind string) refOption {
	return func(ref *duckv1.KReference) {
		ref.APIVersion = apiVersion
		ref.Kind = kind
	}
}

var (
	asKnativeServiceRef = withAPIVersionKind(servingv1.SchemeGroupVersion.String(), "Service")
	asKnativeRouteRef   = withAPIVersionKind(servingv1.SchemeGroupVersion.String(), "Route")
)

func withURL(scheme, host string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Status.URL = &apis.URL{Scheme: scheme, Host: host}
	}
}

func withAddress(scheme, host string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Status.Address = &duckv1.Addressable{URL: &apis.URL{
			Scheme: scheme,
			Host:   host,
		}}
	}
}

func withIngressNotConfigured(dm *v1beta1.DomainMapping) {
	dm.Status.MarkIngressNotConfigured()
}

func withTargetIngressNotConfigured(message string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Status.MarkTargetIngressNotConfigured(message)
	}
}

func withTargetNotOwned(message string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Status.MarkTargetNotOwned(message)
	}
}

func withPropagatedStatus(status netv1alpha1.IngressStatus) domainMappingOption {
	return func(r *v1beta1.DomainMapping) {
		r.Status.PropagateIngressStatus(status)
	}
}

func withTLSSecret(secretName string) domainMappingOption {
	return func(r *v1beta1.DomainMapping) {
		r.Spec.TLS = &v1beta1.SecretTLS{
			SecretName: secretName,
		}
	}
}

func withCertificateNotRequired(dm *v1beta1.DomainMapping) {
	dm.Status.MarkCertificateNotRequired(v1beta1.TLSCertificateProvidedExternally)
}

func withInitDomainMappingConditions(dm *v1beta1.DomainMapping) {
	dm.Status.InitializeConditions()
}

func withTLSNotEnabled(dm *v1beta1.DomainMapping) {
	dm.Status.MarkTLSNotEnabled(servingv1.ExternalDomainTLSNotEnabledMessage)
}

func withCertificateNotReady(dm *v1beta1.DomainMapping) {
	dm.Status.MarkCertificateNotReady(dm.Name)
}

func withHTTPDowngraded(dm *v1beta1.DomainMapping) {
	dm.Status.MarkHTTPDowngrade(dm.Name)
}

func withCertificateReady(dm *v1beta1.DomainMapping) {
	dm.Status.MarkCertificateReady(dm.Name)
}

func withCertificateFail(dm *v1beta1.DomainMapping) {
	dm.Status.MarkCertificateProvisionFailed(dm.Name)
}

func withCertificateNotOwned(dm *v1beta1.DomainMapping) {
	dm.Status.MarkCertificateNotOwned(dm.Name)
}

func withDomainClaimNotOwned(dm *v1beta1.DomainMapping) {
	dm.Status.MarkDomainClaimNotOwned()
}

func withDomainClaimed(dm *v1beta1.DomainMapping) {
	dm.Status.MarkDomainClaimed()
}

func withReferenceResolved(dm *v1beta1.DomainMapping) {
	dm.Status.MarkReferenceResolved()
}

func withReferenceNotResolved(message string) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Status.MarkReferenceNotResolved(message)
	}
}

func withGeneration(generation int64) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.Generation = generation
	}
}

func withObservedGeneration(dm *v1beta1.DomainMapping) {
	dm.Status.ObservedGeneration = dm.Generation
}

func withFinalizer(dm *v1beta1.DomainMapping) {
	dm.ObjectMeta.Finalizers = append(dm.ObjectMeta.Finalizers, "domainmappings.serving.knative.dev")
}

func withDeletionTimestamp(t *metav1.Time) domainMappingOption {
	return func(dm *v1beta1.DomainMapping) {
		dm.ObjectMeta.DeletionTimestamp = t
	}
}

func ingress(dm *v1beta1.DomainMapping, ingressClass string, opt ...IngressOption) *netv1alpha1.Ingress {
	return ingressWithChallenges(dm, ingressClass, nil /* challenges */, opt...)
}

func ingressWithChallenges(dm *v1beta1.DomainMapping, ingressClass string, challenges []netv1alpha1.HTTP01Challenge, opt ...IngressOption) *netv1alpha1.Ingress {
	ing := resources.MakeIngress(dm, dm.Spec.Ref.Name, dm.Spec.Ref.Name+"."+dm.Spec.Ref.Namespace+".svc.cluster.local", ingressClass, netv1alpha1.HTTPOptionEnabled, nil /* tls */, challenges...)
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

func withIngressHTTPOption(httpOpt netv1alpha1.HTTPOption) IngressOption {
	return func(ing *netv1alpha1.Ingress) {
		ing.Spec.HTTPOption = httpOpt
	}
}

func addressableMapping(ns, name, host, path string) *v1beta1.DomainMapping {
	return &v1beta1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Status: v1beta1.DomainMappingStatus{
			URL:     &apis.URL{Scheme: "http", Host: host, Path: path},
			Address: &duckv1.Addressable{URL: &apis.URL{Scheme: "http", Host: host, Path: path}},
		},
	}
}

func knativeService(ns, name string) *servingv1.Service {
	return testingv1.Service(name, ns, testingv1.WithSvcStatusAddress)
}

func serviceRoute(service *servingv1.Service) *servingv1.Route {
	route := standaloneRoute(service.Namespace, servicenames.Route(service))
	route.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(service)}
	return route
}

func standaloneRoute(ns, name string) *servingv1.Route {
	return testingv1.Route(ns, name, testingv1.WithRouteUID(types.UID(name+"-route-uid")), testingv1.WithAddress)
}

func routeIngress(route *servingv1.Route) *netv1alpha1.Ingress {
	return &netv1alpha1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            routenames.Ingress(route),
			Namespace:       route.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
		},
		Spec: netv1alpha1.IngressSpec{
			Rules: []netv1alpha1.IngressRule{{
				Hosts:      []string{routenames.K8sServiceFullname(route)},
				Visibility: netv1alpha1.IngressVisibilityClusterLocal,
				HTTP:       &netv1alpha1.HTTPIngressRuleValue{Paths: routeIngressHTTPPaths()},
			}},
		},
	}
}

func routeIngressHTTPPaths() []netv1alpha1.HTTPIngressPath {
	return []netv1alpha1.HTTPIngressPath{{
		RewriteHost: "source-host.example",
		AppendHeaders: map[string]string{
			"Knative-Serving-Default-Route": "true",
		},
		Splits: []netv1alpha1.IngressBackendSplit{{
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: "default",
				ServiceName:      "website-00001",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 80,
			AppendHeaders: map[string]string{
				"Knative-Serving-Namespace": "default",
				"Knative-Serving-Revision":  "website-00001",
			},
		}, {
			IngressBackend: netv1alpha1.IngressBackend{
				ServiceNamespace: "default",
				ServiceName:      "website-00002",
				ServicePort:      intstr.FromInt(80),
			},
			Percent: 20,
			AppendHeaders: map[string]string{
				"Knative-Serving-Namespace": "default",
				"Knative-Serving-Revision":  "website-00002",
			},
		}},
	}}
}

func routeIngressEdgeCaseRows() TableTest {
	const targetHost = "website.default.svc.cluster.local"

	service := knativeService("default", "website")
	service.UID = types.UID("website-service-uid")
	route := serviceRoute(service)
	anotherService := knativeService("default", "another-service")
	anotherService.UID = types.UID("another-service-uid")
	route.OwnerReferences = []metav1.OwnerReference{*kmeta.NewControllerRef(anotherService)}
	dm := domainMapping("default", "route-not-owned.example.com", withRef("default", "website", asKnativeServiceRef))
	rows := TableTest{{
		Name: "Knative Service rejects a Route owned by another Service",
		Key:  "default/route-not-owned.example.com",
		Objects: []runtime.Object{
			service,
			route,
			resources.MakeIngress(dm, "old-revision", targetHost, "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
			dm,
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "route-not-owned.example.com",
				withRef("default", "website", asKnativeServiceRef),
				withURL("http", "route-not-owned.example.com"),
				withAddress("http", "route-not-owned.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withTargetNotOwned("Service default/website does not own Route default/website."),
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "route-not-owned.example.com")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "route-not-owned.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "route-not-owned.example.com"),
		},
	}, {
		Name: "Knative Route uses the first matching cluster-local rule",
		Key:  "default/multiple-matching-rules.example.com",
		Objects: func() []runtime.Object {
			route := standaloneRoute("default", "website")
			targetIngress := routeIngress(route)
			second := targetIngress.Spec.Rules[0].DeepCopy()
			second.HTTP.Paths[0].Splits[0].ServiceName = "unexpected-revision"
			targetIngress.Spec.Rules = append(targetIngress.Spec.Rules, *second)
			return []runtime.Object{
				route,
				targetIngress,
				domainMapping("default", "multiple-matching-rules.example.com", withRef("default", "website", asKnativeRouteRef)),
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "multiple-matching-rules.example.com",
				withRef("default", "website", asKnativeRouteRef),
				withURL("http", "multiple-matching-rules.example.com"),
				withAddress("http", "multiple-matching-rules.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withIngressNotConfigured,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "multiple-matching-rules.example.com")),
			resources.MakeIngressWithHTTPPaths(
				domainMapping("default", "multiple-matching-rules.example.com", withRef("default", "website", asKnativeRouteRef)),
				routeIngressHTTPPaths(), "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "multiple-matching-rules.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "multiple-matching-rules.example.com"),
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "multiple-matching-rules.example.com"),
		},
	}, {
		Name:    "Knative Route reports a matching cluster-local rule without HTTP",
		Key:     "default/missing-http.example.com",
		WantErr: true,
		Objects: func() []runtime.Object {
			route := standaloneRoute("default", "website")
			targetIngress := routeIngress(route)
			targetIngress.Spec.Rules[0].HTTP = nil
			return []runtime.Object{
				route,
				targetIngress,
				domainMapping("default", "missing-http.example.com", withRef("default", "website", asKnativeRouteRef)),
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "missing-http.example.com",
				withRef("default", "website", asKnativeRouteRef),
				withURL("http", "missing-http.example.com"),
				withAddress("http", "missing-http.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "missing-http.example.com")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "missing-http.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "missing-http.example.com"),
			Eventf(corev1.EventTypeWarning, "InternalError", "target Ingress default/website has a matching cluster-local rule without HTTP"),
		},
	}, {
		Name: "target Ingress has no matching cluster-local rule",
		Key:  "default/no-matching-rule.example.com",
		Objects: func() []runtime.Object {
			route := standaloneRoute("default", "website")
			targetIngress := routeIngress(route)
			targetIngress.Spec.Rules[0].Hosts = []string{"another.default.svc.cluster.local"}
			dm := domainMapping("default", "no-matching-rule.example.com", withRef("default", "website", asKnativeRouteRef))
			return []runtime.Object{
				route,
				targetIngress,
				resources.MakeIngress(dm, "old-revision", targetHost, "the-ingress-class", netv1alpha1.HTTPOptionEnabled, nil),
				dm,
			}
		}(),
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: domainMapping("default", "no-matching-rule.example.com",
				withRef("default", "website", asKnativeRouteRef),
				withURL("http", "no-matching-rule.example.com"),
				withAddress("http", "no-matching-rule.example.com"),
				withInitDomainMappingConditions,
				withTLSNotEnabled,
				withDomainClaimed,
				withReferenceResolved,
				withTargetIngressNotConfigured(`Ingress default/website has no cluster-local rule for host "website.default.svc.cluster.local".`),
			),
		}},
		SkipNamespaceValidation: true,
		WantCreates: []runtime.Object{
			resources.MakeDomainClaim(domainMapping("default", "no-matching-rule.example.com")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "no-matching-rule.example.com"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "no-matching-rule.example.com"),
		},
	}}

	return rows
}

func readyCertStatus() netv1alpha1.CertificateStatus {
	certStatus := &netv1alpha1.CertificateStatus{}
	certStatus.MarkReady()
	return *certStatus
}

func service(ns, name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
}

func withIngressNotReady(ing *netv1alpha1.Ingress) {
	ing.Status.MarkIngressNotReady("progressing", "hold your horses")
}

func withIngressTLS(tls netv1alpha1.IngressTLS) func(ing *netv1alpha1.Ingress) {
	return func(ing *netv1alpha1.Ingress) {
		ing.Spec.TLS = []netv1alpha1.IngressTLS{tls}
	}
}

func patchAddFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	p := fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":""}}`, servingv1.Resource("domainmappings").String())
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(p),
	}
}

func patchRemoveFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(`{"metadata":{"finalizers":[],"resourceVersion":""}}`),
	}
}

func makeTestReconciler(ctx context.Context, listers *Listers) *Reconciler {
	testTracker := ctx.Value(TrackerKey).(tracker.Interface)
	return &Reconciler{
		certificateLister: listers.GetCertificateLister(),
		ingressLister:     listers.GetIngressLister(),
		serviceLister:     listers.GetServiceLister(),
		routeLister:       listers.GetRouteLister(),
		domainClaimLister: listers.GetDomainClaimLister(),
		netclient:         networkingclient.Get(ctx),
		resolver:          resolver.NewURIResolverFromTracker(ctx, testTracker),
		tracker:           testTracker,
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgreconciler.ConfigStore = (*testConfigStore)(nil)
