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

// kube_checks contains functions which poll Kubernetes objects until
// they get into the state desired by the caller or time out.

package test

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8styped "k8s.io/client-go/kubernetes/typed/core/v1"
	"knative.dev/pkg/test/logging"
)

const (
	interval   = 1 * time.Second
	podTimeout = 8 * time.Minute
	logTimeout = 1 * time.Minute
)

// WaitForDeploymentState polls the status of the Deployment called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForDeploymentState(client *KubeClient, name string, inState func(d *appsv1.Deployment) (bool, error), desc string, namespace string, timeout time.Duration) error {
	d := client.Kube.AppsV1().Deployments(namespace)
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForDeploymentState/%s/%s", name, desc))
	defer span.End()

	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		d, err := d.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(d)
	})
}

// WaitForPodListState polls the status of the PodList
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took to get into the state checked by inState.
func WaitForPodListState(client *KubeClient, inState func(p *corev1.PodList) (bool, error), desc string, namespace string) error {
	p := client.Kube.CoreV1().Pods(namespace)
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForPodListState/%s", desc))
	defer span.End()

	return wait.PollImmediate(interval, podTimeout, func() (bool, error) {
		p, err := p.List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		return inState(p)
	})
}

// GetConfigMap gets the configmaps for a given namespace
func GetConfigMap(client *KubeClient, namespace string) k8styped.ConfigMapInterface {
	return client.Kube.CoreV1().ConfigMaps(namespace)
}

// DeploymentScaledToZeroFunc returns a func that evaluates if a deployment has scaled to 0 pods
func DeploymentScaledToZeroFunc() func(d *appsv1.Deployment) (bool, error) {
	return func(d *appsv1.Deployment) (bool, error) {
		return d.Status.ReadyReplicas == 0, nil
	}
}

// WaitForLogContent waits until logs for given Pod/Container include the given content.
// If the content is not present within timeout it returns error.
func WaitForLogContent(client *KubeClient, podName, containerName, namespace, content string) error {
	return wait.PollImmediate(interval, logTimeout, func() (bool, error) {
		logs, err := client.PodLogs(podName, containerName, namespace)
		if err != nil {
			return true, err
		}
		return strings.Contains(string(logs), content), nil
	})
}

// WaitForAllPodsRunning waits for all the pods to be in running state
func WaitForAllPodsRunning(client *KubeClient, namespace string) error {
	return WaitForPodListState(client, PodsRunning, "PodsAreRunning", namespace)
}

// WaitForPodRunning waits for the given pod to be in running state
func WaitForPodRunning(client *KubeClient, name string, namespace string) error {
	p := client.Kube.CoreV1().Pods(namespace)
	return wait.PollImmediate(interval, podTimeout, func() (bool, error) {
		p, err := p.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return PodRunning(p), nil
	})
}

// PodsRunning will check the status conditions of the pod list and return true all pods are Running
func PodsRunning(podList *corev1.PodList) (bool, error) {
	for _, pod := range podList.Items {
		if isRunning := PodRunning(&pod); !isRunning {
			return false, nil
		}
	}
	return true, nil
}

// PodRunning will check the status conditions of the pod and return true if it's Running
func PodRunning(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
}
