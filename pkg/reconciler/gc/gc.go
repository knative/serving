/*
Copyright 2019 The Knative Authors.

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

package gc

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	configreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1alpha1/configuration"
	listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	spkgreconciler "knative.dev/serving/pkg/reconciler"
	configns "knative.dev/serving/pkg/reconciler/gc/config"
)

// reconciler implements controller.Reconciler for Garbage Collection resources.
type reconciler struct {
	*spkgreconciler.Base

	// listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister

	configStore spkgreconciler.ConfigStore
}

// Check that our reconciler implements configreconciler.Interface
var _ configreconciler.Interface = (*reconciler)(nil)

func (c *reconciler) ReconcileKind(ctx context.Context, config *v1alpha1.Configuration) pkgreconciler.Event {
	// TODO(n3wscott): We should not need this.
	ctx = c.configStore.ToContext(ctx)

	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)

	selector := labels.SelectorFromSet(labels.Set{serving.ConfigurationLabelKey: config.Name})
	revs, err := c.revisionLister.Revisions(config.Namespace).List(selector)
	if err != nil {
		return err
	}

	gcSkipOffset := cfg.StaleRevisionMinimumGenerations

	if gcSkipOffset >= int64(len(revs)) {
		return nil
	}

	// Sort by creation timestamp descending
	sort.Slice(revs, func(i, j int) bool {
		return revs[j].CreationTimestamp.Before(&revs[i].CreationTimestamp)
	})

	for _, rev := range revs[gcSkipOffset:] {
		if isRevisionStale(ctx, rev, config) {
			err := c.ServingClientSet.ServingV1alpha1().Revisions(rev.Namespace).Delete(rev.Name, &metav1.DeleteOptions{})
			if err != nil {
				logger.With(zap.Error(err)).Errorf("Failed to delete stale revision %q", rev.Name)
				continue
			}
		}
	}
	return nil
}

func isRevisionStale(ctx context.Context, rev *v1alpha1.Revision, config *v1alpha1.Configuration) bool {
	if config.Status.LatestReadyRevisionName == rev.Name {
		return false
	}

	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)

	curTime := time.Now()
	if rev.ObjectMeta.CreationTimestamp.Add(cfg.StaleRevisionCreateDelay).After(curTime) {
		// Revision was created sooner than staleRevisionCreateDelay. Ignore it.
		return false
	}

	lastPin, err := rev.GetLastPinned()
	if err != nil {
		if err.(v1alpha1.LastPinnedParseError).Type != v1alpha1.AnnotationParseErrorTypeMissing {
			logger.Errorw("Failed to determine revision last pinned", zap.Error(err))
		} else {
			// Revision was never pinned and its RevisionConditionReady is not true after staleRevisionCreateDelay.
			// It usually happens when ksvc was deployed with wrong configuration.
			rc := rev.Status.GetCondition(v1beta1.RevisionConditionReady)
			if rc == nil || rc.Status != corev1.ConditionTrue {
				return true
			}
		}
		return false
	}

	ret := lastPin.Add(cfg.StaleRevisionTimeout).Before(curTime)
	if ret {
		logger.Infof("Detected stale revision %v with creation time %v and lastPinned time %v.", rev.ObjectMeta.Name, rev.ObjectMeta.CreationTimestamp, lastPin)
	}
	return ret
}
