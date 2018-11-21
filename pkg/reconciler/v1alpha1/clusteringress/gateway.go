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

package clusteringress

import (
	"context"
	"encoding/json"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/knative/pkg/apis/duck"
	istiov1alpha3 "github.com/knative/pkg/apis/istio/v1alpha3"
	istioclients "github.com/knative/pkg/client/clientset/versioned/typed/istio/v1alpha3"
	"github.com/knative/serving/pkg/system"
)

var (
	gatewayName = "knative-shared-gateway"
	// TODO(lichuqiang): make this configurable.
	ingressSuffixes = []string{".svc", ".svc.cluster.local"}
)

func (c *Reconciler) updateGatewayLabelSelector() {
	// TODO: improve this when we find way to pass in ctx.
	ctx := context.TODO()
	ctx = c.configStore.ToContext(ctx)

	ingressUrl := ingressGatewayFromContext(ctx)
	// Extract ingress service from the URL.
	for _, suffix := range ingressSuffixes {
		ingressUrl = strings.TrimSuffix(ingressUrl, suffix)
	}
	// The url should be in format of <service-name>.<service-namespace> then.
	serviceInfo := strings.Split(ingressUrl, ".")
	if len(serviceInfo) != 2 {
		c.Logger.Errorf("Failed to update gateway label selector, invalid ingress url: %s", ingressUrl)
		return
	}
	serviceName, serviceNamespace := serviceInfo[0], serviceInfo[1]

	// Fetch the selector to update to from ingress service.
	service, err := c.KubeClientSet.CoreV1().Services(serviceNamespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		c.Logger.Errorf("Failed to update gateway label selector, error getting ingress service %s/%s: %v",
			serviceNamespace, serviceName, err)
		return
	}
	newSelector := service.Spec.Selector

	// Fetch origin selector from gateway.
	gatewayClient := c.SharedClientSet.NetworkingV1alpha3().Gateways(system.Namespace)
	gateway, err := gatewayClient.Get(gatewayName, metav1.GetOptions{})
	if err != nil {
		c.Logger.Errorf("Failed to update gateway label selector, error getting gateway %s/%s: %v",
			system.Namespace, gatewayName, err)
		return
	}
	originSelector := gateway.Spec.Selector
	if equality.Semantic.DeepEqual(newSelector, originSelector) {
		// Skip when no update in label selector.
		return
	}

	if err := setLabelSelectorForGateway(gatewayClient, gateway, newSelector); err != nil {
		c.Logger.Errorf("Failed to update gateway label selector: %v", err)
	}

	return
}

func setLabelSelectorForGateway(
	gatewayClient istioclients.GatewayInterface,
	gateway *istiov1alpha3.Gateway,
	selector map[string]string,
) error {
	before, after := gateway, gateway.DeepCopy()
	after.Spec.Selector = selector
	patch, err := duck.CreatePatch(before, after)
	if err != nil {
		return err
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = gatewayClient.Patch(gatewayName, types.JSONPatchType, patchBytes)
	return err
}
