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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	net "github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
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
		},
		want: nil,
	}, {
		name: "has missing scaleTargetRef",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: 1,
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
		},
		want: apis.ErrOutOfBoundsValue(-1, 0,
			v1beta1.RevisionContainerConcurrencyMax, "containerConcurrency"),
	}, {
		name: "multi invalid, bad concurrency and missing ref kind",
		rs: &PodAutoscalerSpec{
			ContainerConcurrency: -2,
			ScaleTargetRef: corev1.ObjectReference{
				APIVersion: "apps/v1",
				Name:       "bar",
			},
		},
		want: apis.ErrOutOfBoundsValue(-2, 0,
			v1beta1.RevisionContainerConcurrencyMax, "containerConcurrency").Also(
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
			},
		},
		want: (&apis.FieldError{
			Message: fmt.Sprintf("Invalid %s annotation value: must be an integer equal or greater than 0", autoscaling.MinScaleAnnotationKey),
			Paths:   []string{autoscaling.MinScaleAnnotationKey},
		}).ViaField("annotations").ViaField("metadata"),
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
			},
		},
		want: apis.ErrOutOfBoundsValue(-1, 0,
			v1beta1.RevisionContainerConcurrencyMax, "spec.containerConcurrency"),
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.r.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
