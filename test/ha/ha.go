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
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	pkgTest "knative.dev/pkg/test"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	servingNamespace = "knative-serving"
	haReplicas       = 2
)

func getLeader(t *testing.T, clients *test.Clients, component, labelSelector string) (string, error) {
	watcher, err := clients.KubeClient.Kube.CoreV1().Events(servingNamespace).Watch(metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=Lease,involvedObject.name=%s", component),
	})
	if err != nil {
		return "", fmt.Errorf("unable to create watcher: %w", err)
	}
	defer watcher.Stop()
	eventCh := watcher.ResultChan()
	timeoutCh := time.After(time.Minute)
	for {
		select {
		case <-timeoutCh:
			return "", fmt.Errorf("timeout")
		case event := <-eventCh:
			lease := event.Object.(*corev1.Event)
			if strings.Contains(lease.Message, "became leader") {
				eventPod := strings.Split(lease.Message, "_")[0]
				currentPodList, err := clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).List(metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				if err != nil {
					return "", fmt.Errorf("error retrieving pods with label %s: %w", labelSelector, err)
				}
				for _, pod := range currentPodList.Items {
					if pod.Name == eventPod { // the leader must be an existing pod, ignore old events
						return eventPod, nil
					}
				}
			}
		}
	}
}

func waitForPodDeleted(t *testing.T, clients *test.Clients, podName string) error {
	if err := wait.PollImmediate(test.PollInterval, time.Minute, func() (bool, error) {
		if _, err := clients.KubeClient.Kube.CoreV1().Pods(servingNamespace).Get(podName, metav1.GetOptions{}); err != nil {
			if apierrs.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		return err
	}
	return nil
}

func scaleUpDeployment(clients *test.Clients, name string) error {
	return scaleDeployment(clients, name, haReplicas)
}

func scaleDownDeployment(clients *test.Clients, name string) error {
	return scaleDeployment(clients, name, 1 /*target number of replicas*/)
}

func scaleDeployment(clients *test.Clients, name string, replicas int) error {
	scaleRequest := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: int32(replicas)}}
	scaleRequest.Name = name
	scaleRequest.Namespace = servingNamespace
	if _, err := clients.KubeClient.Kube.AppsV1().Deployments(servingNamespace).UpdateScale(name, scaleRequest); err != nil {
		return fmt.Errorf("error scaling: %w", err)
	}
	return pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		name,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == int32(replicas), nil
		},
		"DeploymentIsScaled",
		servingNamespace,
		time.Minute,
	)
}

func createPizzaPlanetService(t *testing.T, serviceName string, fopt ...rtesting.ServiceOption) (test.ResourceNames, *v1test.ResourceObjects) {
	t.Helper()
	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: serviceName,
		Image:   test.PizzaPlanet1,
	}
	resources, err := v1test.CreateServiceReady(t, clients, &names, fopt...)
	if err != nil {
		t.Fatalf("Failed to create Service: %v", err)
	}

	assertServiceWorks(t, clients, names, resources.Service.Status.URL.URL(), test.PizzaPlanetText1)
	return names, resources
}

func assertServiceWorks(t pkgTest.TLegacy, clients *test.Clients, names test.ResourceNames, url *url.URL, expectedText string) {
	t.Helper()
	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		url,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.EventuallyMatchesBody(expectedText))),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatal(fmt.Sprintf("The endpoint for Route %s at %s didn't serve the expected text %q: %v", names.Route, url, expectedText, err))
	}
}
