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

package route

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	fakenetworkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakeingressinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/ingress/fake"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakerevisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1/revision/fake"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/traffic"

	. "knative.dev/serving/pkg/testing/v1"
)

func TestReconcileIngressInsert(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")
	tc, tls := testIngressParams(t, r)
	_, ro, err := reconciler.reconcileIngress(updateContext(ctx, 0), r, tc, tls, "foo-ingress")
	if err != nil {
		t.Error("Unexpected error:", err)
	}
	if got, want := ro, tc.BuildRollout(); !cmp.Equal(got, want) {
		t.Errorf("Default rollout mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
	}
}

func TestReconcileIngressUpdateReenqueueRollout(t *testing.T) {
	var reconciler *Reconciler
	const (
		fakeCurSecs      = 19551982
		fakeCurNsec      = 19841988
		fakeCurTime      = fakeCurSecs*1_000_000_000 + fakeCurNsec // *1e9
		allocatedTraffic = 48
	)
	fakeClock := clock.NewFakePassiveClock(time.Unix(fakeCurSecs, fakeCurNsec))
	baseCtx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		r.clock = fakeClock
		reconciler = r
	})
	defer cancel()

	for _, rd := range []int{120, 600, 3600} {
		rds := strconv.Itoa(rd)
		t.Run(rds, func(t *testing.T) {
			r := Route("test-ns-"+rds, "rollout-route")

			tc, tls := testIngressParams(t, r, func(tc *traffic.Config) {
				tc.Targets[traffic.DefaultTarget] = traffic.RevisionTargets{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "thor",
						RevisionName:      "thursday",
						Percent:           ptr.Int64(52),
						LatestRevision:    ptr.Bool(true),
					},
					Protocol: networking.ProtocolHTTP1,
				}, {
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "odin",
						RevisionName:      "wednesday",
						Percent:           ptr.Int64(allocatedTraffic),
						LatestRevision:    ptr.Bool(true),
					},
					Protocol: networking.ProtocolHTTP1,
				}}
			})

			fakeClock.SetTime(time.Unix(fakeCurSecs, fakeCurNsec))
			wasReenqueued := false
			duration := 0 * time.Second
			reconciler.enqueueAfter = func(_ interface{}, d time.Duration) {
				wasReenqueued = true
				duration = d
			}
			ctx := updateContext(baseCtx, rd)
			_, ro, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress-class")
			if err != nil {
				t.Fatal("Unexpected error:", err)
			}

			if got, want := ro, tc.BuildRollout(); !cmp.Equal(got, want) {
				t.Errorf("Complex initial rollout mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
			}

			ing := getRouteIngressFromClient(ctx, t, r)
			fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ing)

			// Now we have initial version. Let's make a rollout.
			tc.Targets[traffic.DefaultTarget][1].RevisionName = "miercoles"

			_, updRO, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress-class")
			if err != nil {
				t.Error("Unexpected error:", err)
			}

			// This should update the rollout with the new annotation.
			updated := getRouteIngressFromClient(ctx, t, r)

			if got, want := updated.Annotations[networking.RolloutAnnotationKey],
				ing.Annotations[networking.RolloutAnnotationKey]; got == want {
				t.Fatal("Expected difference in the rollout annotation, but found none")
			}
			if wasReenqueued {
				t.Fatal("Unexpected re-enqueuing happened")
			}

			ro = deserializeRollout(ctx, updated.Annotations[networking.RolloutAnnotationKey])
			if ro == nil {
				t.Fatal("Rollout was bogus and impossible to deserialize")
			}

			// Verify the same rollout was returned by the ReconcileResources.
			if !cmp.Equal(ro, updRO) {
				t.Errorf("Returned and Annotation Rollouts differ: diff(-ret,+ann):\n%s", cmp.Diff(updRO, ro))
			}

			// After sorting `odin` will come first.
			if got, want := ro.Configurations[0].StepParams.StartTime, int64(fakeCurTime); got != want {
				t.Errorf("Rollout StartTime = %d, want: %d", got, want)
			}

			// OK. So now let's observe ready to start the rollout. For that we need to make ingress ready.
			cp := updated.DeepCopy()
			cp.Status.MarkLoadBalancerReady(nil, nil)
			cp.Status.MarkNetworkConfigured()
			fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(cp)
			// For comparisons.
			ing = updated

			// Advance the time by 5s.
			const drift = 5
			fakeClock.SetTime(fakeClock.Now().Add(drift * time.Second))

			// Drift seconds of the duration already spent.
			totalDuration := float64(rd - drift)
			// Step count is minimum of % points of traffic allocated to the config (1% already shifted).
			steps := math.Min(totalDuration/drift, allocatedTraffic-1)
			stepSize := math.Max(1, math.Round((allocatedTraffic-1)/steps)) // we round the step size.
			stepDuration := time.Duration(int(totalDuration * float64(time.Second) / steps))

			_, updRO, err = reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress")
			if err != nil {
				t.Error("Unexpected error:", err)
			}

			// This should update the rollout with the new annotation with steps and stuff.
			updated = getRouteIngressFromClient(ctx, t, r)

			if got, want := updated.Annotations[networking.RolloutAnnotationKey],
				ing.Annotations[networking.RolloutAnnotationKey]; got == want {
				t.Fatal("Expected difference in the rollout annotation, but found none")
			}
			ro = deserializeRollout(ctx, updated.Annotations[networking.RolloutAnnotationKey])
			if ro == nil {
				t.Fatal("Rollout was bogus and impossible to deserialize")
			}

			// Verify NextStepTime, StepDuration and StepSize are set as expected.
			// There is some rounding possible for various durations, so leave a 5ns leeway.
			if diff := math.Abs(float64(ro.Configurations[0].StepParams.NextStepTime - fakeClock.Now().Add(stepDuration).UnixNano())); diff > 5 {
				t.Errorf("NextStepTime = %d, want: %d±5ns; diff = %vns",
					ro.Configurations[0].StepParams.NextStepTime, fakeClock.Now().Add(stepDuration).UnixNano(), diff)
			}
			if got, want := ro.Configurations[0].StepParams.StepDuration, int64(stepDuration); got != want {
				t.Errorf("StepDuration = %d, want: %d", got, want)
			}

			// Verify the same rollout was returned by the ReconcileResources.
			if !cmp.Equal(ro, updRO) {
				t.Errorf("Returned and Annotation Rollouts after re-enqueue differ: diff(-ret,+ann):\n%s",
					cmp.Diff(updRO, ro))
			}

			if got, want := ro.Configurations[0].StepParams.StepSize, int(stepSize); got != want {
				t.Errorf("StepSize = %d, want: %d", got, want)
			}
			if !wasReenqueued {
				t.Fatal("Re-enqueuing didn't happen")
			}
			if got, want, diff := duration, stepDuration, math.Abs(float64(duration-stepDuration)); diff > 5 {
				t.Errorf("Re-enqueue duration = %v, want: %v±5ns; diff = %vns", got, want, diff)
			}
		})
	}
}

func TestReconcileIngressUpdateReenqueueRolloutAnnotation(t *testing.T) {
	var reconciler *Reconciler
	const (
		fakeCurSecs      = 19551982
		fakeCurNsec      = 19841988
		fakeCurTime      = fakeCurSecs*1_000_000_000 + fakeCurNsec // *1e9
		allocatedTraffic = 48
	)
	fakeClock := clock.NewFakePassiveClock(time.Unix(fakeCurSecs, fakeCurNsec))
	baseCtx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		r.clock = fakeClock
		reconciler = r
	})
	defer cancel()

	for _, rd := range []int{240, 666, 7200} {
		rds := strconv.Itoa(rd)
		t.Run(rds, func(t *testing.T) {
			r := Route("test-ns-"+rds, "rollout-route")
			r.Annotations = map[string]string{
				serving.RolloutDurationKey: rds + "s",
			}

			tc, tls := testIngressParams(t, r, func(tc *traffic.Config) {
				tc.Targets[traffic.DefaultTarget] = traffic.RevisionTargets{{
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "thor",
						RevisionName:      "thursday",
						Percent:           ptr.Int64(52),
						LatestRevision:    ptr.Bool(true),
					},
					Protocol: networking.ProtocolHTTP1,
				}, {
					TrafficTarget: v1.TrafficTarget{
						ConfigurationName: "odin",
						RevisionName:      "wednesday",
						Percent:           ptr.Int64(allocatedTraffic),
						LatestRevision:    ptr.Bool(true),
					},
					Protocol: networking.ProtocolHTTP1,
				}}
			})

			fakeClock.SetTime(time.Unix(fakeCurSecs, fakeCurNsec))
			wasReenqueued := false
			duration := 0 * time.Second
			reconciler.enqueueAfter = func(_ interface{}, d time.Duration) {
				wasReenqueued = true
				duration = d
			}
			ctx := updateContext(baseCtx, 311 /*rolloutDuration*/) // This should be ignored, since we set annotation.
			_, ro, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress-class")
			if err != nil {
				t.Fatal("Unexpected error:", err)
			}

			if got, want := ro, tc.BuildRollout(); !cmp.Equal(got, want) {
				t.Errorf("Complex initial rollout mismatch: diff(-want,+got):\n%s", cmp.Diff(want, got))
			}

			ing := getRouteIngressFromClient(ctx, t, r)
			fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ing)

			// Now we have initial version. Let's make a rollout.
			tc.Targets[traffic.DefaultTarget][1].RevisionName = "miercoles"

			_, updRO, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress-class")
			if err != nil {
				t.Error("Unexpected error:", err)
			}

			// This should update the rollout with the new annotation.
			updated := getRouteIngressFromClient(ctx, t, r)

			if got, want := updated.Annotations[networking.RolloutAnnotationKey],
				ing.Annotations[networking.RolloutAnnotationKey]; got == want {
				t.Fatal("Expected difference in the rollout annotation, but found none")
			}
			if wasReenqueued {
				t.Fatal("Unexpected re-enqueuing happened")
			}

			ro = deserializeRollout(ctx, updated.Annotations[networking.RolloutAnnotationKey])
			if ro == nil {
				t.Fatal("Rollout was bogus and impossible to deserialize")
			}

			// Verify the same rollout was returned by the ReconcileResources.
			if !cmp.Equal(ro, updRO) {
				t.Errorf("Returned and Annotation Rollouts differ: diff(-ret,+ann):\n%s", cmp.Diff(updRO, ro))
			}

			// After sorting `odin` will come first.
			if got, want := ro.Configurations[0].StepParams.StartTime, int64(fakeCurTime); got != want {
				t.Errorf("Rollout StartTime = %d, want: %d", got, want)
			}

			// OK. So now let's observe ready to start the rollout. For that we need to make ingress ready.
			cp := updated.DeepCopy()
			cp.Status.MarkLoadBalancerReady(nil, nil)
			cp.Status.MarkNetworkConfigured()
			fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(cp)
			// For comparisons.
			ing = updated

			// Advance the time by 5s.
			const drift = 5
			fakeClock.SetTime(fakeClock.Now().Add(drift * time.Second))

			// Drift seconds of the duration already spent.
			totalDuration := float64(rd - drift)
			// Step count is minimum of % points of traffic allocated to the config (1% already shifted).
			steps := math.Min(totalDuration/drift, allocatedTraffic-1)
			stepSize := math.Max(1, math.Round((allocatedTraffic-1)/steps)) // we round the step size.
			stepDuration := time.Duration(int(totalDuration * float64(time.Second) / steps))

			_, updRO, err = reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress")
			if err != nil {
				t.Error("Unexpected error:", err)
			}

			// This should update the rollout with the new annotation with steps and stuff.
			updated = getRouteIngressFromClient(ctx, t, r)

			if got, want := updated.Annotations[networking.RolloutAnnotationKey],
				ing.Annotations[networking.RolloutAnnotationKey]; got == want {
				t.Fatal("Expected difference in the rollout annotation, but found none")
			}
			ro = deserializeRollout(ctx, updated.Annotations[networking.RolloutAnnotationKey])
			if ro == nil {
				t.Fatal("Rollout was bogus and impossible to deserialize")
			}

			// Verify NextStepTime, StepDuration and StepSize are set as expected.
			// There is some rounding possible for various durations, so leave a 5ns leeway.
			if diff := math.Abs(float64(ro.Configurations[0].StepParams.NextStepTime - fakeClock.Now().Add(stepDuration).UnixNano())); diff > 5 {
				t.Errorf("NextStepTime = %d, want: %d±5ns; diff = %vns",
					ro.Configurations[0].StepParams.NextStepTime, fakeClock.Now().Add(stepDuration).UnixNano(), diff)
			}
			if got, want := ro.Configurations[0].StepParams.StepDuration, int64(stepDuration); got != want {
				t.Errorf("StepDuration = %d, want: %d", got, want)
			}

			// Verify the same rollout was returned by the ReconcileResources.
			if !cmp.Equal(ro, updRO) {
				t.Errorf("Returned and Annotation Rollouts after re-enqueue differ: diff(-ret,+ann):\n%s",
					cmp.Diff(updRO, ro))
			}

			if got, want := ro.Configurations[0].StepParams.StepSize, int(stepSize); got != want {
				t.Errorf("StepSize = %d, want: %d", got, want)
			}
			if !wasReenqueued {
				t.Fatal("Re-enqueuing didn't happen")
			}
			if got, want, diff := duration, stepDuration, math.Abs(float64(duration-stepDuration)); diff > 5 {
				t.Errorf("Re-enqueue duration = %v, want: %v±5ns; diff = %vns", got, want, diff)
			}
		})
	}
}

func TestReconcileIngressUpdateNoRollout(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()
	ctx = updateContext(ctx, 0)

	r := Route("test-ns", "test-route")

	tc, tls := testIngressParams(t, r)
	if _, _, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	initial := getRouteIngressFromClient(ctx, t, r)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(initial)

	tc, tls = testIngressParams(t, r, func(tc *traffic.Config) {
		tc.Targets[traffic.DefaultTarget][0].RevisionName = "revision2"
	})
	if _, _, err := reconciler.reconcileIngress(ctx, r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	if diff := cmp.Diff(initial, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
	initialdRO := deserializeRollout(ctx, initial.Annotations[networking.RolloutAnnotationKey])
	if initialdRO == nil {
		t.Fatal("Rollout was bogus and impossible to deserialize")
	}
	updatedRO := deserializeRollout(ctx, updated.Annotations[networking.RolloutAnnotationKey])
	if updatedRO == nil {
		t.Fatal("Rollout was bogus and impossible to deserialize")
	}
	// This verifies no rollout was started.
	if got := len(updatedRO.Configurations[0].Revisions); got != 1 {
		t.Errorf("|revisions in rollout| = %d, want: 1", got)
	}
	// This verifies that the new *noop* rollout was installed.
	if was, become := initialdRO.Configurations[0].Revisions[0].RevisionName,
		updatedRO.Configurations[0].Revisions[0].RevisionName; was == become {
		t.Errorf("Revision for default rollout = %s was unchanged", was)
	}
}

func TestReconcileIngressUpdate(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()

	r := Route("test-ns", "test-route")

	tc, tls := testIngressParams(t, r)
	if _, _, err := reconciler.reconcileIngress(updateContext(ctx, 0), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	initial := getRouteIngressFromClient(ctx, t, r)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(initial)

	tc, tls = testIngressParams(t, r, func(tc *traffic.Config) {
		tc.Targets[traffic.DefaultTarget][0].TrafficTarget.Percent = ptr.Int64(50)
		tc.Targets[traffic.DefaultTarget] = append(tc.Targets[traffic.DefaultTarget], traffic.RevisionTarget{
			TrafficTarget: v1.TrafficTarget{
				Percent:      ptr.Int64(50),
				RevisionName: "revision2",
			},
		})
	})
	if _, _, err := reconciler.reconcileIngress(updateContext(ctx, 0), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	if diff := cmp.Diff(initial, updated); diff == "" {
		t.Error("Expected difference, but found none")
	}
}

func TestReconcileIngressRolloutDeserializeFail(t *testing.T) {
	// Test cases where previous rollout is invalid or missing.
	tests := []struct {
		name string
		val  string
	}{{
		name: "missing",
	}, {
		name: "invalid",
		val: func() string {
			ro := &traffic.Rollout{
				Configurations: []*traffic.ConfigurationRollout{{
					ConfigurationName: "configuration",
					Percent:           202, // <- this is not right!
				}},
			}
			d, _ := json.Marshal(ro)
			return string(d)
		}(),
	}, {
		name: "nonparseable",
		val:  `<?xml version="1.0" encoding="utf-8"?><rollout name="from-the-wrong-universe"/>`,
	}}

	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	defer cancel()
	ctx = updateContext(ctx, 0)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Route("test-ns", "test-route-"+tc.name)

			traffic, tls := testIngressParams(t, r)
			ing, err := resources.MakeIngress(ctx, r, traffic, tls, "foo-ingress")
			if err != nil {
				t.Fatal("Error creating ingress:", err)
			}
			ing.Annotations[networking.RolloutAnnotationKey] = tc.val
			ingClient := fakenetworkingclient.Get(ctx).NetworkingV1alpha1().Ingresses(ing.Namespace)
			ingClient.Create(ctx, ing, metav1.CreateOptions{})
			fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(ing)
			if _, _, err := reconciler.reconcileIngress(ctx, r, traffic, tls, "foo-ingress"); err != nil {
				t.Error("Unexpected error:", err)
			}
			ing, err = ingClient.Get(ctx, ing.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal("Could not get the ingress:", err)
			}
			// In all cases we want to ignore the previous one, since it's bogus.
			want := func() string {
				d, _ := json.Marshal(traffic.BuildRollout())
				return string(d)
			}()
			if got := ing.Annotations[networking.RolloutAnnotationKey]; got != want {
				t.Errorf("Incorrect Rollout Annotation; diff(-want,+got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestReconcileRevisionTargetDoesNotExist(t *testing.T) {
	ctx, _, _, _, cancel := newTestSetup(t)
	defer cancel()

	r := Route("test-ns", "test-route", WithRouteLabel(map[string]string{"route": "test-route"}))
	rev := newTestRevision(r.Namespace, "revision")
	fakeservingclient.Get(ctx).ServingV1().Revisions(r.Namespace).Create(ctx, rev, metav1.CreateOptions{})
	fakerevisioninformer.Get(ctx).Informer().GetIndexer().Add(rev)
}

func newTestRevision(namespace, name string) *v1.Revision {
	return &v1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.RevisionSpec{},
	}
}

func testIngressParams(t *testing.T, r *v1.Route, trafficOpts ...func(tc *traffic.Config)) (*traffic.Config,
	[]netv1alpha1.IngressTLS) {
	tc := &traffic.Config{
		Targets: map[string]traffic.RevisionTargets{
			traffic.DefaultTarget: {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "config",
					RevisionName:      "revision",
					Percent:           ptr.Int64(65),
					LatestRevision:    ptr.Bool(true),
				},
			}},
			"a-good-tag": {{
				TrafficTarget: v1.TrafficTarget{
					ConfigurationName: "config2",
					RevisionName:      "revision2",
					Percent:           ptr.Int64(100),
					LatestRevision:    ptr.Bool(true),
				},
			}},
		},
	}

	for _, opt := range trafficOpts {
		opt(tc)
	}

	tls := []netv1alpha1.IngressTLS{{
		Hosts:           []string{"test-route.test-ns.example.com"},
		SecretName:      "test-secret",
		SecretNamespace: "test-ns",
	}}
	return tc, tls
}

func TestReconcileIngressClassAnnotation(t *testing.T) {
	var reconciler *Reconciler
	ctx, _, _, _, cancel := newTestSetup(t, func(r *Reconciler) {
		reconciler = r
	})
	t.Cleanup(cancel)

	const expClass = "foo.ingress.networking.knative.dev"

	r := Route("test-ns", "test-route")
	tc, tls := testIngressParams(t, r)
	if _, _, err := reconciler.reconcileIngress(updateContext(ctx, 0), r, tc, tls, "foo-ingress"); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated := getRouteIngressFromClient(ctx, t, r)
	fakeingressinformer.Get(ctx).Informer().GetIndexer().Add(updated)

	if _, _, err := reconciler.reconcileIngress(updateContext(ctx, 0), r, tc, tls, expClass); err != nil {
		t.Error("Unexpected error:", err)
	}

	updated = getRouteIngressFromClient(ctx, t, r)
	updatedClass := updated.Annotations[networking.IngressClassAnnotationKey]
	if expClass != updatedClass {
		t.Errorf("Unexpected annotation got %q want %q", expClass, updatedClass)
	}
}

func updateContext(ctx context.Context, rolloutDurationSecs int) context.Context {
	cfg := reconcilerTestConfig(false)
	cfg.Network.RolloutDurationSecs = rolloutDurationSecs
	c := config.ToContext(ctx, cfg)
	return c
}

func getContext() context.Context {
	return updateContext(context.Background(), 0)
}
