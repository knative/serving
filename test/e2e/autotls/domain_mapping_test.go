// +build e2e

/*
Copyright 2021 The Knative Authors

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

package autotls

import (
	"context"
	"testing"

	"github.com/kelseyhightower/envconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ntest "knative.dev/networking/test"
	"knative.dev/networking/test/conformance/ingress"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

type dmConfig struct {
	// TLSServiceName is the name of testing Knative Service.
	// It is not required for self-signed CA or for the HTTP01 challenge when wildcard domain
	// is mapped to the Ingress IP.
	TLSServiceName string `envconfig:"tls_service_name" required:"false"`
	// TLSTestNamespace is the namespace of where the tls tests run.
	TLSTestNamespace string `envconfig:"tls_test_namespace" required:"false"`
	// CustomDomainSuffix is the custom domain used for the domainMapping.
	CustomDomainSuffix string `envconfig:"custom_domain_suffix" required:"false"`
}

func TestDomainMappingAutoTLS(t *testing.T) {
	if !ntest.NetworkingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	t.Parallel()

	var env dmConfig
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v", err)
	}

	ctx := context.Background()

	clients := e2e.SetupWithNamespace(t, test.TLSNamespace)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}

	if len(env.TLSServiceName) != 0 {
		names.Service = env.TLSServiceName + "-dmtls"
	}

	// Clean up on test failure or interrupt.
	test.EnsureTearDown(t, clients, &names)

	// Set up initial Service.
	svc, err := v1test.CreateServiceReady(t, clients, &names,
		func(service *v1.Service) {
			service.Annotations = map[string]string{"networking.knative.dev/disableAutoTLS": "True"}
		})
	if err != nil {
		t.Fatalf("Failed to create initial Service %q: %v", names.Service, err)
	}

	// Using fixed hostnames can lead to conflicts when multiple tests run at
	// once, so include the svc name to avoid collisions.
	suffix := "example.com"

	if env.CustomDomainSuffix != "" {
		suffix = env.CustomDomainSuffix
	}

	if env.TLSTestNamespace != "" {
		suffix = env.TLSTestNamespace + "." + suffix
	}

	host := "dm." + suffix

	// Point DomainMapping at our service.
	var dm *v1alpha1.DomainMapping
	if err := reconciler.RetryTestErrors(func(int) error {
		dm, err = clients.ServingAlphaClient.DomainMappings.Create(ctx, &v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      host,
				Namespace: svc.Service.Namespace,
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace:  svc.Service.Namespace,
					Name:       svc.Service.Name,
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		}, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Create(DomainMapping) = %v, expected no error", err)
	}

	t.Cleanup(func() {
		clients.ServingAlphaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	// Wait for DomainMapping to go Ready.
	if waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		state, err := clients.ServingAlphaClient.DomainMappings.Get(ctx, dm.Name, metav1.GetOptions{})

		// DomainMapping can go Ready if only http is available.
		// Hence the checking for the URL scheme to make sure it is ready for https
		dmTLSReady := state.IsReady() && state.Status.URL != nil && state.Status.URL.Scheme == "https"

		return dmTLSReady, err
	}); waitErr != nil {
		t.Fatalf("The DomainMapping %q was not marked as Ready: %v", dm.Name, waitErr)
	}

	certName := dm.Name
	rootCAs := createRootCAs(t, clients, svc.Route.Namespace, certName)
	httpsClient := createHTTPSClient(t, clients, svc, rootCAs)
	ingress.RuntimeRequest(ctx, t, httpsClient, "https://"+host)
}
