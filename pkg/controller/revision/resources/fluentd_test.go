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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller/revision/config"
)

func TestMakeFluentdConfigMap(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		oc   *config.Observability
		want *corev1.ConfigMap
	}{{
		name: "empty config",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
		},
		oc: &config.Observability{},
		want: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-fluentd",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Data: map[string]string{
				"varlog.conf": fluentdSidecarPreOutputConfig,
			},
		},
	}, {
		name: "non-empty config",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
		},
		oc: &config.Observability{
			FluentdSidecarOutputConfig: "foo bar config",
		},
		want: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar-fluentd",
				Labels: map[string]string{
					serving.RevisionLabelKey: "bar",
					serving.RevisionUID:      "1234",
					AppLabelKey:              "bar",
				},
				Annotations: map[string]string{},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Revision",
					Name:               "bar",
					UID:                "1234",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Data: map[string]string{
				"varlog.conf": fluentdSidecarPreOutputConfig + "foo bar config",
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeFluentdConfigMap(test.rev, test.oc)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeDeployment (-want, +got) = %v", diff)
			}
		})
	}
}
