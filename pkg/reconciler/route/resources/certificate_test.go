/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package resources

import (
	"fmt"
	"testing"

	"github.com/knative/pkg/kmeta"

	"github.com/google/go-cmp/cmp"
	netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var route = &v1alpha1.Route{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "route",
		Namespace: "default",
		UID:       "12345",
	},
}

var dnsNames = []string{"v1.default.example.com", "subroute.v1.default.example.com"}

func TestMakeCertificate(t *testing.T) {
	want := &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", route.Name, route.UID),
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: fmt.Sprintf("%s-%s", route.Name, route.UID),
		},
	}
	got := MakeCertificate(route, dnsNames)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MakeCertificate (-want, +got) = %v", diff)
	}
}
