/*
Copyright 2026 The Knative Authors

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

package servicescaler

import (
	"context"
	"fmt"
	"testing"

	// Inject the fake informers that this controller needs.

	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestReconcile(t *testing.T) {
	now := metav1.Now()
	table := TableTest{
		{
			Name: "bad workqueue key",
			// Make sure Reconcile handles bad keys.
			Key: "too/many/parts",
		}, {
			Name: "key not found",
			// Make sure Reconcile handles good keys that don't exist.
			Key: "foo/not-found",
		}, {
			Name: "annotate service minscale with 1 revision",
			Objects: []runtime.Object{
				simpleRunLatest("default", "test-route", "revision-test", WithServiceMinScale("3"), WithRouteFinalizer),
				rev("default", "revision-test"),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "revision-test").Name, "3", "default/test-route"),
			},
			Key: "default/test-route",
		}, {
			Name: "add minscale annotation to two revisions split",
			Objects: []runtime.Object{
				routeWithTraffic("default", "test-service",
					[]v1.TrafficTarget{
						{
							RevisionName:   rev("default", "test-service").Name,
							Percent:        getInt64Pointer(50),
							LatestRevision: getBoolPointer(false),
						},
						{
							RevisionName:   rev("default", "test-service", WithRevName("the-revision")).Name,
							Percent:        getInt64Pointer(50),
							LatestRevision: getBoolPointer(true),
						},
					}, WithServiceMinScale("6"), WithRouteFinalizer,
				),
				rev("default", "test-service"),
				rev("default", "test-service", WithRevName("the-revision")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "3", "default/test-service"),
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service", WithRevName("the-revision")).Name, "3", "default/test-service"),
			},
			Key: "default/test-service",
		}, {
			Name: "steady state",
			Objects: []runtime.Object{
				simpleRunLatest("default", "steady-state", "test-service", WithServiceMinScale("3"), WithRouteFinalizer),
				rev("default", "test-service", WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3"), WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/steady-state")),
			},
			Key: "default/steady-state",
		}, {
			Name: "two routes one with higher min scale",
			Objects: []runtime.Object{
				simpleRunLatest("default", "first-route", "first-route", WithRouteFinalizer,
					WithSpecTraffic(configTraffic("new")), WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "3"})),
				simpleRunLatest("default", "second-route", "first-route", WithRouteFinalizer,
					WithSpecTraffic(configTraffic("new")), WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "5"})),
				rev("default", "first-route", WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3"), WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/first-route")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default",
					"first-route", WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3"),
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/first-route")).Name, "5", "default/second-route"),
			},
			Key: "default/second-route",
		}, {
			Name: "transition after deletion with previous annotations present (error case)",
			Objects: []runtime.Object{
				simpleRunLatest("default", "second-route", "test-service", WithRouteFinalizer,
					WithSpecTraffic(configTraffic("new")), WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "3"})),
				rev("default", "test-service", WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "5"), WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/first-route")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default",
					"test-service", WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "5"),
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/first-route")).Name, "3", "default/second-route"),
			},
			Key: "default/second-route",
		}, {
			Name: "steady state two routes",
			Objects: []runtime.Object{
				simpleRunLatest("default", "steady-state-1", "test-service", WithRouteFinalizer,
					WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "5"})),
				simpleRunLatest("default", "steady-state-2", "test-service", WithRouteFinalizer,
					WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "3"})),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "5"), WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/steady-state-1")),
			},
			Key: "default/steady-state-2",
		}, {
			Name: "add minscale annotation to two revisions split one zero",
			Objects: []runtime.Object{
				routeWithTraffic("default", "test-service",
					[]v1.TrafficTarget{
						{
							RevisionName:   rev("default", "test-service").Name,
							Percent:        getInt64Pointer(50),
							LatestRevision: getBoolPointer(false),
						},
						{
							RevisionName:   rev("default", "test-service", WithRevName("the-revision")).Name,
							Percent:        getInt64Pointer(50),
							LatestRevision: getBoolPointer(true),
						},
						{
							RevisionName:   rev("default", "test-service", WithRevName("the-revision-2")).Name,
							Percent:        getInt64Pointer(0),
							LatestRevision: getBoolPointer(false),
						},
					}, WithServiceMinScale("6"), WithRouteFinalizer,
				),
				rev("default", "test-service"),
				rev("default", "test-service", WithRevName("the-revision")),
				rev("default", "test-service", WithRevName("the-revision-2"),
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "6"), WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/test-service")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "3", "default/test-service"),
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service", WithRevName("the-revision")).Name, "3", "default/test-service"),
				patchRemoveServiceMinscaleAnnotationKey("default", rev("default", "test-service", WithRevName("the-revision-2")).Name),
			},
			Key: "default/test-service",
		}, {
			Name: "delete route existing ann",
			Objects: []runtime.Object{
				simpleRunLatest("default", "delete-route", "test-service", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/delete-route"),
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchRemoveServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name),
				patchRemoveFinalizerAction("default", "delete-route"),
			},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "FinalizerUpdate", `Updated "delete-route" finalizers`),
			},
			Key: "default/delete-route",
		}, {
			Name: "first reconcile with with finalizer",
			Objects: []runtime.Object{
				simpleRunLatest("default", "first-reconcile", "test-service", WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "3"})),
				rev("default", "test-service"),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddFinalizerAction("default", "first-reconcile"),
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "3", "default/first-reconcile"),
			},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile"),
			},
			Key: "default/first-reconcile",
		}, {
			Name: "route updated lower service minscale",
			Objects: []runtime.Object{
				simpleRunLatest("default", "test-route", "test-service", WithRouteFinalizer,
					WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "3"})),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "5"),
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/test-route")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "3", "default/test-route"),
			},
			Key: "default/test-route",
		}, {
			Name: "route updated higher service minscale",
			Objects: []runtime.Object{
				simpleRunLatest("default", "test-route", "test-service", WithRouteFinalizer,
					WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "5"})),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3"),
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/test-route")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "5", "default/test-route"),
			},
			Key: "default/test-route",
		}, {
			Name: "route deleted with unrelated revision in same namespace",
			Objects: []runtime.Object{
				simpleRunLatest("default", "delete-route", "other-service", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, "default/other-route"),
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchRemoveFinalizerAction("default", "delete-route"),
			},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "FinalizerUpdate", `Updated "delete-route" finalizers`),
			},
			Key: "default/delete-route",
		}, {
			Name: "re-reconcile blank route name",
			Objects: []runtime.Object{
				simpleRunLatest("default", "test-route", "test-service", WithRouteFinalizer,
					WithRouteAnnotation(map[string]string{serving.ServiceMinscaleAnnotationKey: "2"})),
				rev("default", "test-service",
					WithRevisionAnn(serving.ServiceMinscaleRouteAnnotationKey, ""),
					WithRevisionAnn(serving.ServiceMinscaleAnnotationKey, "3")),
			},
			WantPatches: []clientgotesting.PatchActionImpl{
				patchAddServiceMinscaleAnnotationKey("default", rev("default", "test-service").Name, "2", "default/test-route"),
			},
			Key: "default/test-route",
		},
	}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		client := servingclient.Get(ctx)
		rLister := listers.GetRevisionLister()
		rIndexer := listers.IndexerFor(&v1.Revision{})
		routeLister := listers.GetRouteLister()

		r := &Reconciler{
			client:          client,
			revisionLister:  rLister,
			revisionIndexer: rIndexer,
			routeLister:     routeLister,
			tracker:         &NullTracker{},
		}

		return routereconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetRouteLister(), controller.GetEventRecorder(ctx), r)
	}))
}

func configTraffic(name string) v1.TrafficTarget {
	return v1.TrafficTarget{
		ConfigurationName: name,
		Percent:           ptr.Int64(100),
		LatestRevision:    ptr.Bool(true),
	}
}

func revTraffic(name string, latest bool) v1.TrafficTarget {
	return v1.TrafficTarget{
		RevisionName:   name,
		Percent:        ptr.Int64(100),
		LatestRevision: ptr.Bool(latest),
	}
}

func routeWithTraffic(namespace, name string, status []v1.TrafficTarget, opts ...RouteOption) *v1.Route {
	return Route(namespace, name,
		append([]RouteOption{
			WithStatusTraffic(status...), WithInitRouteConditions,
			MarkTrafficAssigned, MarkCertificateReady, MarkIngressReady, WithRouteObservedGeneration,
		}, opts...)...)
}

func simpleRunLatest(namespace, name, serviceName string, opts ...RouteOption) *v1.Route {
	return routeWithTraffic(namespace, name,
		[]v1.TrafficTarget{revTraffic(serviceName+"-dbnfd", true)},
		opts...)
}

func simpleConfig(namespace, name string, opts ...ConfigOption) *v1.Configuration {
	cfg := &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            name,
			ResourceVersion: "v1",
		},
	}
	cfg.Status.InitializeConditions()
	cfg.Status.SetLatestCreatedRevisionName(name + "-dbnfd")
	cfg.Status.SetLatestReadyRevisionName(name + "-dbnfd")

	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

func rev(namespace, name string, opts ...RevisionOption) *v1.Revision {
	cfg := simpleConfig(namespace, name)
	rev := &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            cfg.Status.LatestReadyRevisionName,
			ResourceVersion: "v1",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cfg)},
		},
	}

	for _, opt := range opts {
		opt(rev)
	}
	return rev
}

func patchRemoveServiceMinscaleAnnotationKey(namespace, name string) clientgotesting.PatchActionImpl {
	return patchAddServiceMinscaleAnnotationKey(namespace, name, "null", "null")
}

func patchAddServiceMinscaleAnnotationKey(namespace, revisionName, serviceMinScale, routeKey string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{
		Name:       revisionName,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
	}

	// Note: the raw json `"key": null` removes a value, whereas an actual value
	// called "null" would need quotes to parse as a string `"key":"null"`.
	if serviceMinScale != "null" {
		serviceMinScale = `"` + serviceMinScale + `"`
	}
	if routeKey != "null" {
		routeKey = `"` + routeKey + `"`
	}

	action.Patch = []byte(fmt.Sprintf(
		`{"metadata":{"annotations":{"serving.knative.dev/service-min-scale":%s,"serving.knative.dev/service-min-scale-route":%s}}}`, serviceMinScale, routeKey))
	return action
}

func getInt64Pointer(val int) *int64 {
	p := int64(val)
	return &p
}

func getBoolPointer(val bool) *bool {
	return &val
}

func patchRemoveFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(`{"metadata":{"finalizers":[],"resourceVersion":""}}`),
	}
}

func patchAddFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	p := fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":""}}`, v1.Resource("routes").String())
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(p),
	}
}
