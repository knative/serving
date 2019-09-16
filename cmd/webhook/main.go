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
	"flag"
	"log"
	"net/http"

	// Injection related imports.
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/profiling"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	"knative.dev/pkg/webhook"
	autoscalingv1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	apiconfig "knative.dev/serving/pkg/apis/config"
	net "knative.dev/serving/pkg/apis/networking/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

const (
	component = "webhook"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func main() {
	flag.Parse()

	// Set up signals so we handle the first shutdown signal gracefully.
	ctx := signals.NewContext()

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Failed to get cluster config:", err)
	}

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))

	ctx, _ = injection.Default.SetupInformers(ctx, cfg)
	kubeClient := kubeclient.Get(ctx)

	config, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration:", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(config, component)
	defer logger.Sync()
	logger = logger.With(zap.String(logkey.ControllerType, component))

	if err := version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
		logger.Fatalw("Version check failed", err)
	}

	profilingHandler := profiling.NewHandler(logger, false)

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace())
	// Watch the observability config map and dynamically update request logs.
	configMapWatcher.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map
	configMapWatcher.Watch(metrics.ConfigMapName(),
		metrics.UpdateExporterFromConfigMap(component, logger),
		profilingHandler.UpdateFromConfigMap)

	store := apiconfig.NewStore(logger.Named("config-store"))
	store.WatchConfigs(configMapWatcher)

	if err = configMapWatcher.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start the ConfigMap watcher", zap.Error(err))
	}

	options := webhook.ControllerOptions{
		ServiceName:                     "webhook",
		DeploymentName:                  "webhook",
		Namespace:                       system.Namespace(),
		Port:                            8443,
		SecretName:                      "webhook-certs",
		ResourceMutatingWebhookName:     "webhook.serving.knative.dev",
		ResourceAdmissionControllerPath: "/",
	}

	resourceHandlers := map[schema.GroupVersionKind]webhook.GenericCRD{
		v1alpha1.SchemeGroupVersion.WithKind("Revision"):                 &v1alpha1.Revision{},
		v1alpha1.SchemeGroupVersion.WithKind("Configuration"):            &v1alpha1.Configuration{},
		v1alpha1.SchemeGroupVersion.WithKind("Route"):                    &v1alpha1.Route{},
		v1alpha1.SchemeGroupVersion.WithKind("Service"):                  &v1alpha1.Service{},
		v1beta1.SchemeGroupVersion.WithKind("Revision"):                  &v1beta1.Revision{},
		v1beta1.SchemeGroupVersion.WithKind("Configuration"):             &v1beta1.Configuration{},
		v1beta1.SchemeGroupVersion.WithKind("Route"):                     &v1beta1.Route{},
		v1beta1.SchemeGroupVersion.WithKind("Service"):                   &v1beta1.Service{},
		v1.SchemeGroupVersion.WithKind("Revision"):                       &v1.Revision{},
		v1.SchemeGroupVersion.WithKind("Configuration"):                  &v1.Configuration{},
		v1.SchemeGroupVersion.WithKind("Route"):                          &v1.Route{},
		v1.SchemeGroupVersion.WithKind("Service"):                        &v1.Service{},
		autoscalingv1alpha1.SchemeGroupVersion.WithKind("PodAutoscaler"): &autoscalingv1alpha1.PodAutoscaler{},
		autoscalingv1alpha1.SchemeGroupVersion.WithKind("Metric"):        &autoscalingv1alpha1.Metric{},
		net.SchemeGroupVersion.WithKind("Certificate"):                   &net.Certificate{},
		net.SchemeGroupVersion.WithKind("ClusterIngress"):                &net.ClusterIngress{},
		net.SchemeGroupVersion.WithKind("Ingress"):                       &net.Ingress{},
		net.SchemeGroupVersion.WithKind("ServerlessService"):             &net.ServerlessService{},
	}

	resourceAdmissionController := webhook.NewResourceAdmissionController(resourceHandlers, options, true)
	admissionControllers := map[string]webhook.AdmissionController{
		options.ResourceAdmissionControllerPath: resourceAdmissionController,
	}

	// Decorate contexts with the current state of the config.
	ctxFunc := func(ctx context.Context) context.Context {
		return v1.WithUpgradeViaDefaulting(store.ToContext(ctx))
	}

	controller, err := webhook.New(kubeClient, options, admissionControllers, logger, ctxFunc)

	if err != nil {
		logger.Fatalw("Failed to create admission controller", zap.Error(err))
	}

	profilingServer := profiling.NewServer(profilingHandler)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return controller.Run(ctx.Done())
	})
	eg.Go(profilingServer.ListenAndServe)

	// This will block until either a signal arrives or one of the grouped functions
	// returns an error.
	<-egCtx.Done()

	profilingServer.Shutdown(context.Background())
	// Don't forward ErrServerClosed as that indicates we're already shutting down.
	if err := eg.Wait(); err != nil && err != http.ErrServerClosed {
		logger.Errorw("Error while running server", zap.Error(err))
	}
}
