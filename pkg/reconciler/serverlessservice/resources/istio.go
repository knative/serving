/*
Copyright 2019 The Knative Authors

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

package resources

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/kmeta"
)

func MakeVirtualService(sks *v1alpha1.ServerlessService) *v1alpha3.VirtualService {
	ns := sks.Namespace
	name := kmeta.ChildName(sks.Name, "-private")
	host := fmt.Sprintf("%s.%s.svc.cluster.local", name, ns)
	vs := &v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kmeta.ChildName(sks.Name, "-private"),
			Namespace:       sks.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1alpha3.VirtualService{
			Hosts: []string{host},
			Http: []*istiov1alpha3.HTTPRoute{{
				Route: []*istiov1alpha3.HTTPRouteDestination{{
					Destination: &istiov1alpha3.Destination{
						Host:   host,
						Subset: "direct",
					},
				}},
			}},
		},
	}

	return vs
}

func MakeDestinationRule(sks *v1alpha1.ServerlessService) *v1alpha3.DestinationRule {
	ns := sks.Namespace
	name := kmeta.ChildName(sks.Name, "-private")
	host := fmt.Sprintf("%s.%s.svc.cluster.local", name, ns)

	return &v1alpha3.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       ns,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(sks)},
		},
		Spec: istiov1alpha3.DestinationRule{
			Host: host,
			Subsets: []*istiov1alpha3.Subset{{
				Name: "direct",
				TrafficPolicy: &istiov1alpha3.TrafficPolicy{
					LoadBalancer: &istiov1alpha3.LoadBalancerSettings{
						LbPolicy: &istiov1alpha3.LoadBalancerSettings_Simple{
							Simple: istiov1alpha3.LoadBalancerSettings_PASSTHROUGH,
						},
					},
				},
			}},
		},
	}
}
