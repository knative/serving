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

package routephase

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	reconcilerv1alpha1 "github.com/knative/serving/pkg/reconciler/v1alpha1"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestRouteReconcile_Triggers(t *testing.T) {
	triggers := (&Reconciler{}).Triggers()

	expected := []reconciler.Trigger{{
		ObjectKind:  v1alpha1.SchemeGroupVersion.WithKind("Route"),
		EnqueueType: reconciler.EnqueueObject,
	}}

	if diff := cmp.Diff(expected, triggers); diff != "" {
		t.Errorf("unexpected diff for the route triggers (-want,+got) %s", diff)

	}
}

func TestRouteReconcile(t *testing.T) {
	scenarios := ReconcilerTests{{
		Name: "missing route",
		Key:  "default/missing-route",
	}, {
		Name: "bad key",
		Key:  "bad/key/with/many/parts",
	}, {
		Name: "new route",
		Key:  "default/new-route",
		Objects: Objects{
			simpleRunLatest("default", "new-route", "config", nil),
		},
		ExpectedUpdates: Updates{
			simpleRunLatest("default", "new-route", "config", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			}),
		},
	}, {
		Name: "update route status failed",
		Key:  "default/update-route-failed",
		Failures: Failures{
			InduceFailure("update", "routes"),
		},
		Objects: Objects{
			simpleRunLatest("default", "update-route-failed", "config", nil),
		},
		ExpectError: true,
		ExpectedUpdates: Updates{
			simpleRunLatest("default", "update-route-failed", "config", &v1alpha1.RouteStatus{
				Conditions: duckv1alpha1.Conditions{{
					Type:   v1alpha1.RouteConditionAllTrafficAssigned,
					Status: corev1.ConditionUnknown,
				}, {
					Type:   v1alpha1.RouteConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			}),
		},
	}}

	scenarios.Run(t, ReconcilerSetup(reconcilerWithNoPhases))
}

func TestRouteReconcile_DelegatesToPhases(t *testing.T) {
	firstPhase := &mockPhase{}
	secondPhase := &mockPhase{}

	r := testReconciler(t, simpleRunLatest("default", "some-route", "config", nil))
	r.RoutePhases = []RoutePhase{
		firstPhase,
		secondPhase,
	}

	if got, want := len(r.Phases()), 2; got != want {
		t.Errorf("unexpected phases size got %d != want %d", got, want)
	}

	ctx := TestContextWithLogger(t)
	err := r.Reconcile(ctx, "default/some-route")

	if err != nil {
		t.Errorf("Expected Reconcile() to not return an error - %s", err)
	}

	if got := firstPhase.reconcileInvocations; got != 1 {
		t.Errorf("Expected the first phase to be reconciled only once - got %d", got)
	}

	if got := secondPhase.reconcileInvocations; got != 1 {
		t.Errorf("Expected the first phase to be reconciled only once - got %d", got)
	}
}

func reconcilerWithNoPhases(
	opts reconciler.CommonOptions,
	deps *reconcilerv1alpha1.DependencyFactory,
) reconciler.Reconciler {

	reconciler := New(opts, deps).(*Reconciler)
	reconciler.Configs = &FakeConfigStore{}
	reconciler.RoutePhases = nil
	return reconciler
}

func testReconciler(t *testing.T, objs ...runtime.Object) *Reconciler {
	opts := reconciler.CommonOptions{
		Logger:           TestLogger(t),
		Recorder:         &record.FakeRecorder{},
		ObjectTracker:    &NullTracker{},
		ConfigMapWatcher: &FakeConfigMapWatcher{},
		WorkQueue:        &FakeWorkQueue{},
	}

	deps := NewFakeDependencies(objs)
	reconciler := New(opts, deps).(*Reconciler)
	reconciler.Configs = &FakeConfigStore{}
	reconciler.RoutePhases = nil
	return reconciler
}

type mockPhase struct {
	triggers             []reconciler.Trigger
	route                *v1alpha1.Route
	reconcileInvocations int
	reconcile            func(context.Context, *v1alpha1.Route) (v1alpha1.RouteStatus, error)
}

func (p *mockPhase) Reconcile(ctx context.Context, route *v1alpha1.Route) (v1alpha1.RouteStatus, error) {
	p.reconcileInvocations++
	p.route = route.DeepCopy()

	if p.reconcile != nil {
		return p.reconcile(ctx, route)
	}
	return v1alpha1.RouteStatus{}, nil
}

func (p *mockPhase) Triggers() []reconciler.Trigger {
	return p.triggers
}

func simpleRunLatest(namespace, name, config string, status *v1alpha1.RouteStatus) *v1alpha1.Route {
	return routeWithTraffic(namespace, name, status, v1alpha1.TrafficTarget{
		ConfigurationName: config,
		Percent:           100,
	})
}

func routeWithTraffic(namespace, name string, status *v1alpha1.RouteStatus, traffic ...v1alpha1.TrafficTarget) *v1alpha1.Route {
	route := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.RouteSpec{
			Traffic: traffic,
		},
	}
	if status != nil {
		route.Status = *status
	}
	return route
}
