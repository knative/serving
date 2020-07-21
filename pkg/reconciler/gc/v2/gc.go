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
	"strings"
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

	min, max := int(cfg.MinNonActiveRevisions), int(cfg.MaxNonActiveRevisions)
	if len(revs) <= min ||
		max == gc.Disabled &&
			cfg.RetainSinceCreateTime == gc.Disabled &&
			cfg.RetainSinceLastActiveTime == gc.Disabled {
		return nil
	}

	// filter out active revs
	revs = nonactiveRevisions(revs, config)

	// Sort by last active descending
	sort.Slice(revs, func(i, j int) bool {
		a, b := revisionLastActiveTime(revs[i]), revisionLastActiveTime(revs[j])
		return a.After(b)
	})

	for i, rev := range revs {
		if max != gc.Disabled && i >= max {
			logger.Infof("Maximum(%d) reached. Deleting oldest non-active revision %q",
				max, rev.ObjectMeta.Name)
		} else if isRevisionStale(cfg, rev, logger) && i >= min {
			logger.Info("Deleting stale revision: ", rev.ObjectMeta.Name)
		} else {
			continue
		}

		if err := client.ServingV1().Revisions(rev.Namespace).Delete(
			rev.Name, &metav1.DeleteOptions{}); err != nil {
			logger.With(zap.Error(err)).Error("Failed to GC revision: ", rev.Name)
			continue
		}
	}
	return nil
}

func nonactiveRevisions(revs []*v1.Revision, config *v1.Configuration) []*v1.Revision {
	nonactiverevs := make([]*v1.Revision, 0, len(revs))
	for _, rev := range revs {
		if !isRevisionActive(rev, config) {
			nonactiverevs = append(nonactiverevs, rev)
		}
	}
	return nonactiverevs
}

func isRevisionActive(rev *v1.Revision, config *v1.Configuration) bool {
	if config.Status.LatestReadyRevisionName == rev.Name {
		return true // never delete latest ready, even if config is not active.
	}

	if strings.EqualFold(rev.Annotations[serving.RevisionPreservedAnnotationKey], "true") {
		return true
	}
	// Anything that the labeler hasn't explicitly labelled as inactive.
	// Revisions which do not yet have any annotation are not eligible for deletion.
	return rev.GetRoutingState() != v1.RoutingStateReserve
}

func isRevisionStale(cfg *gc.Config, rev *v1.Revision, logger *zap.SugaredLogger) bool {
	sinceCreate, sinceActive := cfg.RetainSinceCreateTime, cfg.RetainSinceLastActiveTime
	if sinceCreate == gc.Disabled && sinceActive == gc.Disabled {
		return false // Time checks are both disabled. Not stale.
	}

	createTime := rev.ObjectMeta.CreationTimestamp.Time
	if sinceCreate != gc.Disabled && time.Since(createTime) < sinceCreate {
		return false // Revision was created sooner than RetainSinceCreateTime. Not stale.
	}

	active := revisionLastActiveTime(rev)
	if sinceActive != gc.Disabled && time.Since(active) < sinceActive {
		return false // Revision was recently active. Not stale.
	}

	logger.Infof("Detected stale revision %q with creation time %v and last active time %v.",
		rev.ObjectMeta.Name, createTime, active)
	return true
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
