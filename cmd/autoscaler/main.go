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

// Multitenant autoscaler executable.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/leaderelection"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/websocket"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/autoscaler/scaling"
	"knative.dev/serving/pkg/autoscaler/statserver"
	smetrics "knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	"knative.dev/serving/pkg/reconciler/metric"
	"knative.dev/serving/pkg/resources"
)

const (
	statsServerAddr = ":8080"
	statsBufferLen  = 1000
	component       = "autoscaler"
	controllerNum   = 2
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")

	b2IPM sync.RWMutex
	b2IP  = make(map[string]string)
)

func main() {
	// Set up signals so we handle the first shutdown signal gracefully.
	ctx := signals.NewContext()

	// Report stats on Go memory usage every 30 seconds.
	msp := metrics.NewMemStatsAll()
	msp.Start(ctx, 30*time.Second)
	if err := view.Register(msp.DefaultViews()...); err != nil {
		log.Fatal("Error exporting go memstats view: ", err)
	}

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig: ", err)
	}

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))
	log.Printf("Registering %d controllers", controllerNum)

	// Adjust our client's rate limits based on the number of controller's we are running.
	cfg.QPS = controllerNum * rest.DefaultQPS
	cfg.Burst = controllerNum * rest.DefaultBurst

	ctx, informers := injection.Default.SetupInformers(ctx, cfg)

	kubeClient := kubeclient.Get(ctx)

	// We sometimes startup faster than we can reach kube-api. Poll on failure to prevent us terminating
	if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			log.Print("Failed to get k8s version ", err)
		}
		return err == nil, nil
	}); perr != nil {
		log.Fatal("Timed out attempting to get k8s version: ", err)
	}

	// Set up our logger.
	loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration: ", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)
	ctx = logging.WithLogger(ctx, logger)

	// statsCh is the main communication channel between the stats server and multiscaler.
	statsCh := make(chan asmetrics.StatMessage, statsBufferLen)
	defer close(statsCh)

	profilingHandler := profiling.NewHandler(logger, false)

	ns := system.Namespace()
	cmw := configmap.NewInformedWatcher(kubeClient, ns)
	// Watch the logging config map and dynamically update logging levels.
	cmw.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map
	cmw.Watch(metrics.ConfigMapName(),
		metrics.ConfigMapWatcher(component, nil /* SecretFetcher */, logger),
		profilingHandler.UpdateFromConfigMap)

	podLister := podinformer.Get(ctx).Lister()

	collector := asmetrics.NewMetricCollector(
		statsScraperFactoryFunc(podLister), logger)

	// Set up scalers.
	// uniScalerFactory depends endpointsInformer to be set.
	multiScaler := scaling.NewMultiScaler(ctx.Done(),
		uniScalerFactoryFunc(podLister, collector), logger)

	controllers := []*controller.Impl{
		kpa.NewController(ctx, cmw, multiScaler),
		metric.NewController(ctx, cmw, collector),
	}

	// Set up a statserver.
	statsServer := statserver.New(statsServerAddr, statsCh, logger)

	// Start watching the configs.
	if err := cmw.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start watching configs", zap.Error(err))
	}

	// Start all of the informers and wait for them to sync.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	selfIP := os.Getenv("POD_IP")
	cc := leaderelection.ComponentConfig{
		Component:             "autoscaler",
		Buckets:               5,
		SharedReconcilerCount: len(controllers),
		LeaseDuration:         15 * time.Second,
		RenewDeadline:         10 * time.Second,
		RetryPeriod:           2 * time.Second,
	}
	ctx = leaderelection.WithDynamicLeaderElectorBuilder(
		ctx, kubeClient, cc)
	go controller.StartAll(ctx, controllers...)

	bs := leaderelection.NewEndpointsBucketSet(cc)

	go watch(ctx, kubeClient, bs.Buckets(), selfIP)
	// acceptStat is the func to call when this pod owns the given StatMessage.
	acceptStat := func(sm asmetrics.StatMessage) {
		collector.Record(sm.Key, time.Now(), sm.Stat)
		multiScaler.Poke(sm.Key, sm.Stat)
	}

	processStat, cancel := statForwarder(3, acceptStat, logger, bs, selfIP)

	defer cancel()
	go func() {
		for sm := range statsCh {
			processStat(sm)
		}
	}()

	profilingServer := profiling.NewServer(profilingHandler)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(statsServer.ListenAndServe)
	eg.Go(profilingServer.ListenAndServe)

	// This will block until either a signal arrives or one of the grouped functions
	// returns an error.
	<-egCtx.Done()

	statsServer.Shutdown(5 * time.Second)
	profilingServer.Shutdown(context.Background())
	// Don't forward ErrServerClosed as that indicates we're already shutting down.
	if err := eg.Wait(); err != nil && err != http.ErrServerClosed {
		logger.Errorw("Error while running server", zap.Error(err))
	}
}

func uniScalerFactoryFunc(podLister corev1listers.PodLister,
	metricClient asmetrics.MetricClient) scaling.UniScalerFactory {
	return func(decider *scaling.Decider) (scaling.UniScaler, error) {
		configName := decider.Labels[serving.ConfigurationLabelKey]
		if configName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.ConfigurationLabelKey, decider.Name)
		}
		revisionName := decider.Labels[serving.RevisionLabelKey]
		if revisionName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.RevisionLabelKey, decider.Name)
		}
		serviceName := decider.Labels[serving.ServiceLabelKey] // This can be empty.

		// Create a stats reporter which tags statistics by PA namespace, configuration name, and PA name.
		ctx, err := smetrics.RevisionContext(decider.Namespace, serviceName, configName, revisionName)
		if err != nil {
			return nil, err
		}

		podAccessor := resources.NewPodAccessor(podLister, decider.Namespace, revisionName)
		return scaling.New(decider.Namespace, decider.Name, metricClient,
			podAccessor, &decider.Spec, ctx)
	}
}

func statsScraperFactoryFunc(podLister corev1listers.PodLister) asmetrics.StatsScraperFactory {
	return func(metric *av1alpha1.Metric, logger *zap.SugaredLogger) (asmetrics.StatsScraper, error) {
		if metric.Spec.ScrapeTarget == "" {
			return nil, nil
		}

		revisionName := metric.Labels[serving.RevisionLabelKey]
		if revisionName == "" {
			return nil, fmt.Errorf("label %q not found or empty in Metric %s", serving.RevisionLabelKey, metric.Name)
		}

		podAccessor := resources.NewPodAccessor(podLister, metric.Namespace, revisionName)
		return asmetrics.NewStatsScraper(metric, podAccessor, logger)
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	metrics.FlushExporter()
}

type leaseInfo struct {
	HolderIdentity string `json:"holderIdentity"`
}

func getIP(name string) string {
	b2IPM.RLock()
	defer b2IPM.RUnlock()
	return b2IP[name]
}

func setIP(name, IP string) {
	b2IPM.Lock()
	defer b2IPM.Unlock()
	b2IP[name] = IP
}

func watch(ctx context.Context, kubeClient kubernetes.Interface, buckets []reconciler.Bucket, selfIP string) {
	el := endpointsinformer.Get(ctx).Lister().Endpoints(system.Namespace())
	sl := serviceinformer.Get(ctx).Lister().Services(system.Namespace())

	for {
		for _, b := range buckets {
			name := b.Name()
			e, err := el.Get(name)
			if err != nil {
				log.Printf("## get endpoint %s error\n", name)
				continue
			}

			if v, ok := e.Annotations["control-plane.alpha.kubernetes.io/leader"]; ok {
				l := &leaseInfo{}
				if err := json.Unmarshal([]byte(v), l); err != nil {
					log.Printf("## got json error: %v\n", err)
					continue
				}

				if getIP(name) != l.HolderIdentity {
					setIP(name, l.HolderIdentity)
					if l.HolderIdentity == selfIP {
						if _, err := sl.Get(name); err != nil {
							if apierrs.IsNotFound(err) {
								createService(kubeClient, name)
							}
						}
						want := e.DeepCopy()
						want.Subsets = []v1.EndpointSubset{
							v1.EndpointSubset{
								Addresses: []v1.EndpointAddress{
									v1.EndpointAddress{IP: selfIP},
								},
								Ports: []v1.EndpointPort{
									v1.EndpointPort{
										Name: "http",
										Port: 8080,
									},
								},
							},
						}
						kubeClient.CoreV1().Endpoints(system.Namespace()).Update(want)
					}
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func createService(kubeClient kubernetes.Interface, name string) {
	ns := system.Namespace()
	_, err := kubeClient.CoreV1().Services(ns).Create(&v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				v1.ServicePort{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	})
	if err != nil {
		log.Printf("## create svc err: %v", err)
	}
}

type statProcessor func(sm asmetrics.StatMessage)

// statForwarder returns two functions:
// 1) a function to process a single StatMessage.
// 2) a function to call when terminating.
func statForwarder(bucketSize int, accept statProcessor, logger *zap.SugaredLogger, bs *hash.BucketSet, selfIP string) (statProcessor, func()) {
	// TODO: Same to activator, use gRPC instead of websocket for sending metrics.
	wsMap := make(map[string]*websocket.ManagedConnection, bucketSize-1)
	for _, b := range bs.Buckets() {
		dns := fmt.Sprintf("ws://%s.%s.svc.%s:8080", b.Name(), system.Namespace(), pkgnet.GetClusterDomainName())
		logger.Info("Connecting to Autoscaler at bucket", dns)
		statSink := websocket.NewDurableSendingConnection(dns, logger)
		wsMap[dns] = statSink
	}

	return func(sm asmetrics.StatMessage) {
			// If this pod has the key, accept it.
			targetIP := getIP(bs.Owner(sm.Key.String()))
			if targetIP == selfIP {
				accept(sm)
				return
			}

			// Otherwise forward to the owner.
			if ws, ok := wsMap[bs.Owner(sm.Key.String())]; ok {
				ws.Send(sm)
			} else {
				logger.Warnf("Couldn't find Pod owner for Stat with Key %v, dropping it", sm.Key)
			}
		}, func() {
			for _, ws := range wsMap {
				ws.Shutdown()
			}
		}
}
