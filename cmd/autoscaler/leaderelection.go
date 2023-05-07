/*
Copyright 2022 The Knative Authors

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

package main

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/leaderelection"
	"knative.dev/pkg/reconciler"
)

type leaderAwareReconciler interface {
	reconciler.LeaderAware
	controller.Reconciler
}

// leaderAware is intended to wrap a controller.Reconciler in order to disable
// leader election. It accomplishes this by not implementing the reconciler.LeaderAware
// interface. Bucket promotion/demotion needs to be done manually
//
// The controller's reconciler needs to be set prior to calling Run
type leaderAware struct {
	reconciler leaderAwareReconciler
	enqueue    func(bkt reconciler.Bucket, key types.NamespacedName)
}

func (l *leaderAware) Reconcile(ctx context.Context, key string) error {
	return l.reconciler.Reconcile(ctx, key)
}

func setupSharedElector(ctx context.Context, controllers []*controller.Impl) (leaderelection.Elector, error) {
	reconcilers := make([]*leaderAware, 0, len(controllers))

	for _, c := range controllers {
		if r, ok := c.Reconciler.(leaderAwareReconciler); ok {
			la := &leaderAware{reconciler: r, enqueue: c.MaybeEnqueueBucketKey}
			// rewire the controller's reconciler so it isn't leader aware
			// this prevents the universal bucket from being promoted
			c.Reconciler = la
			reconcilers = append(reconcilers, la)
		}
	}

	// the elector component config on the ctx will override the queueName
	// value so we leave this empty
	queueName := ""

	// this is a noop function since we will use each controller's
	// MaybeEnqueueBucketKey when we promote buckets
	noopEnqueue := func(reconciler.Bucket, types.NamespacedName) {}

	el, err := leaderelection.BuildElector(ctx, coalesce(reconcilers), queueName, noopEnqueue)

	if err != nil {
		return nil, err
	}

	electorWithBuckets, ok := el.(leaderelection.ElectorWithInitialBuckets)
	if !ok || len(electorWithBuckets.InitialBuckets()) == 0 {
		return el, nil
	}

	for _, r := range reconcilers {
		for _, b := range electorWithBuckets.InitialBuckets() {
			// Controller hasn't started yet so need to enqueue anything
			r.reconciler.Promote(b, nil)
		}
	}
	return el, nil
}

func coalesce(reconcilers []*leaderAware) reconciler.LeaderAware {
	return &reconciler.LeaderAwareFuncs{
		PromoteFunc: func(b reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
			for _, r := range reconcilers {
				r.reconciler.Promote(b, r.enqueue)
			}
			return nil
		},
		DemoteFunc: func(b reconciler.Bucket) {
			for _, r := range reconcilers {
				r.reconciler.Demote(b)
			}
		},
	}
}
