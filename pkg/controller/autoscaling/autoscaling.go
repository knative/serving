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

package autoscaling

import (
	"fmt"
	"time"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerName      = "Autoscaling"
	controllerAgentName = "autoscaling-controller"
)

// RevisionSynchronizer is an interface for notifying the presence or absence of revisions.
type RevisionSynchronizer interface {
	// OnPresent is called when the given revision exists.
	OnPresent(rev *v1alpha1.Revision, logger *zap.SugaredLogger)

	// OnAbsent is called when a revision in the given namespace with the given name ceases to exist.
	OnAbsent(namespace string, name string, logger *zap.SugaredLogger)
}

// Controller tracks revisions and notifies a RevisionSynchronizer of their presence and absence.
type Controller struct {
	*controller.Base
	revSynch               RevisionSynchronizer
	kubeInformerFactory    kubeinformers.SharedInformerFactory
	servingInformerFactory informers.SharedInformerFactory
	sharedRevisionInformer servinginformers.RevisionInformer
	lister                 listers.RevisionLister
	logger                 *zap.SugaredLogger
}

// NewController creates an autoscaling Controller.
func NewController(
	opts *controller.Options,
	revSynch RevisionSynchronizer,
	informerResyncInterval time.Duration) *Controller {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(opts.KubeClientSet, informerResyncInterval)
	servingInformerFactory := informers.NewSharedInformerFactory(opts.ServingClientSet, informerResyncInterval)

	sharedRevisionInformer := servingInformerFactory.Serving().V1alpha1().Revisions()

	c := Controller{
		Base: controller.NewBase(*opts,
			controllerAgentName,
			controllerName,
		),
		revSynch:               revSynch,
		kubeInformerFactory:    kubeInformerFactory,
		servingInformerFactory: servingInformerFactory,
		sharedRevisionInformer: sharedRevisionInformer,
		lister:                 sharedRevisionInformer.Lister(),
		logger:                 opts.Logger,
	}

	opts.Logger.Debugf("NewController returning controller %#v", c)
	return &c
}

// Run starts the Controller monitoring revisions. The Controller uses numThreads goroutines for
// monitoring and blocks until stopCh is closed, at which point it terminates gracefully
// and returns.
func (c *Controller) Run(numThreads int, stopCh <-chan struct{}) error {
	c.logger.Info("Starting revision informer")
	go c.servingInformerFactory.Start(stopCh)

	c.logger.Info("Waiting for revision informer cache to sync")
	informer := c.sharedRevisionInformer.Informer()
	if ok := cache.WaitForCacheSync(stopCh, informer.HasSynced); !ok {
		c.logger.Fatalf("failed to wait for revision informer cache to sync")
	}

	c.logger.Info("Setting up event handlers")
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.Enqueue,
		UpdateFunc: controller.PassNew(c.Enqueue),
		DeleteFunc: c.Enqueue,
	})

	c.logger.Info("Launching controller worker threads")
	return c.RunController(numThreads, stopCh, c.Reconcile, controllerName)
}

// Reconcile notifies the RevisionSynchronizer of the presence or absence.
func (c *Controller) Reconcile(revKey string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(revKey)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", revKey, err))
		return nil
	}

	logger := loggerWithRevisionInfo(c.logger, namespace, name)
	logger.Debug("Reconcile Revision")

	rev, err := c.lister.Revisions(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug("Revision no longer exists")
			c.revSynch.OnAbsent(namespace, name, logger)
			return nil
		}
		runtime.HandleError(err)
		return err
	}

	logger.Debug("Revision exists")
	c.revSynch.OnPresent(rev.DeepCopy(), logger)

	return nil
}

func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Revision, name))
}
