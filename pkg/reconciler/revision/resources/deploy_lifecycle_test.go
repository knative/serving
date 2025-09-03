/*
Copyright 2024 The Knative Authors

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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/serving/pkg/networking"
)

func TestBuildLifecycleWithDrainWait(t *testing.T) {
	drainCommand := fmt.Sprintf("until curl -f http://localhost:%d/drain-complete; do sleep 0.1; done", networking.QueueAdminPort)

	tests := []struct {
		name     string
		existing *corev1.Lifecycle
		want     []string
	}{
		{
			name:     "no existing lifecycle",
			existing: nil,
			want:     []string{"/bin/sh", "-c", drainCommand},
		},
		{
			name: "existing exec command",
			existing: &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/app/cleanup.sh"},
					},
				},
			},
			want: []string{"/bin/sh", "-c", fmt.Sprintf("/app/cleanup.sh && %s", drainCommand)},
		},
		{
			name: "existing HTTP GET",
			existing: &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(8080),
						Path: "/shutdown",
					},
				},
			},
			want: []string{"/bin/sh", "-c", fmt.Sprintf("curl -f http://localhost:8080/shutdown && %s", drainCommand)},
		},
		{
			name: "existing HTTP GET without path",
			existing: &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(9090),
					},
				},
			},
			want: []string{"/bin/sh", "-c", fmt.Sprintf("curl -f http://localhost:9090/ && %s", drainCommand)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildLifecycleWithDrainWait(tt.existing)

			if result == nil || result.PreStop == nil || result.PreStop.Exec == nil {
				t.Fatal("Expected lifecycle with exec prestop handler")
			}

			gotCommand := result.PreStop.Exec.Command
			if len(gotCommand) != len(tt.want) {
				t.Errorf("Command length mismatch: got %d, want %d", len(gotCommand), len(tt.want))
			}

			for i, cmd := range tt.want {
				if i < len(gotCommand) && gotCommand[i] != cmd {
					t.Errorf("Command[%d]: got %q, want %q", i, gotCommand[i], cmd)
				}
			}
		})
	}
}
