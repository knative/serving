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

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/hash"
	"knative.dev/pkg/network"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
)

// ControllerOrdinal tries to get ordinal from the pod name of a StatefulSet,
// which is provided from the environment variable CONTROLLER_ORDINAL.
func ControllerOrdinal() (int, error) {
	ssc, err := newStatefulSetConfig()
	if err != nil {
		return 0, err
	}
	return ssc.StatefulSetID.ordinal, nil
}

// NewStatefulSetBucketAndSet creates a BucketSet for StatefulSet controller with
// the given bucket size and the information from environment variables. Then uses
// the created BucketSet to create a Bucket for this StatefulSet Pod.
func NewStatefulSetBucketAndSet(buckets int) (reconciler.Bucket, *hash.BucketSet, error) {
	ssc, err := newStatefulSetConfig()
	if err != nil {
		return nil, nil, err
	}

	if ssc.StatefulSetID.ordinal >= buckets {
		return nil, nil, fmt.Errorf("ordinal %d is out of range [0, %d)",
			ssc.StatefulSetID.ordinal, buckets)
	}

	names := sets.String{}
	for i := 0; i < buckets; i++ {
		names.Insert(statefulSetPodDNS(i, ssc))
	}

	bs := hash.NewBucketSet(names)
	return hash.NewBucket(statefulSetPodDNS(ssc.StatefulSetID.ordinal, ssc), bs), bs, nil
}

func statefulSetPodDNS(ordinal int, ssc *statefulSetConfig) string {
	return fmt.Sprintf("%s://%s-%d.%s.%s.svc.%s:%s", ssc.Protocol,
		ssc.StatefulSetID.ssName, ordinal, ssc.ServiceName,
		system.Namespace(), network.GetClusterDomainName(), ssc.Port)
}
