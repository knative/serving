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

package v1alpha1

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
)

func TestMetricValidation(t *testing.T) {
	tests := []struct {
		name string
		m    *Metric
		want *apis.FieldError
	}{{
		name: "invalid BYO name",
		m: &Metric{
			ObjectMeta: metav1.ObjectMeta{
				Name: "@-invalid",
			},
			Spec: MetricSpec{
				ScrapeTarget: "hello-12ft",
			},
		},
		want: &apis.FieldError{
			Message: fmt.Sprintf("not a DNS 1035 label: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]"),
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "invalid BYO generateName",
		m: &Metric{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "@-invalid",
			},
			Spec: MetricSpec{
				ScrapeTarget: "hello-12ft",
			},
		},
		want: &apis.FieldError{
			Message: fmt.Sprintf("not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]"),
			Paths:   []string{"metadata.generateName"},
		},
	}, {
		name: "empty name or generateName",
		m: &Metric{
			ObjectMeta: metav1.ObjectMeta{
				Name:         "",
				GenerateName: "",
			},
			Spec: MetricSpec{
				ScrapeTarget: "hello-12ft",
			},
		},
		want: &apis.FieldError{
			Message: fmt.Sprintf("name or generateName is required"),
			Paths:   []string{"metadata.name"},
		},
	}, {
		name: "empty metric spec",
		m: &Metric{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: MetricSpec{},
		},
		want: &apis.FieldError{
			Message: fmt.Sprintf("missing field(s)"),
			Paths:   []string{"spec"},
		},
	}, {
		name: "valid name and spec",
		m: &Metric{
			ObjectMeta: metav1.ObjectMeta{
				Name: "valid",
			},
			Spec: MetricSpec{
				ScrapeTarget: "hello-12ft",
			},
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := test.m.Validate(context.Background()).Error(), test.want.Error(); !cmp.Equal(got, want) {
				t.Errorf("Validate (-want, +got) = %s", cmp.Diff(want, got))
			}
		})
	}
}
