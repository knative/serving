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
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	// Injection related imports.
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/injection"
	revisioninformer "knative.dev/serving/pkg/client/injection/informers/serving/v1alpha1/revision"

	"github.com/kelseyhightower/envconfig"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection/sharedmain"
	pkglogging "knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/tracing"
	tracingconfig "knative.dev/pkg/tracing/config"
	"knative.dev/pkg/version"
	"knative.dev/pkg/websocket"
	"knative.dev/serving/pkg/activator"
	activatorconfig "knative.dev/serving/pkg/activator/config"
	activatorhandler "knative.dev/serving/pkg/activator/handler"
	activatornet "knative.dev/serving/pkg/activator/net"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/autoscaler"
	"knative.dev/serving/pkg/goversion"
	pkghttp "knative.dev/serving/pkg/http"
	"knative.dev/serving/pkg/logging"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/queue"
)

// Fail if using unsupported go version.
var _ = goversion.IsSupported()

const (
	component = "activator"

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
)

var (
	masterURL = flag.String("master", "", "The address of the Kubernetes API server. "+
		"Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func statReporter(statSink *websocket.ManagedConnection, stopCh <-chan struct{},
	statChan <-chan []autoscaler.StatMessage, logger *zap.SugaredLogger) {
	for {
		select {
		case sm := <-statChan:
			go func() {
				for _, msg := range sm {
					if err := statSink.Send(msg); err != nil {
						logger.Errorw("Error while sending stat", zap.Error(err))
					}
				}
			}()
		case <-stopCh:
			// It's a sending connection, so no drainage required.
			statSink.Shutdown()
			return
		}
	}
}

type config struct {
	PodName string `split_words:"true" required:"true"`
	PodIP   string `split_words:"true" required:"true"`
}

func main() {
	flag.Parse()

	// Set up a context that we can cancel to tell informers and other subprocesses to stop.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Report stats on Go memory usage every 30 seconds.
	msp := metrics.NewMemStatsAll()
	msp.Start(ctx, 30*time.Second)
	if err := view.Register(msp.DefaultViews()...); err != nil {
		log.Fatalf("Error exporting go memstats view: %v", err)
	}

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig:", err)
	}

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))

	ctx, informers := injection.Default.SetupInformers(ctx, cfg)

	// Set up our logger.
	loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration:", err)
	}
	logger, atomicLevel := pkglogging.NewLoggerFromConfig(loggingConfig, component)
	logger = logger.With(zap.String(logkey.ControllerType, component))
	ctx = pkglogging.WithLogger(ctx, logger)
	defer flush(logger)

	kubeClient := kubeclient.Get(ctx)

	// Run informers instead of starting them from the factory to prevent the sync hanging because of empty handler.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	logger.Info("Starting the knative activator")

	var env config
	if err := envconfig.Process("", &env); err != nil {
		logger.Fatalw("Failed to process env", zap.Error(err))
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

	statCh := make(chan []autoscaler.StatMessage)
	defer close(statCh)

	reqCh := make(chan activatorhandler.ReqEvent, requestCountingQueueLength)
	defer close(reqCh)

	params := queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: breakerMaxConcurrency, InitialCapacity: 0}

	// Start throttler.
	throttler := activatornet.NewThrottler(ctx, params,
		// We want to join host port since that will be our search space in the Throttler.
		net.JoinHostPort(env.PodIP, strconv.Itoa(networking.BackendHTTPPort)))
	go throttler.Run(ctx)

	oct := tracing.NewOpenCensusTracer(tracing.WithExporter(networking.ActivatorServiceName, logger))

	tracerUpdater := configmap.TypeFilter(&tracingconfig.Config{})(func(name string, value interface{}) {
		cfg := value.(*tracingconfig.Config)
		if err := oct.ApplyConfig(cfg); err != nil {
			logger.Errorw("Unable to apply open census tracer config", zap.Error(err))
			return
		}
	})

	// Set up our config store
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace())
	configStore := activatorconfig.NewStore(logger, tracerUpdater)
	configStore.WatchConfigs(configMapWatcher)

	// Open a WebSocket connection to the autoscaler.
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.%s%s", "autoscaler", system.Namespace(), pkgnet.GetClusterDomainName(), autoscalerPort)
	logger.Info("Connecting to Autoscaler at ", autoscalerEndpoint)
	statSink := websocket.NewDurableSendingConnection(autoscalerEndpoint, logger)
	go statReporter(statSink, ctx.Done(), statCh, logger)

	// Create and run our concurrency reporter
	reportTicker := time.NewTicker(time.Second)
	defer reportTicker.Stop()
	cr := activatorhandler.NewConcurrencyReporter(ctx, env.PodName, reqCh,
		reportTicker.C, statCh, reporter)
	go cr.Run(ctx.Done())

	// Create activation handler chain
	// Note: innermost handlers are specified first, ie. the last handler in the chain will be executed first
	var ah http.Handler = activatorhandler.New(
		ctx,
		throttler,
		reporter)
	ah = activatorhandler.NewRequestEventHandler(reqCh, ah)
	ah = tracing.HTTPSpanMiddleware(ah)
	ah = configStore.HTTPMiddleware(ah)
	reqLogHandler, err := pkghttp.NewRequestLogHandler(ah, logging.NewSyncFileWriter(os.Stdout), "",
		requestLogTemplateInputGetter(revisioninformer.Get(ctx).Lister()), false /*enableProbeRequestLog*/)
	if err != nil {
		logger.Fatalw("Unable to create request log handler", zap.Error(err))
	}
	ah = reqLogHandler
	ah = &activatorhandler.ProbeHandler{NextHandler: ah}

	// Set up our health check based on the health of stat sink and environmental factors.
	// When drainCh is closed, we should start to drain connections.
	hc, drainCh := newHealthCheck(logger, statSink)
	ah = &activatorhandler.HealthHandler{HealthCheck: hc, NextHandler: ah}

	ah = network.NewProbeHandler(ah)

	// NOTE: MetricHandler is being used as the outermost handler for the purpose of measuring the request latency.
	ah = activatorhandler.NewMetricHandler(ctx, reporter, ah)

	profilingHandler := profiling.NewHandler(logger, false)
	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher.Watch(pkglogging.ConfigMapName(), pkglogging.UpdateLevelFromConfigMap(logger, atomicLevel, component))

	// Watch the observability config map
	configMapWatcher.Watch(metrics.ConfigMapName(),
		metrics.UpdateExporterFromConfigMap(component, logger),
		updateRequestLogFromConfigMap(logger, reqLogHandler),
		profilingHandler.UpdateFromConfigMap)

	if err = configMapWatcher.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start configuration manager", zap.Error(err))
	}

	servers := map[string]*http.Server{
		"http1":   network.NewServer(":"+strconv.Itoa(networking.BackendHTTPPort), ah),
		"h2c":     network.NewServer(":"+strconv.Itoa(networking.BackendHTTP2Port), ah),
		"profile": profiling.NewServer(profilingHandler),
	}

	errCh := make(chan error, len(servers))
	for name, server := range servers {
		go func(name string, s *http.Server) {
			// Don't forward ErrServerClosed as that indicates we're already shutting down.
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("%s server failed: %w", name, err)
			}
		}(name, server)
	}

	// Wait for the signal to drain.
	select {
	case <-drainCh:
		logger.Info("Received the drain signal.")
	case err := <-errCh:
		logger.Errorw("Failed to run HTTP server", zap.Error(err))
	}

	// The drain has started (we are now failing readiness probes).  Let the effects of this
	// propagate so that new requests are no longer routed our way.
	time.Sleep(30 * time.Second)
	logger.Info("Done waiting, shutting down servers.")

	// Drain outstanding requests, and stop accepting new ones.
	for _, server := range servers {
		server.Shutdown(context.Background())
	}
	logger.Info("Servers shutdown.")
}

func newHealthCheck(logger *zap.SugaredLogger, statSink *websocket.ManagedConnection) (func() error, <-chan struct{}) {
	// When we get SIGTERM (sigCh closes), start failing readiness probes.
	sigCh := signals.SetupSignalHandler()

	// Some duration after our first readiness probe failure (to allow time
	// for the network to reprogram) send the signal to drain connections.
	drainCh := make(chan struct{})
	once := sync.Once{}

	return func() error {
		select {
		case <-sigCh:
			// Signal to start the process of draining.
			once.Do(func() {
				logger.Info("Received SIGTERM")
				close(drainCh)
			})
			return errors.New("received SIGTERM from kubelet")
		default:
			logger.Debug("No signal yet.")
			return statSink.Status()
		}
	}, drainCh
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	os.Stdout.Sync()
	os.Stderr.Sync()
	metrics.FlushExporter()
}
