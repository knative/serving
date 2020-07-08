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

package leaderelection

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
)

// TODO: replace this implementation with
// https://gist.github.com/vagababov/47e5f62c5865dbcc9deb1cd48655d7d2

// BucketSet includes all buckets used in StatefulSet ordinal assignment mode.
type BucketSet struct {
	size    int
	buckets []reconciler.Bucket
}

// Bucket returns the bucket for the given ordinal if the ordinal is
// in valided range.
func (bs *BucketSet) Bucket(ordinal int) (reconciler.Bucket, error) {
	if ordinal < 0 || ordinal >= bs.size {
		return &bucket{}, fmt.Errorf("%d is not in range [0, %d)", ordinal, bs.size)
	}
	return bs.buckets[ordinal], nil
}

// BucketOrdinal returns the ordinal of the bucket which has the given key.
// If no bucket has the given key, it returns an error.
func (bs *BucketSet) BucketOrdinal(nn types.NamespacedName) (int, error) {
	for i, b := range bs.buckets {
		if b.Has(nn) {
			return i, nil
		}
	}

	return 0, fmt.Errorf("bucket not found for the key: %v", nn)
}

// BuildBucketSet builds a BucketSet for the given bucket size.
func BuildBucketSet(size int) (BucketSet, error) {
	ssc, err := newStatefulSetConfig()
	if err != nil {
		return BucketSet{}, err
	}

	bs := BucketSet{
		size:    size,
		buckets: make([]reconciler.Bucket, size),
	}

	total := uint32(size)
	for i := 0; i < size; i++ {
		bs.buckets[i] = &bucket{
			// The name is the full pod DNS of the owner pod of this bucket.
			name: fmt.Sprintf("%s://%s-%d.%s.%s.svc.%s:%s", ssc.Protocol,
				ssc.StatefulSetID.ssName, i, ssc.ServiceName,
				system.Namespace(), network.GetClusterDomainName(), ssc.Port),
			index: uint32(i),
			total: total,
		}
	}

	return bs, nil
}
