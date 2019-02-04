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

package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/webhook"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/system"
	webhook_helper "github.com/knative/test-infra/tools/webhook-apicoverage/webhook"
	"github.com/markbates/inflect"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// Common name used for service, deployment and webhook.
	componentCommonName = "apicoverage-webhook"
	// API path to retrieve resource coverage.
	resourceCoveragePath = "/resourcecoverage"
)

var (
	// Using serving namespace.
	servingNamespace = os.Getenv(system.NamespaceEnvKey)
)

func getValidatorRules() ([]admissionregistrationv1beta1.RuleWithOperations) {
	handlers:= map[schema.GroupVersionKind]webhook.GenericCRD{
		v1alpha1.SchemeGroupVersion.WithKind("Revision"):      &v1alpha1.Revision{},
		v1alpha1.SchemeGroupVersion.WithKind("Configuration"): &v1alpha1.Configuration{},
		v1alpha1.SchemeGroupVersion.WithKind("Route"):         &v1alpha1.Route{},
		v1alpha1.SchemeGroupVersion.WithKind("Service"):       &v1alpha1.Service{},
	}

	var rules []admissionregistrationv1beta1.RuleWithOperations
	for gvk := range handlers {
		plural := strings.ToLower(inflect.Pluralize(gvk.Kind))

		rules = append(rules, admissionregistrationv1beta1.RuleWithOperations{
			Operations: []admissionregistrationv1beta1.OperationType{
				admissionregistrationv1beta1.Create,
				admissionregistrationv1beta1.Update,
			},
			Rule: admissionregistrationv1beta1.Rule{
				APIGroups:   []string{gvk.Group},
				APIVersions: []string{gvk.Version},
				Resources:   []string{plural},
			},
		})
	}
	return rules
}

func buildWebhookConfiguration() *webhook_helper.APICoverageWebhook {
	cm, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}

	config, err := logging.NewConfigFromMap(cm)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	logger, _ := logging.NewLoggerFromConfig(config, "webhook")

	clusterConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get in cluster config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		log.Fatalf("Failed to get client set: %v", err)
	}

	return &webhook_helper.APICoverageWebhook{
		Logger: logger,
		KubeClient: kubeClient,
		FailurePolicy: admissionregistrationv1beta1.Fail,
		ClientAuth: tls.NoClientCert,
		RegistrationDelay: time.Second * 2,
		Port: 443,
		Namespace: servingNamespace,
		DeploymentName: componentCommonName,
		ServiceName: componentCommonName,
		WebhookName: componentCommonName + ".knative.serving.dev",
	}
}

func main() {
	webhook := buildWebhookConfiguration()

	ac := APICoverageRecorder {
		Decoder: codecs.UniversalDeserializer(),
		Logger: webhook.Logger,
	}
	ac.init()

	m := http.NewServeMux()
	m.HandleFunc("/", ac.RecordResourceCoverage)
	m.HandleFunc(resourceCoveragePath, ac.GetResourceCoverage)

	webhook.SetupWebhook(m, getValidatorRules(), signals.SetupSignalHandler())
}