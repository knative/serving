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

	"github.com/knative/pkg/kmp"

	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	servingv1alpha1 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"

	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/knative/serving/pkg/testing"
)

const (
	testNamespace = "test-namespace"
	testName      = "test-name"
)

func TestMakeHPA(t *testing.T) {
	cases := []struct {
		name string
		pa   *v1alpha1.PodAutoscaler
		want *autoscalingv2beta1.HorizontalPodAutoscaler
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
		name: "with an actual target",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation(autoscaling.CPU)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.CPU),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withMetric(autoscalingv2beta1.MetricSpec{
				Type: autoscalingv2beta1.ResourceMetricSourceType,
				Resource: &autoscalingv2beta1.ResourceMetricSource{
					Name:                     corev1.ResourceCPU,
					TargetAverageUtilization: ptr.Int32(50),
				},
			})),
	}, {
		name: "with an actual fractional target",
		pa:   pa(WithTargetAnnotation("1982.4"), WithMetricAnnotation(autoscaling.CPU)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.CPU),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "1982.4"),
			withMetric(autoscalingv2beta1.MetricSpec{
				Type: autoscalingv2beta1.ResourceMetricSourceType,
				Resource: &autoscalingv2beta1.ResourceMetricSource{
					Name:                     corev1.ResourceCPU,
					TargetAverageUtilization: ptr.Int32(1983),
				},
			})),
	}, {
		name: "with metric=concurrency",
		pa:   pa(WithMetricAnnotation(autoscaling.Concurrency)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency),
			withMetric(autoscalingv2beta1.MetricSpec{
				Type: autoscalingv2beta1.ObjectMetricSourceType,
				Object: &autoscalingv2beta1.ObjectMetricSource{
					Target: autoscalingv2beta1.CrossVersionObjectReference{
						APIVersion: servingv1alpha1.SchemeGroupVersion.String(),
						Kind:       "revision",
						Name:       testName,
					},
					MetricName:   autoscaling.Concurrency,
					AverageValue: resource.NewQuantity(100, resource.DecimalSI),
					TargetValue:  *resource.NewQuantity(100, resource.DecimalSI),
				},
			})),
	}, {
		name: "with metric=concurrency and target=50",
		pa:   pa(WithTargetAnnotation("50"), WithMetricAnnotation(autoscaling.Concurrency)),
		want: hpa(
			withAnnotationValue(autoscaling.MetricAnnotationKey, autoscaling.Concurrency),
			withAnnotationValue(autoscaling.TargetAnnotationKey, "50"),
			withMetric(autoscalingv2beta1.MetricSpec{
				Type: autoscalingv2beta1.ObjectMetricSourceType,
				Object: &autoscalingv2beta1.ObjectMetricSource{
					Target: autoscalingv2beta1.CrossVersionObjectReference{
						APIVersion: servingv1alpha1.SchemeGroupVersion.String(),
						Kind:       "revision",
						Name:       testName,
					},
					MetricName:   autoscaling.Concurrency,
					AverageValue: resource.NewQuantity(50, resource.DecimalSI),
					TargetValue:  *resource.NewQuantity(50, resource.DecimalSI),
				},
			})),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MakeHPA(tc.pa, config)
			if equal, err := kmp.SafeEqual(tc.want, got); err != nil {
				t.Errorf("Got error comparing output, err = %v", err)
			} else if !equal {
				if diff, err := kmp.SafeDiff(tc.want, got); err != nil {
					t.Errorf("Got error diffing output, err = %v", err)
				} else {
					t.Errorf("MakeHPA() = (-want, +got):\n%v", diff)
				}
			}
		})
	}
}

func pa(options ...PodAutoscalerOption) *v1alpha1.PodAutoscaler {
	p := &v1alpha1.PodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testName,
			UID:       "2006",
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.HPA,
			},
		},
		Spec: v1alpha1.PodAutoscalerSpec{
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

func hpa(options ...hpaOption) *autoscalingv2beta1.HorizontalPodAutoscaler {
	h := &autoscalingv2beta1.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testName,
			Namespace: testNamespace,
			Annotations: map[string]string{
				autoscaling.ClassAnnotationKey: autoscaling.HPA,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         v1alpha1.SchemeGroupVersion.String(),
				Kind:               "PodAutoscaler",
				Name:               testName,
				UID:                "2006",
				Controller:         ptr.Bool(true),
				BlockOwnerDeletion: ptr.Bool(true),
			}},
		},
		Spec: autoscalingv2beta1.HorizontalPodAutoscalerSpec{
			MaxReplicas: math.MaxInt32,
			ScaleTargetRef: autoscalingv2beta1.CrossVersionObjectReference{
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

type hpaOption func(*autoscalingv2beta1.HorizontalPodAutoscaler)

func withAnnotationValue(key, value string) hpaOption {
	return func(pa *autoscalingv2beta1.HorizontalPodAutoscaler) {
		if pa.Annotations == nil {
			pa.Annotations = make(map[string]string)
		}
		pa.Annotations[key] = value
	}
}

func withMinReplicas(i int) hpaOption {
	return func(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
		hpa.Spec.MinReplicas = ptr.Int32(int32(i))
	}
}

func withMaxReplicas(i int) hpaOption {
	return func(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
		hpa.Spec.MaxReplicas = int32(i)
	}
}

func withMetric(m autoscalingv2beta1.MetricSpec) hpaOption {
	return func(hpa *autoscalingv2beta1.HorizontalPodAutoscaler) {
		hpa.Spec.Metrics = []autoscalingv2beta1.MetricSpec{m}
	}
}

var config = &autoscaler.Config{
	EnableScaleToZero:                  true,
	ContainerConcurrencyTargetFraction: 1.0,
	ContainerConcurrencyTargetDefault:  100.0,
	MaxScaleUpRate:                     10.0,
	StableWindow:                       60 * time.Second,
	PanicThresholdPercentage:           200,
	PanicWindow:                        6 * time.Second,
	PanicWindowPercentage:              10,
	TickInterval:                       2 * time.Second,
	ScaleToZeroGracePeriod:             30 * time.Second,
}
