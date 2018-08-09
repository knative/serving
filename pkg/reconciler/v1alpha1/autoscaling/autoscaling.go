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
	"context"
	"fmt"
	"time"

	"github.com/knative/pkg/controller"
	commonlogkey "github.com/knative/pkg/logging/logkey"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/logging/logkey"
	"github.com/knative/serving/pkg/reconciler"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "autoscaling-controller"
)

// KPASynchronizer is an interface for notifying the presence or absence of KPAs.
type KPASynchronizer interface {
	// OnPresent is called when the given KPA exists.
	OnPresent(kpa *kpa.PodAutoscaler, logger *zap.SugaredLogger)

	// OnAbsent is called when a KPA in the given namespace with the given name ceases to exist.
	OnAbsent(namespace string, name string, logger *zap.SugaredLogger)
}

// Reconciler tracks revisions and notifies a KPASynchronizer of their presence and absence.
// TODO(mattmoor): Migrate this to track KPAs once we're creating them in
// the Revision reconciler.
type Reconciler struct {
	*reconciler.Base

	revisionLister listers.RevisionLister
	kpaSynch       KPASynchronizer
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController creates an autoscaling Controller.
func NewController(
	opts *reconciler.Options,
	revisionInformer servinginformers.RevisionInformer,
	kpaSynch KPASynchronizer,
	informerResyncInterval time.Duration,
) *controller.Impl {

	c := &Reconciler{
		Base:           reconciler.NewBase(*opts, controllerAgentName),
		revisionLister: revisionInformer.Lister(),
		kpaSynch:       kpaSynch,
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

// Reconcile notifies the KPASynchronizer of the presence or absence.
func (c *Reconciler) Reconcile(ctx context.Context, revKey string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(revKey)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", revKey, err))
		return nil
	}

	logger := loggerWithKPAInfo(c.Logger, namespace, name)
	logger.Debug("Reconcile Revision")

	rev, err := c.revisionLister.Revisions(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debug("Revision no longer exists")
			// This necessitates that the KPA has the same name as the Revision (for now).
			c.kpaSynch.OnAbsent(namespace, name, logger)
			return nil
		}
		runtime.HandleError(err)
		return err
	}

	logger.Debug("Revision exists")
	kpa := revisionresources.MakeKPA(rev)

	c.kpaSynch.OnPresent(kpa, logger)

	// TODO(mattmoor): We need to reflect the current state of activity
	// back to the KPA via status.

	return nil
}

func loggerWithKPAInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(commonlogkey.Namespace, ns), zap.String(logkey.KPA, name))
}
