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
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/knative/serving/cmd/util"
	"github.com/knative/serving/pkg/autoscaler"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/knative/pkg/logging/logkey"

	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/signals"
	"github.com/knative/pkg/version"
	"github.com/knative/pkg/websocket"
	"github.com/knative/serving/pkg/activator"
	activatorhandler "github.com/knative/serving/pkg/activator/handler"
	activatorutil "github.com/knative/serving/pkg/activator/util"
	v1alpha12 "github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions"
	kubeinformers "k8s.io/client-go/informers"
	corev1 "k8s.io/api/core/v1"
	"github.com/knative/serving/pkg/http/h2c"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/metrics"
	"github.com/knative/serving/pkg/system"
	"github.com/knative/serving/pkg/utils"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"github.com/knative/serving/pkg/queue"
	"github.com/knative/serving/pkg/reconciler"
)

const (
	maxUploadBytes = 32e6 // 32MB - same as app engine
	component      = "activator"

	maxRetries             = 18 // the sum of all retries would add up to 1 minute
	minRetryInterval       = 100 * time.Millisecond
	exponentialBackoffBase = 1.3

	// Add a little buffer space between request handling and stat
	// reporting so that latency in the stat pipeline doesn't
	// interfere with request handling.
	statReportingQueueLength = 10

	// Add enough buffer to not block request serving on stats collection
	requestCountingQueueLength = 100

	// The number of requests that are queued on the breaker before the 503s are sent
	// the value must be adjusted depending on the actual production requirements
	breakerQueueDepth = 10000

	// The upper bound for concurrent requests sent to the revision,
	// as new endpoints show up, the Breakers concurrency increases up to this value
	breakerMaxConcurrency = 1000

	defaultResyncInterval = 10 * time.Hour
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")

	logger *zap.SugaredLogger

	statSink *websocket.ManagedConnection
	statChan = make(chan *autoscaler.StatMessage, statReportingQueueLength)
	reqChan  = make(chan activatorhandler.ReqEvent, requestCountingQueueLength)
)

func statReporter(stopCh <-chan struct{}) {
	for {
		select {
		case sm := <-statChan:
			if statSink == nil {
				logger.Error("Stat sink not connected")
				continue
			}
			err := statSink.Send(sm)
			if err != nil {
				logger.Errorw("Error while sending stat", zap.Error(err))
			}
		case <-stopCh:
			return
		}
	}
}

func main() {
	flag.Parse()
	cm, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	config, err := logging.NewConfigFromMap(cm)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	createdLogger, atomicLevel := logging.NewLoggerFromConfig(config, component)
	logger = createdLogger.With(zap.String(logkey.ControllerType, "activator"))
	defer logger.Sync()

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

	if err := version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
		logger.Fatalf("Version check failed: %v", err)
	}

	reporter, err := activator.NewStatsReporter()
	if err != nil {
		logger.Fatalw("Failed to create stats reporter", zap.Error(err))
	}

	// Set up signals so we handle the first shutdown signal gracefully.
	stopCh := signals.SetupSignalHandler()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, defaultResyncInterval)
	servingInformerFactory := servinginformers.NewSharedInformerFactory(servingClient, defaultResyncInterval)
	endpointInformer := kubeInformerFactory.Core().V1().Endpoints()
	revisionInformer := servingInformerFactory.Serving().V1alpha1().Revisions()

	go endpointInformer.Informer().Run(stopCh)
	go revisionInformer.Informer().Run(stopCh)

	params := queue.BreakerParams{QueueDepth: breakerQueueDepth, MaxConcurrency: breakerMaxConcurrency, InitialCapacity: 0}

	// Return the number of endpoints, 0 if no endpoints are found.
	endpointsGetter := func(revID activator.RevisionID) (int32, error) {
		endpointKey := cache.ExplicitKey(revID.Namespace + "/" + reconciler.GetServingK8SServiceNameForObj(revID.Name))
		endpoints, exists, err := endpointInformer.Informer().GetIndexer().Get(endpointKey)
		if err != nil {
			return 0, fmt.Errorf("Error getting endpoints for revision %v", revID)
		}
		if !exists {
			// No endpoints exist yet.
			return 0, nil
		}
		if exists && endpoints != nil {
			endpoint := endpoints.(*corev1.Endpoints).Subsets
			addresses := activator.EndpointsAddressCount(endpoint)
			return int32(addresses), nil
		}
		return 0, nil
	}

	// Return the revision from the observer.
	// Additionally to observer errors an error is returned if either no revision exists yet
	// or we could not cast the interface to Revision for some reason.
	revisionGetter := func(revID activator.RevisionID) (revision *v1alpha12.Revision, err error) {
		revisionKey := cache.ExplicitKey(revID.Namespace + "/" + revID.Name)
		rev, ok, err := revisionInformer.Informer().GetIndexer().Get(revisionKey)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("Error getting revision for %v", revID)
		}
		if revision, ok = rev.(*v1alpha12.Revision); !ok {
			return nil, fmt.Errorf("Error casting revision %v", revID)
		}
		return revision, err
	}

	throttlerParams := activator.ThrottlerParams{BreakerParams: params, Logger: logger, GetEndpoints: endpointsGetter, GetRevision: revisionGetter}
	throttler := activator.NewThrottler(throttlerParams)

	// Update/create the breaker in the throttler when the number of endpoints changes.
	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: activator.UpdateEndpoints(throttler),
	})

	// Remove the breaker if the revision was deleted.
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: activator.DeleteBreaker(throttler),
	})

	kubeInformerFactory.Start(stopCh)
	servingInformerFactory.Start(stopCh)

	logger.Info("Waiting for informer caches to sync")

	informerSyncs := []cache.InformerSynced{
		endpointInformer.Informer().HasSynced,
		revisionInformer.Informer().HasSynced,
	}
	for i, synced := range informerSyncs {
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %d to sync", i)
		}
	}

	a := activator.NewRevisionActivator(kubeClient, servingClient, logger)
	a = activator.NewDedupingActivator(a)

	// Retry on 503's for up to 60 seconds. The reason is there is
	// a small delay for k8s to include the ready IP in service.
	// https://github.com/knative/serving/issues/660#issuecomment-384062553
	shouldRetry := activatorutil.RetryStatus(http.StatusServiceUnavailable)
	backoffSettings := wait.Backoff{
		Duration: minRetryInterval,
		Factor:   exponentialBackoffBase,
		Steps:    maxRetries,
	}
	rt := activatorutil.NewRetryRoundTripper(activatorutil.AutoTransport, logger, backoffSettings, shouldRetry)

	// Open a websocket connection to the autoscaler
	autoscalerEndpoint := fmt.Sprintf("ws://%s.%s.svc.%s:%s", "autoscaler", system.Namespace(), utils.GetClusterDomainName(), "8080")
	logger.Infof("Connecting to autoscaler at %s", autoscalerEndpoint)
	statSink = websocket.NewDurableSendingConnection(autoscalerEndpoint, logger)
	go statReporter(stopCh)

	podName := util.GetRequiredEnvOrFatal("POD_NAME", logger)

	// Create and run our concurrency reporter
	cr := activatorhandler.NewConcurrencyReporter(podName, reqChan, time.NewTicker(time.Second).C, statChan)
	go cr.Run(stopCh)

	ah := &activatorhandler.FilteringHandler{
		NextHandler: activatorhandler.NewRequestEventHandler(reqChan,
			&activatorhandler.EnforceMaxContentLengthHandler{
				MaxContentLengthBytes: maxUploadBytes,
				NextHandler: &activatorhandler.ActivationHandler{
					Activator: a,
					Transport: rt,
					Logger:    logger,
					Reporter:  reporter,
					Throttler: throttler,
				},
			},
		),
	}

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace())
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map and dynamically update metrics exporter.
	configMapWatcher.Watch(metrics.ObservabilityConfigName, metrics.UpdateExporterFromConfigMap(component, logger))
	if err = configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalw("Failed to start configuration manager", zap.Error(err))
	}

	http1Srv := h2c.NewServer(":8080", ah)
	go func() {
		if err := http1Srv.ListenAndServe(); err != nil {
			logger.Errorw("Error running HTTP server", zap.Error(err))
		}
	}()

	h2cSrv := h2c.NewServer(":8081", ah)
	go func() {
		if err := h2cSrv.ListenAndServe(); err != nil {
			logger.Errorw("Error running HTTP server", zap.Error(err))
		}
	}()

	<-stopCh
	a.Shutdown()
	http1Srv.Shutdown(context.TODO())
	h2cSrv.Shutdown(context.TODO())
}
