//go:build e2e
// +build e2e

/*
Copyright 2022 The Knative Authors

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

package domainmapping

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	"knative.dev/serving/test"
	e2e "knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	wsServerTestImageName = "wsserver"
)

func TestDomainMappingWebsocket(t *testing.T) {
	if !test.ServingFlags.EnableBetaFeatures {
		t.Skip("Beta features not enabled")
	}

	if !strings.Contains(test.ServingFlags.IngressClass, "kourier") {
		t.Skip("Skip this test for non-kourier ingress.")
	}

	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   wsServerTestImageName,
	}

	test.EnsureTearDown(t, clients, &names)
	ctx := context.Background()

	ksvc, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	host := ksvc.Service.Name + ".example.org"
	if test.ServingFlags.CustomDomain != "" {
		host = ksvc.Service.Name + "." + test.ServingFlags.CustomDomain
	}

	dm := v1beta1.DomainMapping{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:        host,
			Namespace:   ksvc.Service.Namespace,
			Annotations: map[string]string{"kourier.knative.dev/disable-http2": "true"},
		},
		Spec: v1beta1.DomainMappingSpec{
			Ref: duckv1.KReference{
				APIVersion: "serving.knative.dev/v1",
				Name:       ksvc.Service.Name,
				Namespace:  ksvc.Service.Namespace,
				Kind:       "Service",
			},
		},
		Status: v1beta1.DomainMappingStatus{},
	}

	_, err = clients.ServingBetaClient.DomainMappings.Create(ctx, &dm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Problem creating DomainMapping %q: %v", host, err)
	}
	test.EnsureCleanup(t, func() {
		clients.ServingBetaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	waitErr := wait.PollUntilContextTimeout(ctx, test.PollInterval, test.PollTimeout, true, func(context.Context) (bool, error) {
		var err error
		dm, err := clients.ServingBetaClient.DomainMappings.Get(ctx, dm.Name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}

		return dm.IsReady(), nil
	})
	if waitErr != nil {
		t.Fatalf("The DomainMapping %s was not marked as Ready: %v", dm.Name, waitErr)
	}

	if err := e2e.ValidateWebSocketConnection(t, clients, names, ""); err != nil {
		t.Error(err)
	}
}
