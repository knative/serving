/*
Copyright 2017 The Kubernetes Authors.

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

package elaservice

import (
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/google/elafros/pkg/controller/util"

	apiv1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var httpServicePortName = "http"
var servicePort = 80

// MakeElaServiceK8SService creates a Service that targets nothing. This is now only
// a placeholder so that we can route the traffic to Istio and the balance with
// route rules exclusively to underlying k8s services that represent Revisions.
func MakeElaServiceK8SService(u *v1alpha1.ElaService) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      util.GetElaK8SServiceName(u),
			Namespace: u.Namespace,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Name: httpServicePortName,
					Port: int32(servicePort),
				},
			},
			Selector: map[string]string{},
		},
	}
}
