/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// crdpolling contains functions which poll Elafros CRDs until they
// get into the state desired by the caller or time out.

package test

import (
	"time"

	"github.com/knative/serving/pkg/apis/ela/v1alpha1"
	elatyped "github.com/knative/serving/pkg/client/clientset/versioned/typed/ela/v1alpha1"
	apiv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
)

const (
	interval = 1 * time.Second
	timeout  = 2 * time.Minute
)

// WaitForRouteState polls the status of the Route called name from client every
// interval until inState returns `true` indicating it is done, returns an
// error or timeout.
func WaitForRouteState(client elatyped.RouteInterface, name string, inState func(r *v1alpha1.Route) (bool, error)) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		r, err := client.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(r)
	})
}

// WaitForConfigurationState polls the status of the Configuration called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout.
func WaitForConfigurationState(client elatyped.ConfigurationInterface, name string, inState func(c *v1alpha1.Configuration) (bool, error)) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		c, err := client.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(c)
	})
}

// WaitForRevisionState polls the status of the Revision called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout.
func WaitForRevisionState(client elatyped.RevisionInterface, name string, inState func(r *v1alpha1.Revision) (bool, error)) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		r, err := client.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(r)
	})
}

// WaitForIngressState polls the status of the Ingress called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout.
func WaitForIngressState(client v1beta1.IngressInterface, name string, inState func(r *apiv1beta1.Ingress) (bool, error)) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		i, err := client.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(i)
	})
}
