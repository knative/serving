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
	"testing"

	"knative.dev/serving/pkg/apis/serving"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	network "knative.dev/networking/pkg"
)

func TestIsClusterLocalService(t *testing.T) {
	tests := []struct {
		name string
		svc  *corev1.Service
		want bool
	}{{
		name: "Service does NOT have visibility label set",
		svc:  &corev1.Service{},
	}, {
		name: "Service has visibility label set to anything but ClusterLocal",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					network.VisibilityLabelKey: "something-unknown",
				},
			},
		},
	}, {
		name: "Service has visibility label set to cluster local",
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					network.VisibilityLabelKey: serving.VisibilityClusterLocal,
				},
			},
		},
		want: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsClusterLocalService(tt.svc); got != tt.want {
				t.Errorf("IsClusterLocalService() = %v, want %v", got, tt.want)
			}
		})
	}
}
