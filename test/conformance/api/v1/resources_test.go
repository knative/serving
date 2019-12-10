// +build e2e

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

package v1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/test/logging"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	rtesting "knative.dev/serving/pkg/testing/v1"
)

var resourceLimit resource.Quantity

func init() {
	resourceLimit = resource.MustParse(test.ContainerMemoryLimit)
}

func TestMustSetCustomResourcesLimits(legacy *testing.T) {
	t, cancel := logging.NewTLogger(legacy)
	defer cancel()

	clients, names, objects, cancel, err := v1test.CreateBasicServiceTest(t,
		test.Autoscale,
		rtesting.WithResourceRequirements(corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resourceLimit,
			},
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resourceLimit,
			},
		}))
	defer cancel()
	t.FatalIfErr(err, "Failed to create initial Service", "name", names.Service)

	t.Run("API", func(t *logging.TLogger) {
		svc, err := clients.ServingClient.Revisions.Get(objects.Revision.Status.ServiceName, metav1.GetOptions{})
		t.FatalIfErr(err, "Failed requesting information about Revision")

		// TODO: need to not panic if any nil pointers/missing keys
		resources := svc.Spec.Containers[0].Resources
		limit := resources.Limits["memory"]
		request := resources.Requests["memory"]

		if limit.Cmp(resourceLimit) != 0 {
			t.Error("Memory limit did not match", "want", resourceLimit, "got", limit)
		}
		if request.Cmp(resourceLimit) != 0 {
			t.Error("Memory request did not match", "want", resourceLimit, "got", request)
		}
	})
}
