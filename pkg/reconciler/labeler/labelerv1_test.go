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

package labeler

import (
	"context"
	"fmt"
	"testing"

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
	"knative.dev/pkg/logging"
	cfgmap "knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/pkg/reconciler/testing"
	labelerv1 "knative.dev/serving/pkg/reconciler/labeler/v1"
	labelerv2 "knative.dev/serving/pkg/reconciler/labeler/v2"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

func TestV1Reconcile(t *testing.T) {
	now := metav1.Now()
	fakeTime := now.Time

	table := TableTest{{
		Name: "bad workqueue key",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "label runLatest configuration",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "first-reconcile"),
			patchAddLabel("default", rev("default", "the-config").Name,
				"serving.knative.dev/route", "first-reconcile"),
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "first-reconcile"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "first-reconcile"),
		},
		Key: "default/first-reconcile",
	}, {
		Name: "label pinned revision",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			pinnedRoute("default", "pinned-revision", "the-revision"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
			rev("default", "the-config", WithRevName("the-revision")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "pinned-revision"),
			patchAddLabel("default", "the-revision",
				"serving.knative.dev/route", "pinned-revision"),
			patchAddLabel("default", "the-config",
				"serving.knative.dev/route", "pinned-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "pinned-revision"),
		},
		Key: "default/pinned-revision",
	}, {
		Name: "steady state",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "steady-state")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "steady-state")),
		},
		Key: "default/steady-state",
	}, {
		Name: "no ready revision",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "no-ready-revision", "the-config", WithStatusTraffic()),
			simpleConfig("default", "the-config", WithLatestReady("")),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddFinalizerAction("default", "no-ready-revision"),
			patchAddLabel("default", rev("default", "the-config").Name,
				"serving.knative.dev/route", "no-ready-revision"),
			patchAddLabel("default", "the-config",
				"serving.knative.dev/route", "no-ready-revision"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "FinalizerUpdate", "Updated %q finalizers", "no-ready-revision"),
		},
		Key: "default/no-ready-revision",
	}, {
		Name: "transitioning route",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "transitioning-route", "old", WithRouteFinalizer,
				WithSpecTraffic(configTraffic("new"))),
			simpleConfig("default", "old",
				WithConfigLabel("serving.knative.dev/route", "transitioning-route")),
			rev("default", "old",
				WithRevisionLabel("serving.knative.dev/route", "transitioning-route")),
			simpleConfig("default", "new"),
			rev("default", "new"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", rev("default", "new").Name,
				"serving.knative.dev/route", "transitioning-route"),
			patchAddLabel("default", "new",
				"serving.knative.dev/route", "transitioning-route"),
		},
		Key: "default/transitioning-route",
	}, {
		Name: "failure adding label (revision)",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
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
			patchAddLabel("default", rev("default", "the-config").Name,
				"serving.knative.dev/route", "add-label-failure"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route label to /, Kind= "the-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/add-label-failure",
	}, {
		Name: "failure adding label (configuration)",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config"),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "add-label-failure")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "add-label-failure"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to add route label to /, Kind= "the-config": inducing failure for patch configurations`),
		},
		Key: "default/add-label-failure",
	}, {
		Name:    "label config with incorrect label",
		Ctx:     setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
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
				`/, Kind= "the-config-dbnfd" is already in use by "another-route", and cannot be used by "the-route"`),
		},
		Key: "default/the-route",
	}, {
		Name: "change configurations",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-change", "new-config", WithRouteFinalizer),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "config-change")),
			rev("default", "old-config",
				WithRevisionLabel("serving.knative.dev/route", "config-change")),
			simpleConfig("default", "new-config"),
			rev("default", "new-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", rev("default", "old-config").Name,
				"serving.knative.dev/route"),
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route"),
			patchAddLabel("default", rev("default", "new-config").Name,
				"serving.knative.dev/route", "config-change"),
			patchAddLabel("default", "new-config", "serving.knative.dev/route", "config-change"),
		},
		Key: "default/config-change",
	}, {
		Name: "update configuration",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-update", "the-config", WithRouteFinalizer),
			simpleConfig("default", "the-config",
				WithLatestCreated("the-config-ecoge"),
				WithConfigLabel("serving.knative.dev/route", "config-update")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "config-update")),
			rev("default", "the-config",
				WithRevName("the-config-ecoge")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", "the-config-ecoge",
				"serving.knative.dev/route", "config-update"),
		},
		Key: "default/config-update",
	}, {
		Name: "delete route",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
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
		Name:    "delete route failure",
		Ctx:     setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "delete-route", "the-config", WithRouteFinalizer, WithRouteDeletionTimestamp(&now)),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "delete-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", "the-config", "serving.knative.dev/route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route label to /, Kind= "the-config": inducing failure for patch configurations`),
		},
		Key: "default/delete-route",
	}, {
		Name: "failure while removing a cfg annotation should return an error",
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
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
				WithRevisionLabel("serving.knative.dev/route", "delete-label-failure")),
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
		Ctx:  setResponsiveGCFeature(context.Background(), cfgmap.Disabled),
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
			patchRemoveLabel("default", rev("default", "old-config").Name,
				"serving.knative.dev/route"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "InternalError",
				`failed to remove route label to /, Kind= "old-config-dbnfd": inducing failure for patch revisions`),
		},
		Key: "default/delete-label-failure",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		ctx = setResponsiveGCFeature(ctx, cfgmap.Disabled)
		clock := clock.NewFakeClock(fakeTime)
		client := servingclient.Get(ctx)
		cLister := listers.GetConfigurationLister()
		cIndexer := listers.IndexerFor(&v1.Configuration{})
		rLister := listers.GetRevisionLister()
		rIndexer := listers.IndexerFor(&v1.Revision{})
		r := &Reconciler{
			caccV1: labelerv1.NewConfigurationAccessor(client, &NullTracker{}, cLister),
			caccV2: labelerv2.NewConfigurationAccessor(client, &NullTracker{}, cLister, cIndexer, clock),
			raccV1: labelerv1.NewRevisionAccessor(client, &NullTracker{}, rLister),
			raccV2: labelerv2.NewRevisionAccessor(client, &NullTracker{}, rLister, rIndexer, clock),
		}

		return routereconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetRouteLister(), controller.GetEventRecorder(ctx), r)
	}))
}

func patchRemoveLabel(namespace, name, key string) clientgotesting.PatchActionImpl {
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:null}}}`, key)),
	}
}

func patchAddLabel(namespace, name, key, value string) clientgotesting.PatchActionImpl {
	return clientgotesting.PatchActionImpl{
		Name:       name,
		ActionImpl: clientgotesting.ActionImpl{Namespace: namespace},
		Patch:      []byte(fmt.Sprintf(`{"metadata":{"labels":{%q:%q}}}`, key, value)),
	}
}
