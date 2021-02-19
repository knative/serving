// +build e2e

/*
Copyright 2021 The Knative Authors

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

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	pkgHa "knative.dev/pkg/test/ha"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	domainmappingDeploymentName = "domain-mapping"
)

func TestDomainMappingHA(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}

	ctx, clients := context.Background(), test.Setup(t)

	if err := pkgTest.WaitForDeploymentScale(context.Background(), clients.KubeClient, domainmappingDeploymentName, system.Namespace(), test.ServingFlags.Replicas); err != nil {
		t.Fatalf("Deployment %s not scaled to %d: %v", domainmappingDeploymentName, test.ServingFlags.Replicas, err)
	}

	leaders, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, domainmappingDeploymentName, system.Namespace(), sets.NewString(), test.ServingFlags.Buckets)
	if err != nil {
		t.Fatal("Failed to get leader:", err)
	}
	t.Log("Got initial leader set:", leaders)

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

	assertServiceEventuallyWorks(t, clients, names, &url.URL{Scheme: "http", Host: host}, test.PizzaPlanetText1, resolvableCustomDomain)

	for _, leader := range leaders.List() {
		if err := clients.KubeClient.CoreV1().Pods(system.Namespace()).Delete(context.Background(), leader,
			metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
			t.Fatalf("Failed to delete pod %s: %v", leader, err)
		}
		if err := pkgTest.WaitForPodDeleted(context.Background(), clients.KubeClient, leader, system.Namespace()); err != nil {
			t.Fatalf("Did not observe %s to actually be deleted: %v", leader, err)
		}
	}

	// Wait for all of the old leaders to go away, and then for the right number to be back.
	if _, err := pkgHa.WaitForNewLeaders(context.Background(), t, clients.KubeClient, domainmappingDeploymentName, system.Namespace(), leaders, test.ServingFlags.Buckets); err != nil {
		t.Fatal("Failed to find new leader:", err)
	}

	// Verify that after changing the leader we can still create a new domainmapping and the second DomainMapping collided with the first.

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
	assertServiceEventuallyWorks(t, clients, names, &url.URL{Scheme: "http", Host: host}, test.PizzaPlanetText1, resolvableCustomDomain)

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
	assertServiceEventuallyWorks(t, clients, names, &url.URL{Scheme: "http", Host: host}, test.PizzaPlanetText2, resolvableCustomDomain)
}
