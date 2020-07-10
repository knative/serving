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

	gcSkipOffset := cfg.StaleRevisionMinimumGenerations

	if gcSkipOffset >= int64(len(revs)) {
		return nil
	}

	// Sort by last active descending
	sort.Slice(revs, func(i, j int) bool {
		a, b := getRevisionLastActiveTime(revs[i]), getRevisionLastActiveTime(revs[j])
		return a.After(b)
	})

	for _, rev := range revs[gcSkipOffset:] {
		if isRevisionStale(ctx, rev, config) {
			err := client.ServingV1().Revisions(rev.Namespace).Delete(rev.Name, &metav1.DeleteOptions{})
			if err != nil {
				logger.With(zap.Error(err)).Errorf("Failed to delete stale revision %q", rev.Name)
				continue
			}
		}
	}
	return nil
}

func isRevisionStale(ctx context.Context, rev *v1.Revision, config *v1.Configuration) bool {
	if config.Status.LatestReadyRevisionName == rev.Name {
		return false
	}
	cfg := configns.FromContext(ctx).RevisionGC
	logger := logging.FromContext(ctx)
	curTime := time.Now()
	createTime := rev.ObjectMeta.CreationTimestamp

	if createTime.Add(cfg.StaleRevisionCreateDelay).After(curTime) {
		// Revision was created sooner than staleRevisionCreateDelay. Ignore it.
		return false
	}

	lastActive := getRevisionLastActiveTime(rev)

	// TODO(whaught): this is carried over from v1, but I'm not sure why we can't delete a ready revision
	// that isn't referenced? Maybe because of labeler failure - can we replace this with 'pending' routing state check?
	if lastActive.Equal(createTime.Time) {
		// Revision was never active and it's not ready after staleRevisionCreateDelay.
		// It usually happens when ksvc was deployed with wrong configuration.
		return !rev.Status.GetCondition(v1.RevisionConditionReady).IsTrue()
	}

	ret := lastActive.Add(cfg.StaleRevisionTimeout).Before(curTime)
	if ret {
		logger.Infof("Detected stale revision %v with creation time %v and last active time %v.",
			rev.ObjectMeta.Name, rev.ObjectMeta.CreationTimestamp, lastActive)
	}
	return ret
}

// getRevisionLastActiveTime returns if present:
// routingStateModified, then lastPinnedTime, then the created time.
// This is used for sort-ordering by most recently active.
func getRevisionLastActiveTime(rev *v1.Revision) time.Time {
	if time, empty := rev.GetRoutingStateModified(), (time.Time{}); time != empty {
		return time
	}
	if time, err := rev.GetLastPinned(); err == nil {
		return time
	}
	return rev.ObjectMeta.GetCreationTimestamp().Time
}
