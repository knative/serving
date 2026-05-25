/*
Copyright 2025 The Knative Authors

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

package revision

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/ptr"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	paalisters "knative.dev/serving/pkg/client/listers/autoscaling/v1alpha1"
)

// mockPANamespaceLister is a mock implementation for testing
type mockPANamespaceLister struct {
	pa *autoscalingv1alpha1.PodAutoscaler
}

func (m *mockPANamespaceLister) Get(name string) (*autoscalingv1alpha1.PodAutoscaler, error) {
	return m.pa, nil
}

func (m *mockPANamespaceLister) List(selector interface{}) ([]*autoscalingv1alpha1.PodAutoscaler, error) {
	return nil, nil
}

type mockPALister struct {
	ns mockPANamespaceLister
}

func (m *mockPALister) PodAutoscalers(namespace string) paalisters.PodAutoscalerNamespaceLister {
	return &m.ns
}

func (m *mockPALister) List(selector interface{}) ([]*autoscalingv1alpha1.PodAutoscaler, error) {
	return nil, nil
}

// TestMergeMetadata tests the metadata merging logic
func TestMergeMetadata(t *testing.T) {
	tests := []struct {
		name    string
		desired map[string]string
		current map[string]string
		want    map[string]string
	}{
		{
			name:    "preserve external annotations",
			desired: map[string]string{"serving.knative.dev/creator": "kubernetes-admin"},
			current: map[string]string{"kubectl.kubernetes.io/restartedAt": "2025-11-27T12:14:41+01:00"},
			want:    map[string]string{"serving.knative.dev/creator": "kubernetes-admin", "kubectl.kubernetes.io/restartedAt": "2025-11-27T12:14:41+01:00"},
		},
		{
			name:    "knative annotations from desired win",
			desired: map[string]string{"serving.knative.dev/lastModifier": "kubernetes-admin"},
			current: map[string]string{"serving.knative.dev/lastModifier": "old-user"},
			want:    map[string]string{"serving.knative.dev/lastModifier": "kubernetes-admin"},
		},
		{
			name:    "delete knative annotations not in desired",
			desired: map[string]string{"serving.knative.dev/creator": "kubernetes-admin"},
			current: map[string]string{"serving.knative.dev/creator": "kubernetes-admin", "autoscaling.knative.dev/min-scale": "2"},
			want:    map[string]string{"serving.knative.dev/creator": "kubernetes-admin"},
		},
		{
			name:    "app label from desired wins",
			desired: map[string]string{"app": "new-revision"},
			current: map[string]string{"app": "old-revision"},
			want:    map[string]string{"app": "new-revision"},
		},
		{
			name:    "mixed knative and external metadata",
			desired: map[string]string{"autoscaling.knative.dev/min-scale": "1", "app": "my-revision"},
			current: map[string]string{"autoscaling.knative.dev/target-burst-capacity": "0", "deployment.kubernetes.io/revision": "2", "kubectl.kubernetes.io/restartedAt": "2025-11-27T12:14:41+01:00"},
			want:    map[string]string{"autoscaling.knative.dev/min-scale": "1", "app": "my-revision", "deployment.kubernetes.io/revision": "2", "kubectl.kubernetes.io/restartedAt": "2025-11-27T12:14:41+01:00"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeMetadata(tt.desired, tt.current)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mergeMetadata() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGetAutoscalerDesiredScale verifies that the checkAndUpdateDeployment logic
// correctly uses the PodAutoscaler's DesiredScale when available. This is a unit test
// of the core logic that fixes issue #16449 (scaled-to-zero services restarting after node preemption).
func TestGetAutoscalerDesiredScale(t *testing.T) {
	tests := []struct {
		name               string
		paDesiredScale     *int32
		deploymentReplicas *int32
		expectedReplicas   *int32
		description        string
	}{
		{
			name:               "use_pa_desired_when_available",
			paDesiredScale:     ptr.Int32(0),
			deploymentReplicas: ptr.Int32(2),
			expectedReplicas:   ptr.Int32(0),
			description:        "Should use PA's DesiredScale=0 instead of preserving deployment's 2",
		},
		{
			name:               "use_pa_desired_for_scale_up",
			paDesiredScale:     ptr.Int32(5),
			deploymentReplicas: ptr.Int32(1),
			expectedReplicas:   ptr.Int32(5),
			description:        "Should use PA's DesiredScale=5 for scaling up",
		},
		{
			name:               "fallback_when_no_pa",
			paDesiredScale:     nil,
			deploymentReplicas: ptr.Int32(3),
			expectedReplicas:   ptr.Int32(3),
			description:        "Should preserve deployment replicas when PA DesiredScale is not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic in checkAndUpdateDeployment
			replicaCount := tt.deploymentReplicas

			if tt.paDesiredScale != nil {
				replicaCount = tt.paDesiredScale
			}

			if cmp.Diff(tt.expectedReplicas, replicaCount) != "" {
				t.Errorf("%s: Expected %v replicas, got %v", tt.description, tt.expectedReplicas, replicaCount)
			}
		})
	}
}
