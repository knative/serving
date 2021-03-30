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

package ha

import (
	"context"
	"net/url"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	"knative.dev/serving/test/e2e"
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
		Image:   test.PizzaPlanet1,
	}

	// Clean up on test failure or interrupt.
	test.EnsureTearDown(t, clients, &names)

	// Set up initial Service.
	svc, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Using fixed hostnames can lead to conflicts when multiple tests run at
	// once, so include the svc name to avoid collisions.
	host := svc.Service.Name + ".example.org"
	// Set resolvabledomain for custom domain to false by default.
	resolvableCustomDomain := false

	if test.ServingFlags.CustomDomain != "" {
		host = svc.Service.Name + "." + test.ServingFlags.CustomDomain
		resolvableCustomDomain = true
	}
	// Point DomainMapping at our service.
	var dm *v1alpha1.DomainMapping
	if err := reconciler.RetryTestErrors(func(int) error {
		dm, err = clients.ServingAlphaClient.DomainMappings.Create(ctx, &v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      host,
				Namespace: svc.Service.Namespace,
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace:  svc.Service.Namespace,
					Name:       svc.Service.Name,
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		}, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Create(DomainMapping) = %v, expected no error", err)
	}

	t.Cleanup(func() {
		clients.ServingAlphaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	// Wait for DomainMapping to go Ready.
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		state, err := clients.ServingAlphaClient.DomainMappings.Get(context.Background(), dm.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		return state.IsReady(), nil
	})
	if waitErr != nil {
		t.Fatalf("The DomainMapping %s was not marked as Ready: %v", dm.Name, waitErr)
	}

	// Should be able to access the test image text via the mapped domain.
	if err := shared.CheckDistribution(ctx, t, clients, &url.URL{Host: host, Scheme: "http"}, test.ConcurrentRequests, test.ConcurrentRequests, []string{test.PizzaPlanetText1}, resolvableCustomDomain); err != nil {
		t.Errorf("CheckDistribution=%v, expected no error", err)
	}

	altClients := e2e.SetupAlternativeNamespace(t)
	altNames := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet2,
	}
	test.EnsureTearDown(t, altClients, &altNames)

	// Set up second Service in alt namespace.
	altSvc, err := v1test.CreateServiceReady(t, altClients, &altNames)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", altNames.Service, err)
	}

	// Create second domain mapping with same name in alt namespace - this will collide with the existing mapping.
	var altDm *v1alpha1.DomainMapping
	if err := reconciler.RetryTestErrors(func(int) error {
		altDm, err = altClients.ServingAlphaClient.DomainMappings.Create(ctx, &v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      host,
				Namespace: altSvc.Service.Namespace,
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace:  altSvc.Service.Namespace,
					Name:       altSvc.Service.Name,
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		}, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Create(DomainMapping) = %v, expected no error", err)
	}

	t.Cleanup(func() {
		altClients.ServingAlphaClient.DomainMappings.Delete(ctx, altDm.Name, metav1.DeleteOptions{})
	})

	// Second domain mapping should go to DomainMappingConditionDomainClaimed=false state.
	waitErr = wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		state, err := altClients.ServingAlphaClient.DomainMappings.Get(context.Background(), dm.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		return state.Generation == state.Status.ObservedGeneration &&
			state.Status.GetCondition(v1alpha1.DomainMappingConditionDomainClaimed).IsFalse(), nil
	})
	if waitErr != nil {
		t.Fatalf("The second DomainMapping %s did not enter DomainMappingConditionDomainClaimed=false state: %v", altDm.Name, waitErr)
	}

	// Because the second DomainMapping collided with the first, it should not have taken effect.
	if err := shared.CheckDistribution(ctx, t, clients, &url.URL{Host: host, Scheme: "http"}, test.ConcurrentRequests, test.ConcurrentRequests, []string{test.PizzaPlanetText1}, resolvableCustomDomain); err != nil {
		t.Errorf("CheckDistribution=%v, expected no error", err)
	}

	// Delete the first DomainMapping.
	if err := clients.ServingAlphaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete=%v, expected no error", err)
	}

	// The second DomainMapping should now be able to claim the domain.
	waitErr = wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		state, err := altClients.ServingAlphaClient.DomainMappings.Get(context.Background(), altDm.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		return state.IsReady(), nil
	})
	if waitErr != nil {
		t.Fatalf("The second DomainMapping %s was not marked as Ready: %v", dm.Name, waitErr)
	}

	// The domain name should now point to the second service.
	if err := shared.CheckDistribution(ctx, t, clients, &url.URL{Host: host, Scheme: "http"}, test.ConcurrentRequests, test.ConcurrentRequests, []string{test.PizzaPlanetText2}, resolvableCustomDomain); err != nil {
		t.Errorf("CheckDistribution=%v, expected no error", err)
	}
}
