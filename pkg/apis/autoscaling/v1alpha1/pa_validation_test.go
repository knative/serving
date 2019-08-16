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
	"context"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/config"
	net "knative.dev/serving/pkg/apis/networking"
)

func TestPodAutoscalerSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		rs   *PodAutoscalerSpec
		want *apis.FieldError
	}{{
		name: "valid",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: nil,
	}, {
		name: "protocol type missing",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
		},
		want: apis.ErrMissingField("protocolType"),
	}, {
		name: "protcol type invalid",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
			ProtocolType: net.ProtocolType("dragon"),
		},
		want: apis.ErrInvalidValue("dragon", "protocolType"),
	}, {
		name: "has missing scaleTargetRef",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 1,
			ProtocolType:         net.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("scaleTargetRef.apiVersion", "scaleTargetRef.kind",
			"scaleTargetRef.name"),
	}, {
		name: "has missing scaleTargetRef kind",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 1,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("scaleTargetRef.kind"),
	}, {
		name: "has missing scaleTargetRef apiVersion",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: corev1.ObjectReference{
				Kind: "Deployment",
				Name: "bar",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("scaleTargetRef.apiVersion"),
	}, {
		name: "has missing scaleTargetRef name",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 0,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("scaleTargetRef.name"),
	}, {
		name: "bad container concurrency",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: -1,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "bar",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: apis.ErrOutOfBoundsValue(-1, 0,
			config.DefaultMaxRevisionContainerConcurrency, "containerConcurrency"),
	}, {
		name: "multi invalid, bad concurrency and missing ref kind",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: -2,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
			ProtocolType: net.ProtocolHTTP1,
		},
		want: apis.ErrOutOfBoundsValue(-2, 0,
			config.DefaultMaxRevisionContainerConcurrency, "containerConcurrency").Also(
			apis.ErrMissingField("scaleTargetRef.kind")),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rs.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %s", diff)
			}
		})
	}
}

func TestPodAutoscalerValidation(t *testing.T) {
	tests := []struct {
		name string
		r    *PodAutoscaler
		want *apis.FieldError
	}{{
		name: "valid",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					"minScale": "2",
				},
			},
			Spec: PodAutoscalerSpec{
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
				ProtocolType: net.ProtocolHTTP1,
			},
		},
		want: nil,
	}, {
		name: "valid, optional fields",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					"minScale": "2",
				},
			},
			Spec: PodAutoscalerSpec{
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
				ProtocolType: net.ProtocolH2C,
			},
		},
		want: nil,
	}, {
		name: "bad protocol",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					"minScale": "2",
				},
			},
			Spec: PodAutoscalerSpec{
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
				ProtocolType: net.ProtocolType("WebSocket"),
			},
		},
		want: apis.ErrInvalidValue("WebSocket", "spec.protocolType"),
	}, {
		name: "bad scale bounds",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
				Annotations: map[string]string{
					autoscaling.MinScaleAnnotationKey: "FOO",
				},
			},
			Spec: PodAutoscalerSpec{
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
				ProtocolType: net.ProtocolHTTP1,
			},
		},
		want: apis.ErrOutOfBoundsValue("FOO", 1, math.MaxInt32, autoscaling.MinScaleAnnotationKey).ViaField("metadata", "annotations"),
	}, {
		name: "empty spec",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
			},
		},
		want: apis.ErrMissingField("spec"),
	}, {
		name: "nested spec error",
		r: &PodAutoscaler{
			ObjectMeta: v1.ObjectMeta{
				Name: "valid",
			},
			Spec: PodAutoscalerSpec{
				ContainerConcurrency: -1,
				ScaleTargetRef: corev1.ObjectReference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "bar",
				},
				ProtocolType: net.ProtocolHTTP1,
			},
		},
		want: apis.ErrOutOfBoundsValue(-1, 0,
			config.DefaultMaxRevisionContainerConcurrency, "spec.containerConcurrency"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if got, want := got.Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Got: %q, want: %q, diff: %s", got, want, cmp.Diff(got, want))
			}
		})
	}
}
