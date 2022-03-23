/*
Copyright 2020 The Knative Authors

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

// collect deletes stale revisions if they are sufficiently old
func collect(
	ctx context.Context,
	client clientset.Interface,
	revisionLister listers.RevisionLister,
	config *v1.Configuration) pkgreconciler.Event {
	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)

	min, max := int(cfg.MinNonActiveRevisions), int(cfg.MaxNonActiveRevisions)
	if max == gc.Disabled && cfg.RetainSinceCreateTime == gc.Disabled && cfg.RetainSinceLastActiveTime == gc.Disabled {
		return nil // all deletion settings are disabled
	}

	selector := labels.SelectorFromSet(labels.Set{serving.ConfigurationLabelKey: config.Name})
	revs, err := revisionLister.Revisions(config.Namespace).List(selector)
	if err != nil {
		return err
	}
	if len(revs) <= min {
		return nil // not enough total revs
	}

	// Filter out active revs
	revs = nonactiveRevisions(revs, config)

	if len(revs) <= min {
		return nil // not enough non-active revs
	}

	// Sort by last active ascending (oldest first)
	sort.Slice(revs, func(i, j int) bool {
		a, b := revisionLastActiveTime(revs[i]), revisionLastActiveTime(revs[j])
		return a.Before(b)
	})

	count := len(revs)
	// If we need `min` to remain, this is the max count of rev can delete.
	maxIdx := len(revs) - min
	staleCount := 0
	for i := 0; i < count; i++ {
		rev := revs[i]
		if !isRevisionStale(cfg, rev, logger) {
			continue
		}
		logger.Info("Deleting stale revision: ", rev.ObjectMeta.Name)
		if err := client.ServingV1().Revisions(rev.Namespace).Delete(ctx, rev.Name, metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to GC revision: "+rev.Name, zap.Error(err))
		}
		revs[i] = nil
		staleCount++
		if staleCount >= maxIdx {
			return nil // Reaches max revs to delete
		}

	}

	nonStaleCount := count - staleCount
	if max == gc.Disabled || nonStaleCount <= max {
		return nil
	}
	needsDeleteCount := nonStaleCount - max

	// Stale revisions has been deleted, delete extra revisions past max.
	logger.Infof("Maximum number of revisions (%d) reached, deleting oldest non-active (%d) revisions",
		max, needsDeleteCount)
	deletedCount := 0
	for _, rev := range revs {
		if deletedCount == needsDeleteCount {
			break
		}
		if rev == nil {
			continue
		}
		logger.Info("Deleting non-active revision: ", rev.ObjectMeta.Name)
		if err := client.ServingV1().Revisions(rev.Namespace).Delete(ctx, rev.Name, metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to GC revision: "+rev.Name, zap.Error(err))
		}
		deletedCount++
	}
	return nil
}

// nonactiveRevisions swaps keeps only non active revisions.
func nonactiveRevisions(revs []*v1.Revision, config *v1.Configuration) []*v1.Revision {
	swap := len(revs)
	for i := 0; i < swap; {
		if isRevisionActive(revs[i], config) {
			swap--
			revs[i] = revs[swap]
		} else {
			i++
		}
	}
	return revs[:swap]
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
// routingStateModified, then the created time.
// This is used for sort-ordering by most recently active.
func revisionLastActiveTime(rev *v1.Revision) time.Time {
	if t := rev.GetRoutingStateModified(); !t.IsZero() {
		return t
	}
	return rev.ObjectMeta.GetCreationTimestamp().Time
}
