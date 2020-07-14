/*
Copyright 2018 The Knative Authors.

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
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestV2Reconcile(t *testing.T) {
	now := metav1.Now()
	fakeTime := now.Time

	table := TableTest{{
		Name: "bad workqueue key",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "label runLatest configuration",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile"),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "the-config").Name, "first-reconcile", now.Time),
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "first-reconcile"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile"),
		},
		Key: "default/first-reconcile",
	}, {
		Name: "label pinned revision",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
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
			patchAddLabel("default", "the-config",
				"serving.knative.dev/route", "pinned-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "pinned-revision"),
		},
		Key: "default/pinned-revision",
	}, {
		Name: "steady state",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "steady-state")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "steady-state"),
				WithRoutingState(v1.RoutingStateActive),
				WithRoutingStateModified(now.Time)),
		},
		Key: "default/steady-state",
	}, {
		Name: "no ready revision",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "no-ready-revision", "the-config", WithStatusTraffic()),
			simpleConfig("default", "the-config", WithLatestReady("")),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "no-ready-revision"),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "the-config").Name, "no-ready-revision", now.Time),
			patchAddLabel("default", "the-config",
				"serving.knative.dev/route", "no-ready-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "no-ready-revision"),
		},
		Key: "default/no-ready-revision",
	}, {
		Name: "transitioning route",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "transitioning-route", "old", WithRouteFinalizer,
				WithSpecTraffic(configTraffic("new"))),
			simpleConfig("default", "old",
				WithConfigLabel("serving.knative.dev/route", "transitioning-route")),
			rev("default", "old",
				WithRevisionLabel("serving.knative.dev/route", "transitioning-route"),
				WithRoutingState(v1.RoutingStateActive)),
			simpleConfig("default", "new"),
			rev("default", "new"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new").Name, "transitioning-route", now.Time),
			patchAddLabel("default", "new",
				"serving.knative.dev/route", "transitioning-route"),
		},
		Key: "default/transitioning-route",
	}, {
		Name: "failure adding label (revision)",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
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
				`failed to add route label to Namespace=default Name="the-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/add-label-failure",
	}, {
		Name: "failure adding label (configuration)",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config"),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "add-label-failure"),
				WithRoutingState(v1.RoutingStateActive),
				WithRoutingStateModified(now.Time)),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "add-label-failure"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route label to Namespace=default Name="the-config": inducing failure for patch configurations`),
		},
		Key: "default/add-label-failure",
	}, {
		Name:    "label config with incorrect label",
		Ctx:     setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		WantErr: true,
		Objects: []runtime.Object{
			simpleRunLatest("default", "the-route", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "another-route")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "another-route")),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route label to Namespace=default Name="the-config-dbnfd": `+
					`resource already has route label "another-route", and cannot be referenced by "the-route"`),
		},
		Key: "default/the-route",
	}, {
		Name: "change configurations",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-change", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "config-change")),
			rev("default", "old-config",
				WithRevisionLabel("serving.knative.dev/route", "config-change"),
				WithRoutingState(v1.RoutingStateActive)),
			simpleConfig("default", "new-config"),
			rev("default", "new-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAndServingStateLabel(
				"default", rev("default", "old-config").Name, now.Time),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new-config").Name, "config-change", now.Time),
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route"),
			patchAddLabel("default", "new-config", "serving.knative.dev/route", "config-change"),
		},
		Key: "default/config-change",
	}, {
		Name: "update configuration",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-update", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithLatestCreated("the-config-ecoge"),
				WithConfigLabel("serving.knative.dev/route", "config-update")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "config-update"),
				WithRoutingState(v1.RoutingStateActive)),
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
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-route", "the-config", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "delete-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", "the-config", "serving.knative.dev/route"),
			patchRemoveFinalizerAction("default", "delete-route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", `Updated "delete-route" finalizers`),
		},
		Key: "default/delete-route",
	}, {
		Name: "failure while removing a cfg annotation should return an error",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-label-failure", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			simpleConfig("default", "new-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			rev("default", "new-config",
				WithRevisionLabel("serving.knative.dev/route", "delete-label-failure"),
				WithRoutingState(v1.RoutingStateActive),
				WithRoutingStateModified(now.Time)),
			rev("default", "old-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route label to /, Kind= "old-config": inducing failure for patch configurations`),
		},
		Key: "default/delete-label-failure",
	}, {
		Name: "failure while removing a rev annotation should return an error",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Enabled),
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "revisions"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-label-failure", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			simpleConfig("default", "new-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			rev("default", "new-config",
				WithRevisionLabel("serving.knative.dev/route", "delete-label-failure")),
			rev("default", "old-config",
				WithRevisionLabel("serving.knative.dev/route", "delete-label-failure")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveRouteAndServingStateLabel(
				"default", rev("default", "old-config").Name, now.Time),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route label to /, Kind= "old-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/delete-label-failure",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			client:              servingclient.Get(ctx),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			tracker:             &NullTracker{},
			clock:               clock.NewFakeClock(fakeTime),
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
		append([]RouteOption{WithSpecTraffic(spec), WithStatusTraffic(status), WithInitRouteConditions}, opts...)...)
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

func patchRemoveLabel(namespace, name, key string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, key)

	action.Patch = []byte(patch)
	return action
}

func patchRemoveRouteAndServingStateLabel(namespace, name string, now time.Time) clientgotesting.PatchActionImpl {
	return patchAddRouteAndServingStateLabel(namespace, name, "null", now)
}

func patchAddLabel(namespace, name, key, value string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, key, value)

	action.Patch = []byte(patch)
	return action
}

func patchAddRouteAndServingStateLabel(namespace, name, routeName string, now time.Time) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	state := string(v1.RoutingStateReserve)

	// Note: the raw json `"key": null` removes a value, whereas an actual value
	// called "null" would need quotes to parse as a string `"key":"null"`.
	if routeName != "null" {
		state = string(v1.RoutingStateActive)
		routeName = `"` + routeName + `"`
	}

	patch := fmt.Sprintf(
		`{"metadata":{"annotations":{"serving.knative.dev/routingStateModified":%q},`+
			`"labels":{"serving.knative.dev/route":%s,`+
			`"serving.knative.dev/routingState":%q}}}`, now.UTC().Format(time.RFC3339), routeName, state)

	action.Patch = []byte(patch)
	return action
}

func patchAddFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	resource := v1.Resource("routes")
	finalizer := resource.String()

	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace
	patch := fmt.Sprintf(`{"metadata":{"finalizers":[%q],"resourceVersion":""}}`, finalizer)
	action.Patch = []byte(patch)
	return action
}

func patchRemoveFinalizerAction(namespace, name string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace
	action.Patch = []byte(`{"metadata":{"finalizers":[],"resourceVersion":""}}`)
	return action
}

func TestNew(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	c := NewController(ctx, configmap.NewStaticWatcher())

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

func setResponsiveGCFeature(ctx context.Context, flag cfgmap.Flag) context.Context {
	c := cfgmap.FromContextOrDefaults(ctx)
	c.Features.ResponsiveRevisionGC = flag
	return cfgmap.ToContext(ctx, c)
}
