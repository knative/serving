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

package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"go.uber.org/zap"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"knative.dev/pkg/environment"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
)

const (
	networkingCertificatesReconcilerLease      = "controller.knative.dev.networking.pkg.certificates.reconciler.reconciler"
	controlProtocolCertificatesReconcilerLease = "controller.knative.dev.control-protocol.pkg.certificates.reconciler.reconciler"
)

func main() {
	logger := setupLogger()
	defer logger.Sync()

	env := environment.ClientConfig{}
	env.InitFlags(flag.CommandLine)

	flag.Parse()

	config, err := env.GetRESTConfig()
	if err != nil {
		logger.Fatalf("failed to get kubeconfig %s", err)
	}

	client := kubernetes.NewForConfigOrDie(config)

	logger.Info("Deleting old Serving resources if any")

	for _, dep := range []string{"domain-mapping", "domainmapping-webhook", "net-certmanager-controller", "net-certmanager-webhook"} {
		if err = client.AppsV1().Deployments(system.Namespace()).Delete(context.Background(), dep, metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
			logger.Fatal("failed to delete deployment ", dep, ": ", err)
		}
	}

	leases, err := client.CoordinationV1().Leases(system.Namespace()).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Fatal("failed to fetch leases: ", err)
	}

	for _, lease := range leases.Items {
		if strings.HasPrefix(lease.Name, "domainmapping") ||
			strings.HasPrefix(lease.Name, "net-certmanager") ||
			strings.HasPrefix(lease.Name, networkingCertificatesReconcilerLease) || strings.HasPrefix(lease.Name, controlProtocolCertificatesReconcilerLease) {
			if err = client.CoordinationV1().Leases(system.Namespace()).Delete(context.Background(), lease.Name, metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
				logger.Fatalf("failed to delete lease %s: %v", lease.Name, err)
			}
		}
	}

	// Delete the rest of the domain mapping resources
	if err = client.CoreV1().Services(system.Namespace()).Delete(context.Background(), "domainmapping-webhook", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete service domainmapping-webhook: ", err)
	}
	if err = client.CoreV1().Secrets(system.Namespace()).Delete(context.Background(), "domainmapping-webhook-certs", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete secret domainmapping-webhook-certs: ", err)
	}
	if err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.Background(), "webhook.domainmapping.serving.knative.dev", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete mutating webhook configuration webhook.domainmapping.serving.knative.dev: ", err)
	}
	if err = client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(context.Background(), "validation.webhook.domainmapping.serving.knative.dev", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete validating webhook configuration validation.webhook.domainmapping.serving.knative.dev: ", err)
	}

	// Delete the rest of the net-certmanager resources
	if err = client.CoreV1().Services(system.Namespace()).Delete(context.Background(), "net-certmanager-controller", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete service net-certmanager-controller: ", err)
	}
	if err = client.CoreV1().Services(system.Namespace()).Delete(context.Background(), "net-certmanager-webhook", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete service net-certmanager-webhook: ", err)
	}
	if err = client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(context.Background(), "config.webhook.net-certmanager.networking.internal.knative.dev", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete validating webhook config.webhook.net-certmanager.networking.internal.knative.dev: ", err)
	}
	if err = client.CoreV1().Secrets(system.Namespace()).Delete(context.Background(), "net-certmanager-webhook-certs", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete secret net-certmanager-webhook-certs: ", err)
	}
	if err = client.RbacV1().ClusterRoles().Delete(context.Background(), "knative-serving-certmanager", metav1.DeleteOptions{}); err != nil && !apierrs.IsNotFound(err) {
		logger.Fatal("failed to delete clusterrole knative-serving-certmanager: ", err)
	}
	logger.Info("Old Serving resource deletion completed successfully")
}

func setupLogger() *zap.SugaredLogger {
	const component = "old-resource-cleanup"

	config, err := logging.NewConfigFromMap(nil)
	if err != nil {
		log.Fatal("Failed to create logging config: ", err)
	}

	logger, _ := logging.NewLoggerFromConfig(config, component)
	return logger
}
