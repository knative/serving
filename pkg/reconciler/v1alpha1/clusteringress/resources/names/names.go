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

package names

import (
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/system"
)

// K8sGatewayFullname is the fully-qualified name of Knative shared gateway.
var K8sGatewayFullname = reconciler.GetK8sServiceFullname(
	"knative-shared-gateway",
	system.Namespace)

// K8sGatewayServiceFullname is the fully-qualified name of in-cluster Knative gateway.
var K8sGatewayServiceFullname = reconciler.GetK8sServiceFullname(
	"knative-ingressgateway",
	"istio-system")

// VirtualService returns the name of the VirtualService child resource for given ClusterIngress.
func VirtualService(i *v1alpha1.ClusterIngress) string {
	return i.Name
}
