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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

func TestNamer(t *testing.T) {
	tests := []struct {
		name  string
		route *v1alpha1.Route
		f     func(kmeta.Accessor) string
		want  string
	}{{
		name:  "K8sService",
		route: getRoute("blah", "default", ""),
		f:     K8sService,
		want:  "blah",
	}, {
		name:  "K8sServiceFullname",
		route: getRoute("bar", "default", ""),
		f:     K8sServiceFullname,
		want:  "bar.default.svc.cluster.local",
	}, {
		name:  "IngressPrefix",
		route: getRoute("bar", "default", "1234-5678-910"),
		f:     Ingress,
		want:  "bar",
	}, {
		name:  "Certificate",
		route: getRoute("bar", "default", "1234-5678-910"),
		f:     Certificate,
		want:  "route-1234-5678-910",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.f(test.route)
			if got != test.want {
				t.Errorf("%s() = %v, wanted %v", test.name, got, test.want)
			}
		})
	}
}

func getRoute(name, ns string, uid types.UID) *v1alpha1.Route {
	return &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       uid,
		},
	}
}
