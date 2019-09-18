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

	// Inject the fake informers that this controller needs.
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/configuration/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision/fake"
	_ "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/route/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	logtesting "knative.dev/pkg/logging/testing"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/reconciler"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"
	. "knative.dev/serving/pkg/testing/v1alpha1"
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
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
		Name: "label runLatest configuration",
		Objects: []runtime.Object{
			simpleRunLatest("default", "first-reconcile", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", rev("default", "the-config").Name,
				"serving.knative.dev/route", "first-reconcile", "v1"),
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "first-reconcile", "v1"),
		},
		Key: "default/first-reconcile",
	}, {
		Name: "steady state",
		Objects: []runtime.Object{
			simpleRunLatest("default", "steady-state", "the-config"),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "steady-state")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "steady-state")),
		},
		Key: "default/steady-state",
	}, {
		Name: "failure adding label (revision)",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "revisions"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", rev("default", "the-config").Name,
				"serving.knative.dev/route", "add-label-failure", "v1"),
		},
		Key: "default/add-label-failure",
	}, {
		Name: "failure adding label (configuration)",
		// Induce a failure during patching
		WantErr: true,
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("patch", "configurations"),
		},
		Objects: []runtime.Object{
			simpleRunLatest("default", "add-label-failure", "the-config"),
			simpleConfig("default", "the-config"),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "add-label-failure")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchAddLabel("default", "the-config", "serving.knative.dev/route", "add-label-failure", "v1"),
		},
		Key: "default/add-label-failure",
	}, {
		Name:    "label config with incorrect label",
		WantErr: true,
		Objects: []runtime.Object{
			simpleRunLatest("default", "the-route", "the-config"),
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "another-route")),
			rev("default", "the-config",
				WithRevisionLabel("serving.knative.dev/route", "another-route")),
		},
		Key: "default/the-route",
	}, {
		Name: "change configurations",
		Objects: []runtime.Object{
			simpleRunLatest("default", "config-change", "new-config"),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "config-change")),
			rev("default", "old-config",
				WithRevisionLabel("serving.knative.dev/route", "config-change")),
			simpleConfig("default", "new-config"),
			rev("default", "new-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", rev("default", "old-config").Name,
				"serving.knative.dev/route", "v1"),
			patchAddLabel("default", rev("default", "new-config").Name,
				"serving.knative.dev/route", "config-change", "v1"),
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route", "v1"),
			patchAddLabel("default", "new-config", "serving.knative.dev/route", "config-change", "v1"),
		},
		Key: "default/config-change",
	}, {
		Name: "delete route",
		Objects: []runtime.Object{
			simpleConfig("default", "the-config",
				WithConfigLabel("serving.knative.dev/route", "delete-route")),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", "the-config", "serving.knative.dev/route", "v1"),
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
			simpleRunLatest("default", "delete-label-failure", "new-config"),
			simpleConfig("default", "old-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			simpleConfig("default", "new-config",
				WithConfigLabel("serving.knative.dev/route", "delete-label-failure")),
			rev("default", "new-config",
				WithRevisionLabel("serving.knative.dev/route", "delete-label-failure")),
			rev("default", "old-config"),
		},
		WantPatches: []clientgotesting.PatchActionImpl{
			patchRemoveLabel("default", "old-config", "serving.knative.dev/route", "v1"),
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
			simpleRunLatest("default", "delete-label-failure", "new-config"),
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
				"serving.knative.dev/route", "v1"),
		},
		Key: "default/delete-label-failure",
	}}

	defer logtesting.ClearAll()
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(ctx, controllerAgentName, cmw),
			routeLister:         listers.GetRouteLister(),
			configurationLister: listers.GetConfigurationLister(),
			revisionLister:      listers.GetRevisionLister(),
		}
	}))
}

func routeWithTraffic(namespace, name string, traffic ...v1alpha1.TrafficTarget) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Status: v1alpha1.RouteStatus{
			RouteStatusFields: v1alpha1.RouteStatusFields{
				Traffic: traffic,
			},
		},
	}
}

func simpleRunLatest(namespace, name, config string) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			RevisionName: config + "-dbnfd",
			Percent:      ptr.Int64(100),
		},
	})
}

func simpleConfig(namespace, name string, opts ...ConfigOption) *v1alpha1.Configuration {
	cfg := &v1alpha1.Configuration{
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

func rev(namespace, name string, opts ...RevisionOption) *v1alpha1.Revision {
	cfg := simpleConfig(namespace, name)
	rev := &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       namespace,
			Name:            cfg.Status.LatestCreatedRevisionName,
			ResourceVersion: "v1",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(cfg)},
		},
	}

	for _, opt := range opts {
		opt(rev)
	}
	return rev
}

func patchRemoveLabel(namespace, name, key, version string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{"%s":null},"resourceVersion":"%s"}}`, key, version)

	action.Patch = []byte(patch)
	return action
}

func patchAddLabel(namespace, name, key, value, version string) clientgotesting.PatchActionImpl {
	action := clientgotesting.PatchActionImpl{}
	action.Name = name
	action.Namespace = namespace

	patch := fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"},"resourceVersion":"%s"}}`, key, value, version)

	action.Patch = []byte(patch)
	return action
}

func TestNew(t *testing.T) {
	defer logtesting.ClearAll()
	ctx, _ := SetupFakeContext(t)

	c := NewController(ctx, configmap.NewStaticWatcher())

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}
