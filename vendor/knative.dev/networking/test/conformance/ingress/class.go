/*
Copyright 2020 The Knative Authors

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

package ingress

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/pkg/reconciler"
)

// TestIngressClass verifies that kingress does not pick ingress up when ingress.class annotation is incorrect.
func TestIngressClass(t *testing.T) {
	t.Parallel()
	ctx, clients := context.Background(), test.Setup(t)

	// Create a backend service to create valid ingress except for invalid ingress.class.
	name, port, _ := CreateRuntimeService(ctx, t, clients, networking.ServicePortNameHTTP1)
	ingressBackend := &v1alpha1.IngressBackend{
		ServiceName:      name,
		ServiceNamespace: test.ServingNamespace,
		ServicePort:      intstr.FromInt(port),
	}

	tests := []struct {
		name        string
		annotations map[string]string
	}{{
		name: "nil",
	}, {
		name:        "omitted",
		annotations: map[string]string{},
	}, {
		name:        "incorrect",
		annotations: map[string]string{networking.IngressClassAnnotationKey: "incorrect"},
	}, {
		name:        "empty",
		annotations: map[string]string{networking.IngressClassAnnotationKey: ""},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			verifyIngressWithAnnotations(ctx, t, clients, test.annotations, ingressBackend)
		})
	}

}

func verifyIngressWithAnnotations(ctx context.Context, t *testing.T, clients *test.Clients,
	annotations map[string]string, backend *v1alpha1.IngressBackend) {
	t.Helper()

	// createIngress internally sets hooks to delete the ingress,
	// so we can ignore `cancel` here.
	original, _ := createIngress(ctx, t, clients,
		v1alpha1.IngressSpec{
			Rules: []v1alpha1.IngressRule{{
				Hosts:      []string{backend.ServiceName + ".example.com"},
				Visibility: v1alpha1.IngressVisibilityExternalIP,
				HTTP: &v1alpha1.HTTPIngressRuleValue{
					Paths: []v1alpha1.HTTPIngressPath{{
						Splits: []v1alpha1.IngressBackendSplit{{
							IngressBackend: *backend,
						}},
					}},
				},
			}},
		},
		OverrideIngressAnnotation(annotations),
	)

	const (
		interval = 2 * time.Second
		duration = 30 * time.Second
	)
	ticker := time.NewTicker(interval)
	select {
	case <-ticker.C:
		var ing *v1alpha1.Ingress
		err := reconciler.RetryTestErrors(func(attempts int) (err error) {
			ing, err = clients.NetworkingClient.Ingresses.Get(ctx, original.Name, metav1.GetOptions{})
			return err
		})
		if err != nil {
			t.Fatal("Error getting Ingress:", err)
		}
		// Verify ingress is not changed.
		if !cmp.Equal(original, ing) {
			t.Fatalf("Unexpected update: diff(-want;+got)\n%s", cmp.Diff(original, ing))
		}
	case <-time.After(duration):
	}
}
