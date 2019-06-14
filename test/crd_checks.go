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

// crdpolling contains functions which poll Knative Serving CRDs until they
// get into the state desired by the caller or time out.

package test

import (
	pkgTest "github.com/knative/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	k8styped "k8s.io/client-go/kubernetes/typed/core/v1"
)

// GetConfigMap gets the knative serving config map.
func GetConfigMap(client *pkgTest.KubeClient) k8styped.ConfigMapInterface {
	return client.Kube.CoreV1().ConfigMaps("knative-serving")
}

// DeploymentScaledToZeroFunc returns a func that evaluates if a deployment has scaled to 0 pods.
func DeploymentScaledToZeroFunc(d *appsv1.Deployment) (bool, error) {
	return d.Status.ReadyReplicas == 0, nil
}
