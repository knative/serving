// +build e2e

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

package e2e

import (
	"fmt"
	"strings"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logstream"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHelloWorld(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{
		RevisionTemplateAnnotations: map[string]string{},
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	domain := resources.Route.Status.URL.Host

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText))),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, test.HelloWorldText, err)
	}

	revision := resources.Revision
	if val, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if val != names.Config {
			t.Fatalf("Expect configuration name in revision label %q but got %q ", names.Config, val)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if val, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if val != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, val)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}
}

func TestQueueSideCarResourceLimit(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	resources, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names, &v1a1test.Options{
		RevisionTemplateAnnotations: map[string]string{
			serving.QueueSideCarResourcePercentageAnnotation: "0.2",
		},
		ContainerResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"):    resource.MustParse("50m"),
				corev1.ResourceName("memory"): resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceName("cpu"):    resource.MustParse("100m"),
				corev1.ResourceName("memory"): resource.MustParse("258Mi"),
			},
		},
	})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}
	domain := resources.Route.Status.URL.Host

	if _, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		domain,
		v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText))),
		"HelloWorldServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, test.HelloWorldText, err)
	}

	revision := resources.Revision
	if val, ok := revision.Labels["serving.knative.dev/configuration"]; ok {
		if val != names.Config {
			t.Fatalf("Expect configuration name in revision label %q but got %q ", names.Config, val)
		}
	} else {
		t.Fatalf("Failed to get configuration name from Revision label")
	}
	if val, ok := revision.Labels["serving.knative.dev/service"]; ok {
		if val != names.Service {
			t.Fatalf("Expect Service name in revision label %q but got %q ", names.Service, val)
		}
	} else {
		t.Fatalf("Failed to get Service name from Revision label")
	}

	container, err := getContainer(clients.KubeClient, resources.Service.Name, "queue-proxy", resources.Service.Namespace)
	if err != nil {
		t.Fatalf("Failed to get queue-proxy container in the pod %v in namespace %v: %v", resources.Service.Name, resources.Service.Namespace, err)
	}

	if container.Resources.Limits.Cpu().Cmp(resource.MustParse("40m")) != 0 {
		t.Fatalf("queue-proxy should have limit.cpu set to 40m got %v", container.Resources.Limits.Cpu())
	}
	if container.Resources.Limits.Memory().Cmp(resource.MustParse("200Mi")) != 0 {
		t.Fatalf("queue-proxy should have limit.memory set to 200Mi got %v", container.Resources.Limits.Memory())
	}
	if container.Resources.Requests.Cpu().Cmp(resource.MustParse("25m")) != 0 {
		t.Fatalf("queue-proxy should have request.cpu set to 25m got %v", container.Resources.Requests.Cpu())
	}
	if container.Resources.Requests.Memory().Cmp(resource.MustParse("50Mi")) != 0 {
		t.Fatalf("queue-proxy should have request.memory set to 50Mi got %v", container.Resources.Requests.Memory())
	}
}

// Container returns container for given Pod and Container in the namespace
func getContainer(client *pkgTest.KubeClient, podName, containerName, namespace string) (corev1.Container, error) {
	pods := client.Kube.CoreV1().Pods(namespace)
	podList, err := pods.List(metav1.ListOptions{})
	if err != nil {
		return corev1.Container{}, err
	}
	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, podName) {
			result, err := pods.Get(pod.Name, metav1.GetOptions{})
			if err != nil {
				return corev1.Container{}, err
			}
			for _, container := range result.Spec.Containers {
				if strings.Contains(container.Name, containerName) {
					return container, nil
				}
			}
		}
	}
	return corev1.Container{}, fmt.Errorf("Could not find container for %s/%s", podName, containerName)
}
