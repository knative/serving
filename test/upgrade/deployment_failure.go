/*
Copyright 2024 The Knative Authors

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

package upgrade

import (
	"context"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/helpers"
	pkgupgrade "knative.dev/pkg/test/upgrade"
	"knative.dev/serving/pkg/apis/autoscaling"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

const (
	webhookName = "broken-webhook"
)

func DeploymentFailurePostUpgrade() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("DeploymentFailurePostUpgrade", func(c pkgupgrade.Context) {
		clients := e2e.Setup(c.T)

		names := test.ResourceNames{
			Service: "deployment-upgrade-failure",
			Image:   test.HelloWorld,
		}
		test.EnsureCleanup(c.T, func() {
			if err := clients.KubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(
				context.Background(), webhookName, metav1.DeleteOptions{}); err != nil {
				c.T.Fatal("Failed to delete webhook:", err)
			}
			test.TearDown(clients, &names)
		})

		service, err := clients.ServingClient.Services.Get(context.Background(), names.Service, metav1.GetOptions{})
		if err != nil {
			c.T.Fatal("Failed to get Service: ", err)
		}

		// Deployment failures should surface to the Service Ready Condition
		if err := v1test.WaitForServiceState(clients.ServingClient, names.Service, v1test.IsServiceFailed, "ServiceIsNotReady"); err != nil {
			c.T.Fatal("Service did not transition to Ready=False", err)
		}

		// Traffic should still work since the deployment has an active replicaset
		url := service.Status.URL.URL()
		assertServiceResourcesUpdated(c.T, clients, names, url, test.HelloWorldText)
	})
}

func DeploymentFailurePreUpgrade() pkgupgrade.Operation {
	return pkgupgrade.NewOperation("DeploymentFailurePreUpgrade", func(c pkgupgrade.Context) {
		c.T.Log("Creating Service")
		ctx := context.Background()

		clients := e2e.Setup(c.T)
		names := &test.ResourceNames{
			Service: "deployment-upgrade-failure",
			Image:   test.HelloWorld,
		}

		resources, err := v1test.CreateServiceReady(c.T, clients, names, func(s *v1.Service) {
			s.Spec.Template.Annotations = map[string]string{
				autoscaling.MinScaleAnnotation.Key(): "1",
				autoscaling.MaxScaleAnnotation.Key(): "1",
			}
		})
		if err != nil {
			c.T.Fatal("Failed to create Service:", err)
		}

		url := resources.Service.Status.URL.URL()
		// This polls until we get a 200 with the right body.
		assertServiceResourcesUpdated(c.T, clients, *names, url, test.HelloWorldText)

		// Setup webhook that fails when deployment is updated
		// Failing to update the Deployment shouldn't cause a traffic drop
		// note: the deployment is only updated if the controllers change the spec
		//       and this happens when the queue proxy image is changed when upgrading
		c.T.Log("Creating Failing Webhook")
		noSideEffects := admissionv1.SideEffectClassNone

		selector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"serving.knative.dev/service": names.Service,
			},
		}

		// Create a broken webhook that breaks scheduling Pods
		_, err = clients.KubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Create(
			ctx,
			&admissionv1.MutatingWebhookConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: webhookName,
				},
				Webhooks: []admissionv1.MutatingWebhook{{
					AdmissionReviewVersions: []string{"v1"},
					Name:                    "webhook.non-existing.dev",
					ClientConfig: admissionv1.WebhookClientConfig{
						Service: &admissionv1.ServiceReference{
							Name:      helpers.AppendRandomString("non-existing"),
							Namespace: helpers.AppendRandomString("non-existing"),
						},
					},
					ObjectSelector: selector,
					TimeoutSeconds: ptr.Int32(5),
					SideEffects:    &noSideEffects,
					Rules: []admissionv1.RuleWithOperations{{
						Operations: []admissionv1.OperationType{"CREATE"},
						Rule: admissionv1.Rule{
							APIGroups:   []string{""}, // core
							APIVersions: []string{"v1"},
							Resources:   []string{"pods"},
						},
					}},
				}},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			c.T.Fatal("Failed to create bad webhook:", err)
		}
	})
}
