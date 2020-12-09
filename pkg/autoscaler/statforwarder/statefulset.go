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

package statforwarder

import (
	"context"

	"go.uber.org/zap"
	"knative.dev/pkg/leaderelection"
)

// StatefulSetBasedProcessor configured "processors" for each of the statefulset ordinals.
func StatefulSetBasedProcessor(ctx context.Context, f *Forwarder, accept statProcessor) error {
	id, bs, err := leaderelection.NewStatefulSetBucketAndSet(len(f.bs.BucketList()))
	if err != nil {
		return err
	}
	for _, b := range bs.Buckets() {
		n := b.Name()
		if n == id.Name() {
			f.setProcessor(n, &localProcessor{
				bkt:    n,
				logger: f.logger.With(zap.String("bucket", n)),
				accept: accept,
			})
		} else {
			f.setProcessor(n, newForwardProcessor(f.logger.With(zap.String("bucket", n)), n, "unused", n))
		}
	}
	return nil
}
