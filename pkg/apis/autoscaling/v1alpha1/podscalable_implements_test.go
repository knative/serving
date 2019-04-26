/*
Copyright 2019 The Knative Authors

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

package v1alpha1

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/knative/pkg/apis/duck"
)

func TestImplementsPodScalable(t *testing.T) {
	instances := []interface{}{
		&PodScalable{},
		&appsv1.ReplicaSet{},
		&appsv1.Deployment{},
		&appsv1.StatefulSet{},
	}
	for _, instance := range instances {
		if err := duck.VerifyType(instance, &PodScalable{}); err != nil {
			t.Error(err)
		}
	}
}
