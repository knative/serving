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

	"go.opencensus.io/trace"
	corev1 "k8s.io/api/core/v1"
	apiv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
)

// WaitForDeploymentState polls the status of the Deployment called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForDeploymentState(client v1beta1.DeploymentInterface, name string, inState func(d *apiv1beta1.Deployment) (bool, error), desc string) error {
	metricName := fmt.Sprintf("WaitForDeploymentState/%s/%s", name, desc)
	_, span := trace.StartSpan(context.Background(), metricName)
	defer span.End()

	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		d, err := client.Get(name, metav1.GetOptions{})
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
func WaitForPodListState(client v1.PodInterface, inState func(p *corev1.PodList) (bool, error), desc string) error {
	metricName := fmt.Sprintf("WaitForPodListState/%s", desc)
	_, span := trace.StartSpan(context.Background(), metricName)
	defer span.End()

	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		p, err := client.List(metav1.ListOptions{})
		if err != nil {
			return true, err
		}
		return inState(p)
	})
}
