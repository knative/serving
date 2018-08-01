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

	commonlogkey "github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "autoscaling-controller"
)

// RevisionSynchronizer is an interface for notifying the presence or absence of revisions.
type RevisionSynchronizer interface {
	// OnPresent is called when the given revision exists.
	OnPresent(rev *v1alpha1.Revision, logger *zap.SugaredLogger)

	// OnAbsent is called when a revision in the given namespace with the given name ceases to exist.
	OnAbsent(namespace string, name string, logger *zap.SugaredLogger)
}

// Reconciler tracks revisions and notifies a RevisionSynchronizer of their presence and absence.
type Reconciler struct {
	*controller.Base

	revisionLister listers.RevisionLister
	revSynch       RevisionSynchronizer
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController creates an autoscaling Controller.
func NewController(
	opts *controller.ReconcileOptions,
	revisionInformer servinginformers.RevisionInformer,
	revSynch RevisionSynchronizer,
	informerResyncInterval time.Duration,
) *controller.Impl {

	c := &Reconciler{
		Base:           controller.NewBase(*opts, controllerAgentName),
		revisionLister: revisionInformer.Lister(),
		revSynch:       revSynch,
	}
	impl := controller.NewImpl(c, c.Logger, "Autoscaling")

	c.Logger.Info("Setting up event handlers")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	return impl
}

// Reconcile notifies the RevisionSynchronizer of the presence or absence.
func (c *Reconciler) Reconcile(revKey string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(revKey)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", revKey, err))
		return nil
	}

	logger := loggerWithRevisionInfo(c.Logger, namespace, name)
	logger.Debug("Reconcile Revision")

	rev, err := c.revisionLister.Revisions(namespace).Get(name)
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
	return logger.With(zap.String(commonlogkey.Namespace, ns), zap.String(logkey.Revision, name))
}
