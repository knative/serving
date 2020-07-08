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

type BucketSet struct {
	buckets []reconciler.Bucket
}

func (bs *BucketSet) Bucket(i int) *reconciler.Bucket {
	if i < 0 || i > len(bs.buckets) {
		return nil
	}
	return &bs.buckets[i]
}

func (bs *BucketSet) BucketOrdinal(nn types.NamespacedName) int {
	for i, b := range bs.buckets {
		if b.Has(nn) {
			return i
		}
	}

	return -1
}

func BuildBucketSet(numB uint32) (BucketSet, error) {
	ssc, err := newStatefulSetConfig()
	if err != nil {
		return BucketSet{}, err
	}

	bs := BucketSet{
		buckets: make([]reconciler.Bucket, numB),
	}

	for i := uint32(0); i < numB; i++ {
		bs.buckets[i] = &bucket{
			// The name is the full pod DNS of the owner pod of this bucket.
			name: fmt.Sprintf("%s://%s-%d.%s.%s.svc.%s:%s", ssc.Protocol,
				ssc.StatefulSetID.ssName, i, ssc.ServiceName,
				system.Namespace(), network.GetClusterDomainName(), ssc.Port),
			index: i,
			total: numB,
		}
	}

	return bs, nil
}
