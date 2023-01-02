package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"go.uber.org/zap"

	// injection
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/injection"
	"knative.dev/serving/pkg/activator"
	"knative.dev/serving/pkg/http/handler"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"knative.dev/control-protocol/pkg/certificates"
	network "knative.dev/networking/pkg"
	netcfg "knative.dev/networking/pkg/config"
	netprobe "knative.dev/networking/pkg/http/probe"
	"knative.dev/pkg/configmap"
	configmapinformer "knative.dev/pkg/configmap/informer"
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
	activatorconfig "knative.dev/serving/pkg/activator/config"
  activatorhandler "knative.dev/serving/pkg/activator/handler"
  apiconfig "knative.dev/serving/pkg/apis/config"
  asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
  pkghttp "knative.ev/serving/pkg/http"
  "knative.dev/serving/pkg/logging"
  "knative.dev/serving/pkg/networking"
)

const (
  component = "activator"

  // listening on this port for autoscaler WebSocket server
  autoscalerPort = ":8080"
)

type config struct {
  PodName string `split_words:"true" required:"true"`
  PodIP string `split_words:"true" required:"true"`

  // set up higher keep-alive values for bigger deployments
  // @todo - to research what are better starting values via running loadtests with these flags
  MaxIdleProxyConns int `split_words:"true" default:"1000"`
  MaxIdleProxyConnsPerHost int `split_words:"true" default:"100"`
}

func main() {
  // configure context with which we can cancel to message informers/other subprocesses to stop
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  // every half minute report on RAM usage by go
  metrics.MemStatsOrDie(ctx)

  cfg := injection.ParseAndGetRESTConfigOrDie()

  log.Printf("%d clients registered", len(injection.Default.GetClients()))
  log.Printf("%d informer factories registered", len(injection.Default.GetInformerFactories()))
  log.Printf("%d informers registered", len(injection.Default.GetInformers()))

  ctx, informers := injection.Default.SetupInformers(ctx, cfg)

  var env config
  if err := envconfig.Process("", &env); err != nil {
    log.Fatal("Couldn't process env: ", err)
  }

  kubeClient := kubeclient.Get(ctx)

  // prevent termination by polling failure - sometimes startup occurs before reaching kube-api
  var err error
  if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
    if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
      log.Print("Couldn't obtain k8s version", err)
    }
    return err == nil, nil
  }); perr != nil {
    log.Fatal("Reached timeout trying to get k8s version: ", err)
  }

  // initialise logger
  loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
  if err != nil {
    log.Fatal("Cannot load/parse logging setup: ", err)
  }

  logger, atomicLevel := pkglogging.NewLoggerFromConfig(loggingConfig, component)
  logger = logger.With(
    zap.String(logkey.ControllerType, component),
    zap.String(logkey.Pod, env.PodName)
  )
  ctx = pkglogging.WithLogger(ctx, logger)
  defer flush(logger)
}
