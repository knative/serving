// +build e2e

/*
Copyright 2020 The Knative Authors

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
	"net/url"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	v1test "knative.dev/serving/test/v1"
)

func TestDomainMapping(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloWorld,
	}

	// Clean up on test failure or interrupt.
	test.EnsureTearDown(t, clients, &names)

	// Setup initial Service.
	svc, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Using fixed hostnames can lead to conflicts when multiple tests run at
	// once, so include the svc name to avoid collisions.
	host := svc.Service.Name + ".mapping.com"

	// Point DomainMapping at our service.
	dm, err := clients.ServingAlphaClient.DomainMappings.Create(ctx, &v1alpha1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Name:      host,
			Namespace: svc.Service.Namespace,
		},
		Spec: v1alpha1.DomainMappingSpec{
			Ref: duckv1.KReference{
				Namespace: svc.Service.Namespace,
				Name:      svc.Service.Name,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create(DomainMapping) = %v, expected no error", err)
	}

	t.Cleanup(func() {
		clients.ServingAlphaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	// Wait for DomainMapping to go Ready.
	waitErr := wait.PollImmediate(test.PollInterval, 1*time.Minute, func() (bool, error) {
		state, err := clients.ServingAlphaClient.DomainMappings.Get(context.Background(), dm.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		return state.Status.IsReady(), nil
	})
	if waitErr != nil {
		t.Fatalf("The DomainMapping %s was not marked as Ready: %v", dm.Name, waitErr)
	}

	// Should be able to access the test image text via the mapped domain.
	if err := shared.CheckDistribution(ctx, t, clients, &url.URL{Host: host, Scheme: "http"}, test.ConcurrentRequests, test.ConcurrentRequests, []string{test.HelloWorldText}); err != nil {
		t.Errorf("CheckDistribution=%v, expected no error", err)
	}
}
