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

	// The set of controllers this controller process runs.
	"flag"
	"log"
	"os"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/net-certmanager/reconciler/certificate"
	"knative.dev/serving/pkg/reconciler/configuration"
	"knative.dev/serving/pkg/reconciler/domainmapping"
	"knative.dev/serving/pkg/reconciler/gc"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/nscert"
	"knative.dev/serving/pkg/reconciler/revision"
	"knative.dev/serving/pkg/reconciler/route"
	"knative.dev/serving/pkg/reconciler/serverlessservice"
	"knative.dev/serving/pkg/reconciler/service"

	"knative.dev/serving/pkg/net-certmanager/client/certmanager/injection/informers/acme/v1/challenge"
	v1certificate "knative.dev/serving/pkg/net-certmanager/client/certmanager/injection/informers/certmanager/v1/certificate"
	"knative.dev/serving/pkg/net-certmanager/client/certmanager/injection/informers/certmanager/v1/certificaterequest"
	"knative.dev/serving/pkg/net-certmanager/client/certmanager/injection/informers/certmanager/v1/clusterissuer"
	"knative.dev/serving/pkg/net-certmanager/client/certmanager/injection/informers/certmanager/v1/issuer"
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

	// Allow configuration of threads per controller
	if val, ok := os.LookupEnv("K_THREADS_PER_CONTROLLER"); ok {
		threadsPerController, err := strconv.Atoi(val)
		if err != nil {
			log.Fatalf("failed to parse value %q of K_THREADS_PER_CONTROLLER: %v\n", val, err)
		}
		controller.DefaultThreadsPerController = threadsPerController
	}

	// TODO(mattmoor): Remove this once HA is stable.
	disableHighAvailability := flag.Bool("disable-ha", false,
		"Whether to disable high-availability functionality for this component.  This flag will be deprecated "+
			"and removed when we have promoted this feature to stable, so do not pass it without filing an "+
			"issue upstream!")

	// HACK: This parses flags, so the above should be set once this runs.
	cfg := injection.ParseAndGetRESTConfigOrDie()

	if *disableHighAvailability {
		ctx = sharedmain.WithHADisabled(ctx)
	}

	// If nil it panics
	client := kubernetes.NewForConfigOrDie(cfg)

	if shouldEnableNetCertManagerController(ctx, client) {
		ctors = append(ctors, certificate.NewController) // add the net-certmanager controller
	} else {
		// Remove all non required informers
		ctx = injection.WithExcludeInformerPredicate(ctx, excludeNetCertManagerInformers)
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

func excludeNetCertManagerInformers(ctx context.Context, inf controller.Informer) bool {
	isCertInf := v1certificate.Get(ctx).Informer() == inf
	isCertReqInf := certificaterequest.Get(ctx).Informer() == inf
	isClusterIssuerInf := clusterissuer.Get(ctx).Informer() == inf
	isIssuerInf := issuer.Get(ctx).Informer() == inf
	isChallengeInf := challenge.Get(ctx).Informer() == inf

	return isCertInf || isCertReqInf || isClusterIssuerInf || isIssuerInf || isChallengeInf
}
