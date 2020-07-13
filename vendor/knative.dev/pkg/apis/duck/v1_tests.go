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

package duck

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"

	"knative.dev/pkg/apis/duck/v1"
)

// Conditions is an Implementable "duck type".
var _ Implementable = (*v1.Conditions)(nil)

// In order for Conditions to be Implementable, KResource must be Populatable.
var _ Populatable = (*v1.KResource)(nil)

// Source is an Implementable "duck type".
var _ Implementable = (*v1.Source)(nil)

// Verify Source resources meet duck contracts.
var _ Populatable = (*v1.Source)(nil)

var _ Populatable = (*v1.WithPod)(nil)
var _ Implementable = (*v1.PodSpecable)(nil)

func TestV1TypesImplements(t *testing.T) {
	testCases := []struct {
		instance interface{}
		iface    Implementable
	}{
		{instance: &v1.AddressableType{}, iface: &v1.Addressable{}},
		{instance: &v1.KResource{}, iface: &v1.Conditions{}},
	}
	for _, tc := range testCases {
		if err := VerifyType(tc.instance, tc.iface); err != nil {
			t.Error(err)
		}
	}
}

func TestV1ImplementsPodSpecable(t *testing.T) {
	instances := []interface{}{
		&v1.WithPod{},
		&appsv1.ReplicaSet{},
		&appsv1.Deployment{},
		&appsv1.StatefulSet{},
		&appsv1.DaemonSet{},
		&batchv1.Job{},
	}
	for _, instance := range instances {
		if err := VerifyType(instance, &v1.PodSpecable{}); err != nil {
			t.Error(err)
		}
	}
}

// Addressable is an Implementable "duck type".
var _ Implementable = (*v1.Addressable)(nil)

// Verify AddressableType resources meet duck contracts.
var _ Populatable = (*v1.AddressableType)(nil)
