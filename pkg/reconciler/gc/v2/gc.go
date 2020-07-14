/*
Copyright 2020 The Knative Authors.

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

package v2

import (
	"context"
	"sort"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	"knative.dev/serving/pkg/gc"
	configns "knative.dev/serving/pkg/reconciler/gc/config"
)

// Collect deletes stale revisions if they are sufficiently old
func Collect(
	ctx context.Context,
	client clientset.Interface,
	revisionLister listers.RevisionLister,
	config *v1.Configuration) pkgreconciler.Event {
	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)

	selector := labels.SelectorFromSet(labels.Set{serving.ConfigurationLabelKey: config.Name})
	revs, err := revisionLister.Revisions(config.Namespace).List(selector)
	if err != nil {
		return err
	}

	minStale, maxStale := int(cfg.MinStaleRevisions), int(cfg.MaxStaleRevisions)
	if l := len(revs); l <= minStale || l <= maxStale {
		return nil
	}

	// Sort by last active descending
	sort.Slice(revs, func(i, j int) bool {
		a, b := revisionLastActiveTime(revs[i]), revisionLastActiveTime(revs[j])
		return a.After(b)
	})

	numStale := 0
	for total, rev := range revs {
		if isRevisionActive(rev, config) {
			continue
		}

		if isRevisionStale(cfg, rev, logger) {
			numStale++
		}

		if total > maxStale || numStale > minStale {
			err := client.ServingV1().Revisions(rev.Namespace).Delete(
				rev.Name, &metav1.DeleteOptions{})
			if err != nil {
				logger.With(
					zap.Error(err)).Errorf(
					"Failed to delete stale revision %q", rev.Name)
				continue
			}
		}
	}
	return nil
}

func isRevisionActive(rev *v1.Revision, config *v1.Configuration) bool {
	if config.Status.LatestReadyRevisionName == rev.Name {
		return false
	}
	return rev.GetRoutingState() != v1.RoutingStateReserve
}

func isRevisionStale(cfg *gc.Config, rev *v1.Revision, logger *zap.SugaredLogger) bool {
	curTime := time.Now()
	createTime := rev.ObjectMeta.CreationTimestamp
	if createTime.Add(cfg.RetainSinceCreateTime).After(curTime) {
		// Revision was created sooner than GCRetainSinceCreateTime. Ignore it.
		return false
	}

	if a := revisionLastActiveTime(rev); a.Add(cfg.RetainSinceLastActiveTime).Before(curTime) {
		logger.Infof("Detected stale revision %v with creation time %v and last active time %v.",
			rev.ObjectMeta.Name, rev.ObjectMeta.CreationTimestamp, a)
		return true
	}
	return false
}

// revisionLastActiveTime returns if present:
// routingStateModified, then lastPinnedTime, then the created time.
// This is used for sort-ordering by most recently active.
func revisionLastActiveTime(rev *v1.Revision) time.Time {
	if t := rev.GetRoutingStateModified(); !t.IsZero() {
		return t
	}
	if t, err := rev.GetLastPinned(); err == nil {
		return t
	}
	return rev.ObjectMeta.GetCreationTimestamp().Time
}
