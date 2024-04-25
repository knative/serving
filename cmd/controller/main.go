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

package main

import (
	"context"
	"fmt"

	// The set of controllers this controller process runs.
	"flag"
	"log"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/reconciler/certificate"
	"knative.dev/serving/pkg/reconciler/configuration"
	"knative.dev/serving/pkg/reconciler/domainmapping"
	"knative.dev/serving/pkg/reconciler/gc"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/nscert"
	"knative.dev/serving/pkg/reconciler/revision"
	"knative.dev/serving/pkg/reconciler/route"
	"knative.dev/serving/pkg/reconciler/serverlessservice"
	"knative.dev/serving/pkg/reconciler/service"

	versioned "knative.dev/serving/pkg/client/certmanager/clientset/versioned"
	"knative.dev/serving/pkg/client/certmanager/injection/informers/acme/v1/challenge"
	v1certificate "knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/certificate"
	"knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/certificaterequest"
	"knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/clusterissuer"
	"knative.dev/serving/pkg/client/certmanager/injection/informers/certmanager/v1/issuer"
)

var ctors = []injection.ControllerConstructor{
	configuration.NewController,
	labeler.NewController,
	revision.NewController,
	route.NewController,
	serverlessservice.NewController,
	service.NewController,
	gc.NewController,
	nscert.NewController,
	domainmapping.NewController,
}

func main() {
	flag.DurationVar(&reconciler.DefaultTimeout,
		"reconciliation-timeout", reconciler.DefaultTimeout,
		"The amount of time to give each reconciliation of a resource to complete before its context is canceled.")

	ctx := signals.NewContext()

	// HACK: This parses flags, so the above should be set once this runs.
	cfg := injection.ParseAndGetRESTConfigOrDie()

	// If nil it panics
	client := kubernetes.NewForConfigOrDie(cfg)

	if shouldEnableNetCertManagerController(ctx, client) {
		v := versioned.NewForConfigOrDie(cfg)
		if ok, err := certManagerCRDsExist(v); !ok {
			log.Fatalf("Please install cert-manager: %v", err)
		}
		for _, inf := range []injection.InformerInjector{challenge.WithInformer, v1certificate.WithInformer, certificaterequest.WithInformer, clusterissuer.WithInformer, issuer.WithInformer} {
			injection.Default.RegisterInformer(inf)
		}
		ctors = append(ctors, certificate.NewController)
	}

	sharedmain.MainWithConfig(ctx, "controller", cfg, ctors...)
}

func shouldEnableNetCertManagerController(ctx context.Context, client *kubernetes.Clientset) bool {
	var cm *v1.ConfigMap
	var err error
	if cm, err = client.CoreV1().ConfigMaps(system.Namespace()).Get(ctx, "config-network", metav1.GetOptions{}); err != nil {
		log.Fatalf("Failed to get cm config-network: %v", err)
	}
	netCfg, err := netcfg.NewConfigFromMap(cm.Data)
	if err != nil {
		log.Fatalf("Failed to construct network config: %v", err)
	}

	return netCfg.ExternalDomainTLS || netCfg.SystemInternalTLSEnabled() || (netCfg.ClusterLocalDomainTLS == netcfg.EncryptionEnabled) ||
		netCfg.NamespaceWildcardCertSelector != nil
}

func certManagerCRDsExist(client *versioned.Clientset) (bool, error) {
	if ok, err := findCRD(client, "cert-manager.io/v1", []string{"certificaterequests", "certificates", "clusterissuers", "issuers"}); !ok {
		return false, err
	}
	if ok, err := findCRD(client, "acme.cert-manager.io/v1", []string{"challenges"}); !ok {
		return false, err
	}
	return true, nil
}

func findCRD(client *versioned.Clientset, groupVersion string, crds []string) (bool, error) {
	resourceList, err := client.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		return false, err
	}
	for _, crdName := range crds {
		isCRDPresent := false
		for _, resource := range resourceList.APIResources {
			if resource.Name == crdName {
				isCRDPresent = true
			}
		}
		if !isCRDPresent {
			return false, fmt.Errorf("cert manager crds are missing: %s", crdName)
		}
	}
	return true, nil
}
