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

package nscert

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	nsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/namespace"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/client/clientset/versioned/scheme"
	servingclient "knative.dev/serving/pkg/client/injection/client"
	kcertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"

	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler/nscert/config"
)

const (
	controllerAgentName = "namespace-controller"
)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)
	nsInformer := nsinformer.Get(ctx)
	knCertificateInformer := kcertinformer.Get(ctx)

	c := &reconciler{
		client:              servingclient.Get(ctx),
		nsLister:            nsInformer.Lister(),
		knCertificateLister: knCertificateInformer.Lister(),
	}

	impl := controller.NewImpl(c, logger, "Namespace")

	logger.Info("Setting up event handlers")
	nsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: pkgreconciler.Not(pkgreconciler.LabelFilterFunc(networking.DisableWildcardCertLabelKey, "true", false)),
		Handler:    controller.HandleAll(impl.Enqueue),
	})

	knCertificateInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(corev1.SchemeGroupVersion.WithKind("Namespace")),
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&network.Config{},
		&routecfg.Domain{},
	}
	resync := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.GlobalResync(nsInformer.Informer())
	})
	configStore := config.NewStore(logger.Named("config-store"), resync)
	configStore.WatchConfigs(cmw)
	c.configStore = configStore

	// TODO(n3wscott): Drop once we can genreconcile core resources.
	logger.Debug("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	watches := []watch.Interface{
		eventBroadcaster.StartLogging(logger.Named("event-broadcaster").Infof),
		eventBroadcaster.StartRecordingToSink(
			&typedcorev1.EventSinkImpl{Interface: kubeclient.Get(ctx).CoreV1().Events("")}),
	}
	c.recorder = eventBroadcaster.NewRecorder(
		scheme.Scheme, corev1.EventSource{Component: controllerAgentName})
	go func() {
		<-ctx.Done()
		for _, w := range watches {
			w.Stop()
		}
	}()

	return impl
}
