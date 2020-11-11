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

	// Delete stale revisions while more than min remain, swap nonstale revisions to the end.
	swap := len(revs)

	// If we need `min` to remain, this is the max index i can reach.
	maxIdx := len(revs) - min
	for i := 0; i < swap; {
		rev := revs[i]
		switch {
		case i >= maxIdx:
			return nil
		case isRevisionStale(cfg, rev, logger):
			i++
			logger.Info("Deleting stale revision: ", rev.ObjectMeta.Name)
			if err := client.ServingV1().Revisions(rev.Namespace).Delete(ctx, rev.Name, metav1.DeleteOptions{}); err != nil {
				logger.Errorw("Failed to GC revision: "+rev.Name, zap.Error(err))
			}
		default:
			swap--
			revs[i], revs[swap] = revs[swap], revs[i]
		}
	}
	revs = revs[swap:] // Reslice to include the nonstale revisions, which are now in reverse order

	if max == gc.Disabled || len(revs) <= max {
		return nil
	}

	// Delete extra revisions past max.
	logger.Infof("Maximum number of revisions (%d) reached, deleting oldest non-active (%d) revisions",
		max, len(revs)-max)
	for _, rev := range revs[max:] {
		logger.Info("Deleting non-active revision: ", rev.ObjectMeta.Name)
		if err := client.ServingV1().Revisions(rev.Namespace).Delete(ctx, rev.Name, metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to GC revision: "+rev.Name, zap.Error(err))
		}
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
