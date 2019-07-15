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
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kelseyhightower/envconfig"
	zipkin "github.com/openzipkin/zipkin-go"
	perrors "github.com/pkg/errors"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	"knative.dev/pkg/websocket"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatorhandler "knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/autoscaler"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	servinginformers "knative.dev/serving/pkg/client/informers/externalversions"
	"knative.dev/serving/pkg/goversion"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
	"knative.dev/serving/pkg/tracing"
	tracingconfig "knative.dev/serving/pkg/tracing/config"
)

// Fail if using unsupported go version.
var _ = goversion.IsSupported()

const (
	component = "activator"

	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10

	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100

	// The number of requests that are queued on the breaker before the 503s are sent.
	// The value must be adjusted depending on the actual production requirements.
	breakerQueueDepth = 10000

	// The upper bound for concurrent requests sent to the revision.
	// As new endpoints show up, the Breakers concurrency increases up to this value.
	breakerMaxConcurrency = 1000

	// The port on which autoscaler WebSocket server listens.
	autoscalerPort = ":8080"

	defaultResyncInterval = 10 * time.Hour
)

var (
	masterURL = flag.String("master", "", "The address of the Kubernetes API server. "+
		"Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func statReporter(statSink *websocket.ManagedConnection, stopCh <-chan struct{},
	statChan <-chan *autoscaler.StatMessage, logger *zap.SugaredLogger) {
	for {
		select {
		case sm := <-statChan:
			if err := statSink.Send(sm); err != nil {
				logger.Errorw("Error while sending stat", zap.Error(err))
			}
		case <-stopCh:
			// It's a sending connection, so no drainage required.
			statSink.Shutdown()
			return
		}
	}
}

type config struct {
	PodName string `split_words:"true" required:"true"`
}

func main() {
	flag.Parse()
	cm, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatal("Error loading logging configuration:", err)
	}
	logConfig, err := logging.NewConfigFromMap(cm)
	if err != nil {
		log.Fatal("Error parsing logging configuration:", err)
	}
	createdLogger, atomicLevel := logging.NewLoggerFromConfig(logConfig, component)
	logger := createdLogger.With(zap.String(logkey.ControllerType, "activator"))
	defer flush(logger)

	logger.Info("Starting the knative activator")

	clusterConfig, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatalw("Error getting cluster configuration", zap.Error(err))
	}
	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatalw("Error building new kubernetes client", zap.Error(err))
	}
	servingClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		logger.Fatalw("Error building serving clientset", zap.Error(err))
	}

	// We sometimes startup faster than we can reach kube-api. Poll on failure to prevent us terminating
	if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			logger.Errorw("Failed to get k8s version", zap.Error(err))
		}
		return err == nil, nil
	}); perr != nil {
		logger.Fatalw("Timed out attempting to get k8s version", zap.Error(err))
	}

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
	}

	// Set up signals so we handle the first shutdown signal gracefully.
	stopCh := signals.SetupSignalHandler()
	statChan := make(chan *autoscaler.StatMessage, statReportingQueueLength)
	defer close(statChan)

	reqChan := make(chan activatorhandler.ReqEvent, requestCountingQueueLength)
	defer close(reqChan)

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, defaultResyncInterval)
	servingInformerFactory := servinginformers.NewSharedInformerFactory(servingClient, defaultResyncInterval)
	endpointInformer := kubeInformerFactory.Core().V1().Endpoints()
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	revisionInformer := servingInformerFactory.Serving().V1alpha1().Revisions()
	sksInformer := servingInformerFactory.Networking().V1alpha1().ServerlessServices()

	// Run informers instead of starting them from the factory to prevent the sync hanging because of empty handler.
	if err := controller.StartInformers(
		stopCh,
		revisionInformer.Informer(),
		endpointInformer.Informer(),
		serviceInformer.Informer(),
		sksInformer.Informer()); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	params := queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: breakerMaxConcurrency, InitialCapacity: 0}
	throttler := activator.NewThrottler(params, endpointInformer, sksInformer.Lister(), revisionInformer.Lister(), logger)

	activatorL3 := fmt.Sprintf("%s:%d", activator.K8sServiceName, networking.ServiceHTTPPort)
	zipkinEndpoint, err := zipkin.NewEndpoint("activator", activatorL3)
	if err != nil {
		logger.Errorw("Unable to create tracing endpoint", zap.Error(err))
		return
	}
	oct := tracing.NewOpenCensusTracer(
		tracing.WithZipkinExporter(tracing.CreateZipkinReporter, zipkinEndpoint),
	)

	tracerUpdater := configmap.TypeFilter(&tracingconfig.Config{})(func(name string, value interface{}) {
		cfg := value.(*tracingconfig.Config)
		if err := oct.ApplyConfig(cfg); err != nil {
			logger.Errorw("Unable to apply open census tracer config", zap.Error(err))
			return
		}
	})

	// Set up our config store
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace())
	configStore := activatorconfig.NewStore(createdLogger, tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)

	// Open a WebSocket connection to the autoscaler.
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.%s%s", "autoscaler", system.Namespace(), network.GetClusterDomainName(), autoscalerPort)
	logger.Info("Connecting to autoscaler at", autoscalerEndpoint)
	statSink := websocket.NewDurableSendingConnection(autoscalerEndpoint, logger)
	go statReporter(statSink, stopCh, statChan, logger)

	var env config
	if err := envconfig.Process("", &env); err != nil {
		logger.Fatal("Failed to process env", err)
	}
	podName := env.PodName

	// Create and run our concurrency reporter
	reportTicker := time.NewTicker(time.Second)
	defer reportTicker.Stop()
	cr := activatorhandler.NewConcurrencyReporter(podName, reqChan, reportTicker.C, statChan)
	go cr.Run(stopCh)

	// Create activation handler chain
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first
	var ah http.Handler = activatorhandler.New(
		logger,
		reporter,
		throttler,
		revisionInformer.Lister(),
		serviceInformer.Lister(),
		sksInformer.Lister(),
	)
	ah = activatorhandler.NewRequestEventHandler(reqChan, ah)
	ah = tracing.HTTPSpanMiddleware(ah)
	ah = configStore.HTTPMiddleware(ah)
	reqLogHandler, err := pkghttp.NewRequestLogHandler(ah, logging.NewSyncFileWriter(os.Stdout), "",
		requestLogTemplateInputGetter(revisionInformer.Lister()))
	if err != nil {
		logger.Fatalw("Unable to create request log handler", zap.Error(err))
	}
	ah = reqLogHandler
	ah = &activatorhandler.ProbeHandler{NextHandler: ah}
	ah = &activatorhandler.HealthHandler{HealthCheck: statSink.Status, NextHandler: ah}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher.Watch(pkglogging.ConfigMapName(), pkglogging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	configMapWatcher.Watch(metrics.ConfigMapName(), metrics.UpdateExporterFromConfigMap(component, logger))
	// Watch the observability config map and dynamically update request logs.
	configMapWatcher.Watch(metrics.ConfigMapName(), updateRequestLogFromConfigMap(logger, reqLogHandler))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalw("Failed to start configuration manager", zap.Error(err))
	}

	servers := map[string]*http.Server{
		"http1": network.NewServer(":"+strconv.Itoa(networking.BackendHTTPPort), ah),
		"h2c":   network.NewServer(":"+strconv.Itoa(networking.BackendHTTP2Port), ah),
	}

	errCh := make(chan error, len(servers))
	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- perrors.Wrapf(err, "%s server failed", name)
			}
		}(name, server)
	}

	// Exit as soon as we see a shutdown signal or one of the servers failed.
	select {
	case <-stopCh:
	case err := <-errCh:
		logger.Errorw("Failed to run HTTP server", zap.Error(err))
	}

	for _, server := range servers {
		server.Shutdown(context.Background())
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
	metrics.FlushExporter()
}
