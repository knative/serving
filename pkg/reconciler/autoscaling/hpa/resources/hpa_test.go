/*
Copyright 2019 The Knative Authors

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

package resources

import (
	"math"
	"testing"
	"time"

	"knative.dev/pkg/kmp"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/autoscaling"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "knative.dev/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testName      = "test-name"
)

func TestMakeHPA(t *testing.T) {
	cases := []struct {
		name string
		pa   *autoscalingv1alpha1.PodAutoscaler
		want *autoscalingv2.HorizontalPodAutoscaler
	}{{
		name: "defaults",
		pa:   pa(),
		want: hpa(),
	}, {
		name: "with lower bound",
		pa:   pa(WithLowerScaleBound(5)),
		want: hpa(withMinReplicas(5), withAnnotationValue(autoscaling.MinScaleAnnotationKey, "5")),
	}, {
		name: "with upper bound",
		pa:   pa(WithUpperScaleBound(5)),
		want: hpa(withMaxReplicas(5), withAnnotationValue(autoscaling.MaxScaleAnnotationKey, "5")),
	}, {
		name: "with an actual cpu target",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation(autoscaling.CPU)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.CPU),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withMetric(autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr.Int32(50),
					},
				},
			})),
	}, {
		name: "with an actual memory target",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation(autoscaling.Memory)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Memory),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withMetric(autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceMemory,
					Target: autoscalingv2.MetricTarget{
						Type:         autoscalingv2.AverageValueMetricType,
						AverageValue: resource.NewQuantity(50*1024*1024, resource.BinarySI),
					},
				},
			})),
	}, {
		name: "with an actual fractional target",
		pa:   pa(WithTargetAnnotation("1982.4"), WithMetricAnnotation(autoscaling.CPU)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.CPU),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "1982.4"),
			withMetric(autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr.Int32(1983),
					},
				},
			})),
	}, {
		name: "with window annotation",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation(autoscaling.CPU), WithWindowAnnotation("60s")),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.CPU),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withAnnotationValue(autoscaling.WindowAnnotationKey, "60s"),
			withMetric(autoscalingv2.MetricSpec{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricSource{
					Name: corev1.ResourceCPU,
					Target: autoscalingv2.MetricTarget{
						Type:               autoscalingv2.UtilizationMetricType,
						AverageUtilization: ptr.Int32(50),
					},
				},
			}),
			withBehavior(&autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: ptr.Int32(60),
				},
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: ptr.Int32(60),
				},
			}),
		),
	}, {
		name: "with custom metric",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation("customMetric")),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, "customMetric"),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withMetric(autoscalingv2.MetricSpec{
				Type: autoscalingv2.PodsMetricSourceType,
				Pods: &autoscalingv2.PodsMetricSource{
					Metric: autoscalingv2.MetricIdentifier{
						Name: "customMetric",
					},
					Target: autoscalingv2.MetricTarget{
						Type:         autoscalingv2.AverageValueMetricType,
						AverageValue: resource.NewQuantity(50, resource.DecimalSI),
					},
				},
			})),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MakeHPA(tc.pa, config)
			if equal, err := kmp.SafeEqual(tc.want, got); err != nil {
				t.Error("Got error comparing output, err =", err)
			} else if !equal {
				if diff, err := kmp.SafeDiff(tc.want, got); err != nil {
					t.Error("Got error diffing output, err =", err)
				} else {
					t.Errorf("MakeHPA() = (-want, +got):\n%v", diff)
				}
			}
		})
	}
}

func pa(options ...PodAutoscalerOption) *autoscalingv1alpha1.PodAutoscaler {
	p := &autoscalingv1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			UID:       "2006",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.HPA,
			},
		},
		Spec: autoscalingv1alpha1.PodAutoscalerSpec{
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps",
				Kind:       "Deployment",
				Name:       "some-name",
			},
		},
	}
	for _, fn := range options {
		fn(p)
	}
	return p
}

func hpa(options ...hpaOption) *autoscalingv2.HorizontalPodAutoscaler {
	h := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.HPA,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         autoscalingv1alpha1.SchemeGroupVersion.String(),
				Kind:               "PodAutoscaler",
				Name:               testName,
				UID:                "2006",
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: math.MaxInt32,
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps",
				Kind:       "Deployment",
				Name:       "some-name",
			},
		},
	}

	for _, o := range options {
		o(h)
	}
	return h
}

type hpaOption func(*autoscalingv2.HorizontalPodAutoscaler)

func withAnnotationValue(key, value string) hpaOption {
	return func(pa *autoscalingv2.HorizontalPodAutoscaler) {
		if pa.Annotations == nil {
			pa.Annotations = make(map[string]string, 1)
		}
		pa.Annotations[key] = value
	}
}

func withMinReplicas(i int) hpaOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.MinReplicas = ptr.Int32(int32(i))
	}
}

func withMaxReplicas(i int) hpaOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.MaxReplicas = int32(i)
	}
}

func withMetric(m autoscalingv2.MetricSpec) hpaOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = []autoscalingv2.MetricSpec{m}
	}
}

func withBehavior(m *autoscalingv2.HorizontalPodAutoscalerBehavior) hpaOption {
	return func(hpa *autoscalingv2.HorizontalPodAutoscaler) {
		hpa.Spec.Behavior = m
	}
}

var config = &autoscalerconfig.Config{
	EnableScaleToZero:                  true,
	ContainerConcurrencyTargetFraction: 1.0,
	ContainerConcurrencyTargetDefault:  100.0,
	RPSTargetDefault:                   200.0,
	TargetUtilization:                  1.0,
	MaxScaleUpRate:                     10.0,
	StableWindow:                       60 * time.Second,
	PanicThresholdPercentage:           200,
	PanicWindowPercentage:              10,
	ScaleToZeroGracePeriod:             30 * time.Second,
}
