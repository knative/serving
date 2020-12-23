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

package labeler

import (
	"context"
	"fmt"
	"testing"
	"time"

	// Inject the fake informers that this controller needs.
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1/route/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgotesting "k8s.io/client-go/testing"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/system"
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	autoscalercfg "knative.dev/serving/pkg/autoscaler/config"

	. "knative.dev/pkg/reconciler/testing"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestV2Reconcile(t *testing.T) {
	now := metav1.Now()
	fakeTime := now.Time
	clock := clock.NewFakeClock(fakeTime)

	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "label runLatest configuration",
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile"),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "the-config").Name, "first-reconcile", now.Time),
			patchAddRouteAnn("default", "the-config", "first-reconcile"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile"),
		},
		Key: "default/first-reconcile",
	}, {
		Name: "label pinned revision",
		Objects: []runtime.Object{
			pinnedRoute("default", "pinned-revision", "the-revision"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
			rev("default", "the-config", WithRevName("the-revision")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "pinned-revision"),
			patchAddRouteAndServingStateLabel(
				"default", "the-revision", "pinned-revision", now.Time),
			patchAddRouteAnn("default", "the-config",
				"pinned-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "pinned-revision"),
		},
		Key: "default/pinned-revision",
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithConfigAnn("serving.knative.dev/routes", "steady-state")),
			rev("default", "the-config",
				WithRevisionAnn("serving.knative.dev/routes", "steady-state"),
				WithRoutingState(v1.RoutingStateActive, clock),
				WithRoutingStateModified(now.Time)),
		},
		Key: "default/steady-state",
	}, {
		Name: "no ready revision",
		Objects: []runtime.Object{
			simpleRunLatest("default", "no-ready-revision", "the-config", WithStatusTraffic()),
			simpleConfig("default", "the-config", WithLatestReady("")),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "no-ready-revision"),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "the-config").Name, "no-ready-revision", now.Time),
			patchAddRouteAnn("default", "the-config",
				"no-ready-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "no-ready-revision"),
		},
		Key: "default/no-ready-revision",
	}, {
		Name: "transitioning route",
		Objects: []runtime.Object{
			simpleRunLatest("default", "transitioning-route", "old", WithRouteFinalizer,
				WithSpecTraffic(configTraffic("new"))),
			simpleConfig("default", "old",
				WithConfigAnn("serving.knative.dev/routes", "transitioning-route")),
			rev("default", "old",
				WithRevisionAnn("serving.knative.dev/routes", "transitioning-route"),
				WithRoutingState(v1.RoutingStateActive, clock)),
			simpleConfig("default", "new"),
			rev("default", "new"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new").Name, "transitioning-route", now.Time),
			patchAddRouteAnn("default", "new",
				"transitioning-route"),
		},
		Key: "default/transitioning-route",
	}, {
		Name: "failure adding annotation (revision)",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "revisions"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "the-config").Name, "add-label-failure", now.Time),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route annotation to Namespace=default Name="the-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/add-label-failure",
	}, {
		Name: "failure adding annotation (configuration)",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config"),
			rev("default", "the-config",
				WithRevisionAnn("serving.knative.dev/routes", "add-label-failure"),
				WithRoutingState(v1.RoutingStateActive, clock),
				WithRoutingStateModified(now.Time)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAnn("default", "the-config", "add-label-failure"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route annotation to Namespace=default Name="the-config": inducing failure for patch configurations`),
		},
		Key: "default/add-label-failure",
	}, {
		Name: "delete route existing ann",
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-route", "the-config", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
			simpleConfig("default", "the-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-route,another-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAnn("default", "the-config", "another-route"),
			patchRemoveFinalizerAction("default", "delete-route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", `Updated "delete-route" finalizers`),
		},
		Key: "default/delete-route",
	}, {
		Name: "change configurations",
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-change", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigAnn("serving.knative.dev/routes", "config-change")),
			rev("default", "old-config",
				WithRevisionAnn("serving.knative.dev/routes", "config-change"),
				WithRoutingState(v1.RoutingStateActive, clock)),
			simpleConfig("default", "new-config"),
			rev("default", "new-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAndServingStateLabel("default", rev("default", "old-config").Name, now.Time),
			patchRemoveRouteAnn("default", "old-config"),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new-config").Name, "config-change", now.Time),
			patchAddRouteAnn("default", "new-config", "config-change"),
		},
		Key: "default/config-change",
	}, {
		Name: "update configuration",
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-update", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithLatestCreated("the-config-ecoge"),
				WithConfigAnn("serving.knative.dev/routes", "config-update")),
			rev("default", "the-config",
				WithRevisionAnn("serving.knative.dev/routes", "config-update"),
				WithRoutingState(v1.RoutingStateActive, clock)),
			rev("default", "the-config",
				WithRevName("the-config-ecoge")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAndServingStateLabel(
				"default", "the-config-ecoge", "config-update", now.Time),
		},
		Key: "default/config-update",
	}, {
		Name: "delete route",
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-route", "the-config", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
			simpleConfig("default", "the-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAnn("default", "the-config"),
			patchRemoveFinalizerAction("default", "delete-route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", `Updated "delete-route" finalizers`),
		},
		Key: "default/delete-route",
	}, {
		Name:    "delete route failure",
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-route", "the-config", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
			simpleConfig("default", "the-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAnn("default", "the-config"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route annotation to /, Kind= "the-config": inducing failure for patch configurations`),
		},
		Key: "default/delete-route",
	}, {
		Name: "failure while removing a cfg annotation should return an error",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-label-failure", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-label-failure")),
			simpleConfig("default", "new-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-label-failure")),
			rev("default", "new-config",
				WithRevisionAnn("serving.knative.dev/routes", "delete-label-failure"),
				WithRoutingState(v1.RoutingStateActive, clock),
				WithRoutingStateModified(now.Time)),
			rev("default", "old-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAnn("default", "old-config"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route annotation to /, Kind= "old-config": inducing failure for patch configurations`),
		},
		Key: "default/delete-label-failure",
	}, {
		Name: "failure while removing a rev annotation should return an error",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "revisions"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-label-failure", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-label-failure")),
			simpleConfig("default", "new-config",
				WithConfigAnn("serving.knative.dev/routes", "delete-label-failure")),
			rev("default", "new-config",
				WithRevisionAnn("serving.knative.dev/routes", "delete-label-failure")),
			rev("default", "old-config",
				WithRoutingState(v1.RoutingStateActive, clock),
				WithRevisionAnn("serving.knative.dev/routes", "delete-label-failure")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAndServingStateLabel("default", rev("default", "old-config").Name, now.Time),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route annotation to /, Kind= "old-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/delete-label-failure",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		client := servingclient.Get(ctx)
		cLister := listers.GetConfigurationLister()
		cIndexer := listers.IndexerFor(&v1.Configuration{})
		rLister := listers.GetRevisionLister()
		rIndexer := listers.IndexerFor(&v1.Revision{})
		r := &Reconciler{
			caccV2: labelerv2.NewConfigurationAccessor(client, &NullTracker{}, cLister, cIndexer, clock),
			raccV2: labelerv2.NewRevisionAccessor(client, &NullTracker{}, rLister, rIndexer, clock),
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

func routeWithTraffic(namespace, name string, spec, status v1.TrafficTarget, opts ...RouteOption) *v1.Route {
	return Route(namespace, name,
		append([]RouteOption{WithSpecTraffic(spec), WithStatusTraffic(status), WithInitRouteConditions,
			MarkTrafficAssigned, MarkCertificateReady, MarkIngressReady, WithRouteObservedGeneration}, opts...)...)
}

func simpleRunLatest(namespace, name, config string, opts ...RouteOption) *v1.Route {
	return routeWithTraffic(namespace, name,
		configTraffic(config),
		revTraffic(config+"-dbnfd", true),
		opts...)
}

func pinnedRoute(namespace, name, revision string, opts ...RouteOption) *v1.Route {
	traffic := revTraffic(revision, false)

	return routeWithTraffic(namespace, name, traffic, traffic, opts...)
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

func patchRemoveRouteAnn(namespace, name string) clientgotesting.PatchActionImpl {
	return patchAddRouteAnn(namespace, name, "null")
}

func patchAddRouteAnn(namespace, name, value string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
	}

	// Note: the raw json `"key": null` removes a value, whereas an actual value
	// called "null" would need quotes to parse as a string `"key":"null"`.
	if value != "null" {
		value = `"` + value + `"`
	}

	action.Patch = []byte(fmt.Sprintf(`{"metadata":{"annotations":{"serving.knative.dev/routes":%s}}}`, value))
	return action
}

func patchRemoveRouteAndServingStateLabel(namespace, name string, now time.Time) clientgotesting.PatchActionImpl {
	return patchAddRouteAndServingStateLabel(namespace, name, "null", now)
}

func patchAddRouteAndServingStateLabel(namespace, name, routeName string, now time.Time) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
	}

	// Note: the raw json `"key": null` removes a value, whereas an actual value
	// called "null" would need quotes to parse as a string `"key":"null"`.
	state := string(v1.RoutingStateReserve)
	if routeName != "null" {
		state = string(v1.RoutingStateActive)
		routeName = `"` + routeName + `"`
	}

	action.Patch = []byte(fmt.Sprintf(
		`{"metadata":{"annotations":{"serving.knative.dev/routes":%s,`+
			`"serving.knative.dev/routingStateModified":%q},`+
			`"labels":{"serving.knative.dev/routingState":%q}}}`, routeName, now.UTC().Format(time.RFC3339), state))
	return action
}

func patchAddFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	p := fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":""}}`, v1.Resource("routes").String())
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

func TestNew(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.FeaturesConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgmap.DefaultsConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      autoscalercfg.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{},
	})

	c := NewController(ctx, configMapWatcher)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}
