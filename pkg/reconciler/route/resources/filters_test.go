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
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"knative.dev/serving/pkg/reconciler/route/config"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFilterService(t *testing.T) {
	tests := []struct {
		name         string
		services     []*corev1.Service
		acceptFilter Filter
		want         []*corev1.Service
	}{
		{
			name: "no services",
		},
		{
			name: "matches services",
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "bar-2",
					},
				},
			},
			acceptFilter: func(service *corev1.Service) bool {
				return strings.HasPrefix(service.Name, "foo")
			},
			want: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo-1",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if got := FilterService(tt.services, tt.acceptFilter); !cmp.Equal(got, tt.want) {
				t.Errorf("FilterService() (-want, +got) = %v", cmp.Diff(tt.want, got))
			}
		})
	}
}

func TestIsClusterLocalService(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{
		{
			name: "Service does NOT have visibility label set",
			svc:  &corev1.Service{},
			want: false,
		},
		{
			name: "Service has visibility label set to anything but ClusterLocal",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						config.VisibilityLabelKey: "something-unknown",
					},
				},
			},
			want: false,
		},
		{
			name: "Service has visibility label set to cluster local",
			svc: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						config.VisibilityLabelKey: config.VisibilityClusterLocal,
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsClusterLocalService(tt.svc); got != tt.want {
				t.Errorf("IsClusterLocalService() = %v, want %v", got, tt.want)
			}
		})
	}
}
