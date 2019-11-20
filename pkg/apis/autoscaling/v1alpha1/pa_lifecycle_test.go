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
package v1alpha1

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	apitestv1 "knative.dev/pkg/apis/testing/v1"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
)

func TestPodAutoscalerDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&PodAutoscaler{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(PodAutoscaler, %T) = %v", test.t, err)
			}
		})
	}
}

func TestGeneration(t *testing.T) {
	r := PodAutoscaler{}
	if a := r.GetGeneration(); a != 0 {
		t.Errorf("empty pa generation should be 0 was: %d", a)
	}

	r.SetGeneration(5)
	if e, a := int64(5), r.GetGeneration(); e != a {
		t.Errorf("getgeneration mismatch expected: %d got: %d", e, a)
	}

}

func TestCanScaleToZero(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		status PodAutoscalerStatus
		result bool
		grace  time.Duration
	}{{
		name:   "empty status",
		status: PodAutoscalerStatus{},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "active condition",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (no LTT)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionFalse,
					// No LTT = beginning of time, so for sure we can.
				}},
			},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (LTT longer than grace period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionFalse,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-30 * time.Second)),
					},
					// LTT = 30 seconds ago.
				}},
			},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "inactive condition (LTT less than grace period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionFalse,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-10 * time.Second)),
					},
					// LTT = 10 seconds ago.
				}},
			},
		},
		result: false,
		grace:  30 * time.Second,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.result, tc.status.CanScaleToZero(now, tc.grace); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

func TestActiveFor(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		status PodAutoscalerStatus
		result time.Duration
	}{{
		name:   "empty status",
		status: PodAutoscalerStatus{},
		result: -1,
	}, {
		name: "unknown condition",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		result: -1,
	}, {
		name: "inactive condition",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		result: -1,
	}, {
		name: "active condition (no LTT)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
					// No LTT = beginning of time, so for sure we can.
				}},
			},
		},
		result: time.Since(time.Time{}),
	}, {
		name: "active condition (LTT longer than idle period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-30 * time.Second)),
					},
					// LTT = 30 seconds ago.
				}},
			},
		},
		result: 30 * time.Second,
	}, {
		name: "active condition (LTT less than idle period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-10 * time.Second)),
					},
					// LTT = 10 seconds ago.
				}},
			},
		},
		result: 10 * time.Second,
	}}

	for _, tc := range cases {
		if got, want := tc.status.ActiveFor(now), tc.result; absDiff(got, want) > 10*time.Millisecond {
			t.Errorf("ActiveFor = %v, want: %v", got, want)
		}
	}
}

func absDiff(a, b time.Duration) time.Duration {
	a -= b
	if a < 0 {
		a *= -1
	}
	return a
}

func TestCanFailActivation(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		status PodAutoscalerStatus
		result bool
		grace  time.Duration
	}{{
		name:   "empty status",
		status: PodAutoscalerStatus{},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "active condition",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		result: false,
		grace:  10 * time.Second,
	}, {
		name: "activating condition (no LTT)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionUnknown,
					// No LTT = beginning of time, so for sure we can.
				}},
			},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "activating condition (LTT longer than grace period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionUnknown,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-30 * time.Second)),
					},
					// LTT = 30 seconds ago.
				}},
			},
		},
		result: true,
		grace:  10 * time.Second,
	}, {
		name: "activating condition (LTT less than grace period ago)",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionUnknown,
					LastTransitionTime: apis.VolatileTime{
						Inner: metav1.NewTime(now.Add(-10 * time.Second)),
					},
					// LTT = 10 seconds ago.
				}},
			},
		},
		result: false,
		grace:  30 * time.Second,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if e, a := tc.result, tc.status.CanFailActivation(now, tc.grace); e != a {
				t.Errorf("%q expected: %v got: %v", tc.name, e, a)
			}
		})
	}
}

func TestIsActivating(t *testing.T) {
	cases := []struct {
		name         string
		status       PodAutoscalerStatus
		isActivating bool
	}{{
		name:         "empty status",
		status:       PodAutoscalerStatus{},
		isActivating: false,
	}, {
		name: "active=unknown",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isActivating: true,
	}, {
		name: "active=true",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isActivating: false,
	}, {
		name: "active=false",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isActivating: false,
	}}

	for _, tc := range cases {
		if e, a := tc.isActivating, tc.status.IsActivating(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestIsReady(t *testing.T) {
	cases := []struct {
		name    string
		status  PodAutoscalerStatus
		isReady bool
	}{{
		name:    "empty status should not be ready",
		status:  PodAutoscalerStatus{},
		isReady: false,
	}, {
		name: "Different condition type should not be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: false,
	}, {
		name: "False condition status should not be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Unknown condition status should not be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionReady,
					Status: corev1.ConditionUnknown,
				}},
			},
		},
		isReady: false,
	}, {
		name: "Missing condition status should not be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type: PodAutoscalerConditionReady,
				}},
			},
		},
		isReady: false,
	}, {
		name: "True condition status should be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status should be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}, {
					Type:   PodAutoscalerConditionReady,
					Status: corev1.ConditionTrue,
				}},
			},
		},
		isReady: true,
	}, {
		name: "Multiple conditions with ready status false should not be ready",
		status: PodAutoscalerStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   PodAutoscalerConditionActive,
					Status: corev1.ConditionTrue,
				}, {
					Type:   PodAutoscalerConditionReady,
					Status: corev1.ConditionFalse,
				}},
			},
		},
		isReady: false,
	}}

	for _, tc := range cases {
		if e, a := tc.isReady, tc.status.IsReady(); e != a {
			t.Errorf("%q expected: %v got: %v", tc.name, e, a)
		}
	}
}

func TestTargetAnnotation(t *testing.T) {
	cases := []struct {
		name       string
		pa         *PodAutoscaler
		wantTarget float64
		wantOK     bool
	}{{
		name:       "not present",
		pa:         pa(map[string]string{}),
		wantTarget: 0,
		wantOK:     false,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.TargetAnnotationKey: "1",
		}),
		wantTarget: 1,
		wantOK:     true,
	}, {
		name: "present float",
		pa: pa(map[string]string{
			autoscaling.TargetAnnotationKey: "19.82",
		}),
		wantTarget: 19.82,
		wantOK:     true,
	}, {
		name: "invalid format",
		pa: pa(map[string]string{
			autoscaling.TargetAnnotationKey: "sandwich",
		}),
		wantTarget: 0,
		wantOK:     false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTarget, gotOK := tc.pa.Target()
			if gotTarget != tc.wantTarget {
				t.Errorf("got target: %v wanted: %v", gotTarget, tc.wantTarget)
			}
			if gotOK != tc.wantOK {
				t.Errorf("got ok: %v wanted %v", gotOK, tc.wantOK)
			}
		})
	}
}

func TestScaleBounds(t *testing.T) {
	cases := []struct {
		name         string
		min          string
		max          string
		reachability ReachabilityType
		wantMin      int32
		wantMax      int32
	}{{
		name:    "present",
		min:     "1",
		max:     "100",
		wantMin: 1,
		wantMax: 100,
	}, {
		name:    "absent",
		wantMin: 0,
		wantMax: 0,
	}, {
		name:    "only min",
		min:     "1",
		wantMin: 1,
		wantMax: 0,
	}, {
		name:    "only max",
		max:     "1",
		wantMin: 0,
		wantMax: 1,
	}, {
		name:         "reachable",
		min:          "1",
		max:          "100",
		reachability: ReachabilityReachable,
		wantMin:      1,
		wantMax:      100,
	}, {
		name:         "unreachable",
		min:          "1",
		max:          "100",
		reachability: ReachabilityUnreachable,
		wantMin:      0,
		wantMax:      100,
	}, {
		name:    "malformed",
		min:     "ham",
		max:     "sandwich",
		wantMin: 0,
		wantMax: 0,
	}, {
		name:    "too small",
		min:     "-1",
		max:     "-1",
		wantMin: 0,
		wantMax: 0,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pa := pa(map[string]string{})
			if tc.min != "" {
				pa.Annotations[autoscaling.MinScaleAnnotationKey] = tc.min
			}
			if tc.max != "" {
				pa.Annotations[autoscaling.MaxScaleAnnotationKey] = tc.max
			}
			pa.Spec.Reachability = tc.reachability

			min, max := pa.ScaleBounds()

			if min != tc.wantMin {
				t.Errorf("got min: %v wanted: %v", min, tc.wantMin)
			}
			if max != tc.wantMax {
				t.Errorf("got max: %v wanted: %v", max, tc.wantMax)
			}
		})
	}
}

func TestMarkResourceNotOwned(t *testing.T) {
	pa := pa(map[string]string{})
	pa.Status.MarkResourceNotOwned("doesn't", "matter")
	active := pa.Status.GetCondition("Active")
	if active.Status != corev1.ConditionFalse {
		t.Errorf("TestMarkResourceNotOwned expected active.Status: False got: %v", active.Status)
	}
	if active.Reason != "NotOwned" {
		t.Errorf("TestMarkResourceNotOwned expected active.Reason: NotOwned got: %v", active.Reason)
	}
}

func TestMarkResourceFailedCreation(t *testing.T) {
	pa := &PodAutoscalerStatus{}
	pa.MarkResourceFailedCreation("doesn't", "matter")
	apitestv1.CheckConditionFailed(pa.duck(), PodAutoscalerConditionActive, t)

	active := pa.GetCondition("Active")
	if active.Status != corev1.ConditionFalse {
		t.Errorf("TestMarkResourceFailedCreation expected active.Status: False got: %v", active.Status)
	}
	if active.Reason != "FailedCreate" {
		t.Errorf("TestMarkResourceFailedCreation expected active.Reason: FailedCreate got: %v", active.Reason)
	}
}

func TestClass(t *testing.T) {
	cases := []struct {
		name string
		pa   *PodAutoscaler
		want string
	}{{
		name: "kpa class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: autoscaling.KPA,
		}),
		want: autoscaling.KPA,
	}, {
		name: "hpa class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: autoscaling.HPA,
		}),
		want: autoscaling.HPA,
	}, {
		name: "default class",
		pa:   pa(map[string]string{}),
		want: autoscaling.KPA,
	}, {
		name: "custom class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: "yolo.sandwich.com",
		}),
		want: "yolo.sandwich.com",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pa.Class()
			if got != tc.want {
				t.Errorf("got class: %q wanted: %q", got, tc.want)
			}
		})
	}
}

func TestMetric(t *testing.T) {
	cases := []struct {
		name string
		pa   *PodAutoscaler
		want string
	}{{
		name: "default class, annotation set",
		pa: pa(map[string]string{
			autoscaling.MetricAnnotationKey: autoscaling.Concurrency,
		}),
		want: autoscaling.Concurrency,
	}, {
		name: "hpa class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: autoscaling.HPA,
		}),
		want: autoscaling.CPU,
	}, {
		name: "kpa class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: autoscaling.KPA,
		}),
		want: autoscaling.Concurrency,
	}, {
		name: "custom class",
		pa: pa(map[string]string{
			autoscaling.ClassAnnotationKey: "yolo.sandwich.com",
		}),
		want: "",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pa.Metric()
			if got != tc.want {
				t.Errorf("Metric() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWindowAnnotation(t *testing.T) {
	cases := []struct {
		name       string
		pa         *PodAutoscaler
		wantWindow time.Duration
		wantOK     bool
	}{{
		name:       "not present",
		pa:         pa(map[string]string{}),
		wantWindow: 0,
		wantOK:     false,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.WindowAnnotationKey: "120s",
		}),
		wantWindow: time.Second * 120,
		wantOK:     true,
	}, {
		name: "invalid",
		pa: pa(map[string]string{
			autoscaling.WindowAnnotationKey: "365d",
		}),
		wantWindow: 0,
		wantOK:     false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotWindow, gotOK := tc.pa.Window()
			if gotWindow != tc.wantWindow {
				t.Errorf("%q expected target: %v got: %v", tc.name, tc.wantWindow, gotWindow)
			}
			if gotOK != tc.wantOK {
				t.Errorf("%q expected ok: %v got %v", tc.name, tc.wantOK, gotOK)
			}
		})
	}
}

func TestPanicWindowPercentageAnnotation(t *testing.T) {
	cases := []struct {
		name           string
		pa             *PodAutoscaler
		wantPercentage float64
		wantOK         bool
	}{{
		name:           "not present",
		pa:             pa(map[string]string{}),
		wantPercentage: 0.0,
		wantOK:         false,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.PanicWindowPercentageAnnotationKey: "10.0",
		}),
		wantPercentage: 10.0,
		wantOK:         true,
	}, {
		name: "malformed",
		pa: pa(map[string]string{
			autoscaling.PanicWindowPercentageAnnotationKey: "sandwich",
		}),
		wantPercentage: 0.0,
		wantOK:         false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercentage, gotOK := tc.pa.PanicWindowPercentage()
			if gotPercentage != tc.wantPercentage {
				t.Errorf("%q expected target: %v got: %v", tc.name, tc.wantPercentage, gotPercentage)
			}
			if gotOK != tc.wantOK {
				t.Errorf("%q expected ok: %v got %v", tc.name, tc.wantOK, gotOK)
			}
		})
	}
}

func TestTargetUtilization(t *testing.T) {
	cases := []struct {
		name   string
		pa     *PodAutoscaler
		want   float64
		wantOK bool
	}{{
		name:   "not present",
		pa:     pa(map[string]string{}),
		want:   0.0,
		wantOK: false,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.TargetUtilizationPercentageKey: "10.0",
		}),
		want:   .1,
		wantOK: true,
	}, {
		name: "malformed",
		pa: pa(map[string]string{
			autoscaling.TargetUtilizationPercentageKey: "NPH",
		}),
		want:   0.0,
		wantOK: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotOK := tc.pa.TargetUtilization()
			if got, want := got, tc.want; got != want {
				t.Errorf("%q target utilization: %v want: %v", tc.name, got, want)
			}
			if gotOK != tc.wantOK {
				t.Errorf("%q expected ok: %v got %v", tc.name, tc.wantOK, gotOK)
			}
		})
	}
}

func TestPanicThresholdPercentage(t *testing.T) {
	cases := []struct {
		name           string
		pa             *PodAutoscaler
		wantPercentage float64
		wantOK         bool
	}{{
		name:           "not present",
		pa:             pa(map[string]string{}),
		wantPercentage: 0.0,
		wantOK:         false,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.PanicThresholdPercentageAnnotationKey: "300.0",
		}),
		wantPercentage: 300.0,
		wantOK:         true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPercentage, gotOK := tc.pa.PanicThresholdPercentage()
			if gotPercentage != tc.wantPercentage {
				t.Errorf("%q expected target: %v got: %v", tc.name, tc.wantPercentage, gotPercentage)
			}
			if gotOK != tc.wantOK {
				t.Errorf("%q expected ok: %v got %v", tc.name, tc.wantOK, gotOK)
			}
		})
	}
}

func pa(annotations map[string]string) *PodAutoscaler {
	p := &PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "test-namespace",
			Name:        "test-name",
			Annotations: annotations,
		},
		Spec: PodAutoscalerSpec{
			ContainerConcurrency: 0,
		},
		Status: PodAutoscalerStatus{},
	}
	return p
}

func TestTypicalFlow(t *testing.T) {
	r := &PodAutoscalerStatus{}
	r.InitializeConditions()
	apitestv1.CheckConditionOngoing(r.duck(), PodAutoscalerConditionActive, t)
	apitestv1.CheckConditionOngoing(r.duck(), PodAutoscalerConditionReady, t)

	// When we see traffic, mark ourselves active.
	r.MarkActive()
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionReady, t)

	// Check idempotency.
	r.MarkActive()
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionReady, t)

	// When we stop seeing traffic, mark outselves inactive.
	r.MarkInactive("TheReason", "the message")
	apitestv1.CheckConditionFailed(r.duck(), PodAutoscalerConditionActive, t)
	if !r.IsInactive() {
		t.Errorf("IsInactive was not set.")
	}
	apitestv1.CheckConditionFailed(r.duck(), PodAutoscalerConditionReady, t)

	// When traffic hits the activator and we scale up the deployment we mark
	// ourselves as activating.
	r.MarkActivating("Activating", "Red team, GO!")
	apitestv1.CheckConditionOngoing(r.duck(), PodAutoscalerConditionActive, t)
	apitestv1.CheckConditionOngoing(r.duck(), PodAutoscalerConditionReady, t)

	// When the activator successfully forwards traffic to the deployment,
	// we mark ourselves as active once more.
	r.MarkActive()
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionActive, t)
	apitestv1.CheckConditionSucceeded(r.duck(), PodAutoscalerConditionReady, t)
}

func TestTargetBC(t *testing.T) {
	cases := []struct {
		name   string
		pa     *PodAutoscaler
		want   float64
		wantOK bool
	}{{
		name: "not present",
		pa:   pa(map[string]string{}),
		want: 0.0,
	}, {
		name: "present",
		pa: pa(map[string]string{
			autoscaling.TargetBurstCapacityKey: "101.0",
		}),
		want:   101,
		wantOK: true,
	}, {
		name: "present 0",
		pa: pa(map[string]string{
			autoscaling.TargetBurstCapacityKey: "0",
		}),
		want:   0,
		wantOK: true,
	}, {
		name: "present -1",
		pa: pa(map[string]string{
			autoscaling.TargetBurstCapacityKey: "-1",
		}),
		want:   -1,
		wantOK: true,
	}, {
		name: "malformed",
		pa: pa(map[string]string{
			autoscaling.TargetBurstCapacityKey: "NPH",
		}),
		want: 0.0,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotOK := tc.pa.TargetBC()
			if got, want := got, tc.want; got != want {
				t.Errorf("%q target burst capacity: %v want: %v", tc.name, got, want)
			}
			if gotOK != tc.wantOK {
				t.Errorf("%q expected ok: %v got %v", tc.name, tc.wantOK, gotOK)
			}
		})
	}
}

func TestScaleStatus(t *testing.T) {
	pas := &PodAutoscalerStatus{}
	if got, want := pas.GetDesiredScale(), int32(-1); got != want {
		t.Errorf("GetDesiredScale = %d, want: %v", got, want)
	}
	pas.DesiredScale = ptr.Int32(19980709)
	if got, want := pas.GetDesiredScale(), int32(19980709); got != want {
		t.Errorf("GetDesiredScale = %d, want: %v", got, want)
	}

	if got, want := pas.GetActualScale(), int32(-1); got != want {
		t.Errorf("GetActualScale = %d, want: %v", got, want)
	}
	pas.ActualScale = ptr.Int32(20060907)
	if got, want := pas.GetActualScale(), int32(20060907); got != want {
		t.Errorf("GetActualScale = %d, want: %v", got, want)
	}
}

func TestPodAutoscalerGetGroupVersionKind(t *testing.T) {
	p := &PodAutoscaler{}
	want := schema.GroupVersionKind{
		Group:   autoscaling.InternalGroupName,
		Version: "v1alpha1",
		Kind:    "PodAutoscaler",
	}
	if got := p.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
