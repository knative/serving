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

func setupSharedElector(ctx context.Context, controllers []*controller.Impl) (leaderelection.Elector, error) {
	var (
		// the elector component config on the ctx will override this value
		// so we leave this empty
		queueName string

		// we leave this 'nil' since since we will use each controller's
		// MaybeEnqueueBucketKey when we promote buckets
		enq func(reconciler.Bucket, types.NamespacedName)
	)

	el, err := leaderelection.BuildElector(ctx, leaderAware(controllers), queueName, enq)

	if err != nil {
		return nil, err
	}

	electorWithBuckets, ok := el.(leaderelection.ElectorWithInitialBuckets)
	if !ok || len(electorWithBuckets.InitialBuckets()) == 0 {
		return el, nil
	}

	for _, c := range controllers {
		r, ok := c.Reconciler.(reconciler.LeaderAware)
		if !ok {
			continue
		}
		for _, b := range electorWithBuckets.InitialBuckets() {
			// Controller hasn't started yet so need to enqueue anything
			r.Promote(b, nil)
		}
	}
	return el, nil
}

func leaderAware(controllers []*controller.Impl) reconciler.LeaderAware {
	return &reconciler.LeaderAwareFuncs{
		PromoteFunc: func(b reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
			for _, c := range controllers {
				if r, ok := c.Reconciler.(reconciler.LeaderAware); ok {
					c := c
					r.Promote(b, c.MaybeEnqueueBucketKey)
				}
			}
			return nil
		},
		DemoteFunc: func(b reconciler.Bucket) {
			for _, c := range controllers {
				if r, ok := c.Reconciler.(reconciler.LeaderAware); ok {
					r.Demote(b)
				}
			}
		},
	}
}
