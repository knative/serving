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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
	. "knative.dev/serving/pkg/testing/v1"
)

var fakeCurTime = time.Unix(1e9, 0)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	now := metav1.Time{Time: fakeCurTime}

	table := TableTest{{
		Name: "change configurations",
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
			patchRemoveRouteAndServingStateLabel(
				"default", rev("default", "old-config").Name, now.Time),
			patchAddRouteAndServingStateLabel(
				"default", rev("default", "new-config").Name, "config-change", now.Time),
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route"),
			patchAddLabel("default", "new-config", "serving.knative.dev/route", "config-change"),
		},
		Key: "default/config-change",
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			client:              servingclient.Get(ctx),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
			clock:               FakeClock{Time: fakeCurTime},
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
		TypeMeta: metav1.TypeMeta{
			Kind: "Revision",
		},
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
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(
		`{"metadata":{"annotations":{"serving.knative.dev/routingStateModified":%q},`+
			`"labels":{"serving.knative.dev/route":null,`+
			`"serving.knative.dev/routingState":"reserve"}}}`, now.Format(time.RFC3339))

	action.Patch = []byte(patch)
	return action
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

	patch := fmt.Sprintf(
		`{"metadata":{"annotations":{"serving.knative.dev/routingStateModified":%q},`+
			`"labels":{"serving.knative.dev/route":%q,`+
			`"serving.knative.dev/routingState":"active"}}}`, now.Format(time.RFC3339), routeName)

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
