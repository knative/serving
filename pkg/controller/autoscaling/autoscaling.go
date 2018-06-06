/*
Copyright 2018 Google LLC

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
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
)

const (
	controllerAgentName = "autoscaling-controller"
)

// scalerRunner wraps an autoscaler.Autoscaler and a CancelFunc for
// implementing shutdown behavior.
type scalerRunner struct {
	// scaler is the wrapped autoscaling instance.
	scaler *autoscaler.Autoscaler
	// stopCh is a channel used to stop the associated goroutine.
	stopCh chan struct{}
}

// Controller implements the autoscaling controller for Revision resources.
// +controller:group=ela,version=v1alpha1,kind=Revision,resource=revisions
type Controller struct {
	*controller.Base

	// lister indexes properties about Revision
	lister listers.RevisionLister
	synced cache.InformerSynced

	// scalers contains the scaler instances for each autoscaled revision.
	scalers       map[string]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh chan struct{}

	//controllerConfig includes the configurations for the controller.
	controllerConfig *ControllerConfig
}

// ControllerConfig includes the configurations for the controller.
type ControllerConfig struct {
	EnableScaleToZero       bool
	MultiConcurrencyTarget  float64
	SingleConcurrencyTarget float64
	AutoscalerTickInterval  time.Duration
	AutoscalerConfig        autoscaler.Config
}

// NewController initializes the controller. This returns *Controller instead of
// controller.Interface because the return value must expose the StatsHandler
// function.
func NewController(
	kubeClientSet kubernetes.Interface,
	servingClientSet clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	servingInformerFactory informers.SharedInformerFactory,
	config *rest.Config,
	controllerConfig *ControllerConfig,
	logger *zap.SugaredLogger) *Controller {

	// obtain references to a shared index informer for the Revision.
	informer := servingInformerFactory.Serving().V1alpha1().Revisions()

	controller := &Controller{
		Base: controller.NewBase(
			kubeClientSet,
			servingClientSet,
			kubeInformerFactory,
			servingInformerFactory,
			informer.Informer(),
			controllerAgentName,
			"Autoscaling",
			logger,
		),
		lister:           informer.Lister(),
		synced:           informer.Informer().HasSynced,
		scalers:          make(map[string]*scalerRunner),
		scalersStopCh:    make(chan struct{}),
		controllerConfig: controllerConfig,
	}

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	err := c.RunController(threadiness, stopCh, []cache.InformerSynced{c.synced},
		c.syncHandler, "Autoscaling")
	close(c.scalersStopCh)

	return err
}

// loggerWithRevisionInfo enriches the logs with revision name and namespace.
func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(logkey.Namespace, ns), zap.String(logkey.Revision, name))
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	logger := loggerWithRevisionInfo(c.Logger, namespace, name)
	ctx := logging.WithLogger(context.TODO(), logger)
	logger.Info("Running reconcile Revision")

	// Get the Revision resource with this namespace/name
	rev, err := c.lister.Revisions(namespace).Get(name)
	if err != nil {
		// The revision may no longer exist, in which case we stop
		// processing and delete its scaler instance.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("revision %q in work queue no longer exists", key))
			c.scalersMutex.Lock()
			if scaler, exists := c.scalers[key]; exists {
				close(scaler.stopCh)
				delete(c.scalers, key)
				logger.Info("Deleted scaler for revision.")
			}
			c.scalersMutex.Unlock()
		}
		return err
	}

	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	c.scalersMutex.Lock()
	defer c.scalersMutex.Unlock()
	if _, exists := c.scalers[key]; !exists {
		scaler, err := c.createScaler(ctx, rev)
		logger.Info("Created scaler for revision.")
		if err != nil {
			return err
		}
		c.scalers[key] = scaler
	}

	return nil
}

func (c *Controller) createScaler(ctx context.Context, rev *v1alpha1.Revision) (*scalerRunner, error) {
	// If the revision has no controller, use the empty string as the controller
	// name.
	var controllerName string
	if controller := metav1.GetControllerOf(rev); controller != nil {
		controllerName = controller.Name
	}

	reporter, err := autoscaler.NewStatsReporter(rev.Namespace, controllerName, rev.Name)
	if err != nil {
		return nil, err
	}

	//TODO take concurrency target from Rev.Spec.concurrencyModel
	var targetConcurrency float64
	switch rev.Spec.ConcurrencyModel {
	case v1alpha1.RevisionRequestConcurrencyModelSingle:
		targetConcurrency = c.controllerConfig.SingleConcurrencyTarget
	case v1alpha1.RevisionRequestConcurrencyModelMulti:
		targetConcurrency = c.controllerConfig.MultiConcurrencyTarget
	default:
		return nil, fmt.Errorf("unknown ConcurrencyModel %q", rev.Spec.ConcurrencyModel)
	}

	config := autoscaler.Config{
		TargetConcurrency:    targetConcurrency,
		MaxScaleUpRate:       c.controllerConfig.AutoscalerConfig.MaxScaleUpRate,
		StableWindow:         c.controllerConfig.AutoscalerConfig.StableWindow,
		PanicWindow:          c.controllerConfig.AutoscalerConfig.PanicWindow,
		ScaleToZeroThreshold: c.controllerConfig.AutoscalerConfig.ScaleToZeroThreshold,
	}
	scaler := autoscaler.NewAutoscaler(config, reporter)
	stopCh := make(chan struct{})
	runner := &scalerRunner{scaler: scaler, stopCh: stopCh}

	ticker := time.NewTicker(c.controllerConfig.AutoscalerTickInterval)

	go func() {
		for {
			select {
			case <-c.scalersStopCh:
				ticker.Stop()
				return
			case <-stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				c.tickScaler(ctx, rev, scaler)
			}
		}
	}()

	return runner, nil
}

func (c *Controller) tickScaler(ctx context.Context, rev *v1alpha1.Revision, scaler *autoscaler.Autoscaler) {
	logger := logging.FromContext(ctx)
	desiredScale, scaled := scaler.Scale(ctx, time.Now())

	if scaled {
		// Cannot scale negative.
		if desiredScale < 0 {
			logger.Errorf("Cannot scale: desiredScale %d < 0.", desiredScale)
			return
		}

		// Don't scale to zero if scale to zero is disabled.
		if desiredScale == 0 && !c.controllerConfig.EnableScaleToZero {
			logger.Error("Cannot scale: Desired scale == 0 && EnableScaleToZero == false.")
			return
		}

		// Get the revision's deployment.
		//TODO scale the revision's scaleTargetRef
		deploymentName := controller.GetRevisionDeploymentName(rev)
		deployment, err := c.KubeClientSet.AppsV1().Deployments(rev.Namespace).Get(deploymentName, metav1.GetOptions{})
		if err != nil {
			logger.Error("Error getting deployment.", zap.String("deployment", deploymentName), zap.Error(err))
			return
		}
		// Don't scale if current scale is zero. Rely on the activator to scale
		// from zero.
		if *deployment.Spec.Replicas == 0 {
			logger.Info("Cannot scale: Current scale is 0; activator must scale from 0.")
			return
		}

		// Scale the deployment.
		deployment.Spec.Replicas = &desiredScale
		_, err = c.KubeClientSet.AppsV1().Deployments(rev.Namespace).Update(deployment)
		if err != nil {
			logger.Error("Error scaling deployment.", zap.String("deployment", deploymentName), zap.Error(err))
			return
		}

		// When scaling to zero, also flip the revision's ServingState to Reserve.
		if desiredScale == 0 {
			if _, err := c.updateRevServingState(ctx, rev, v1alpha1.RevisionServingStateReserve); err != nil {
				logger.Error("Error updating revision serving state.", zap.Error(err))
				return
			}
		}
	}
}

func (c *Controller) updateRevServingState(ctx context.Context, rev *v1alpha1.Revision, state v1alpha1.RevisionServingStateType) (*v1alpha1.Revision, error) {
	revClient := c.ElaClientSet.ServingV1alpha1().Revisions(rev.Namespace)
	newRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newRev.Spec.ServingState = state
	return revClient.Update(newRev)
}

// StatsHandler exposes a websocket handler for receiving stats from queue
// sidecar containers.
func (c *Controller) StatsHandler(w http.ResponseWriter, r *http.Request) {
	var upgrader websocket.Upgrader
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		c.Logger.Error("Error upgrading websocket.", zap.Error(err))
		return
	}

	go func() {
		<-c.scalersStopCh
		// Send a close message to tell the client to immediately reconnect
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(1012, "Restarting"))
		conn.Close()
	}()

	for {
		messageType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			c.Logger.Error("Dropping non-binary message.")
			continue
		}
		dec := gob.NewDecoder(bytes.NewBuffer(msg))
		var sm autoscaler.StatMessage
		err = dec.Decode(&sm)
		if err != nil {
			c.Logger.Error(err)
			continue
		}

		c.RecordStat(sm.RevisionKey, sm.Stat)
	}
}

// RecordStat records a stat for the given revision. revKey should have the
// form namespace/name.
func (c *Controller) RecordStat(revKey string, stat autoscaler.Stat) {
	c.scalersMutex.RLock()
	defer c.scalersMutex.RUnlock()
	scaler, exists := c.scalers[revKey]
	if exists {
		logger := c.Logger
		namespace, name, err := cache.SplitMetaNamespaceKey(revKey)
		if err != nil {
			logger = loggerWithRevisionInfo(c.Logger, namespace, name)
		}
		ctx := logging.WithLogger(context.TODO(), logger)
		scaler.scaler.Record(ctx, stat)
	}
}
