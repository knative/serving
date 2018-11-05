/*
Copyright 2018 The Knative Authors

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

package kmeta

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// DeletionHandlingAccessor tries to get runtime Object from given interface in the way of Accessor first;
// and to handle deletion, it try to fetch info from DeletedFinalStateUnknown on failure.
// The name is a reference to cache.DeletionHandlingMetaNamespaceKeyFunc
func DeletionHandlingAccessor(obj interface{}) (metav1.Object, error) {
	object, err := meta.Accessor(obj)
	if err != nil {
		// To handle obj deletion, try to fetch info from DeletedFinalStateUnknown.
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return nil, fmt.Errorf("Couldn't get object from tombstone %#v", obj)
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			return nil, fmt.Errorf("The object that Tombstone contained is not of metav1.Object %#v", obj)
		}
	}

	return object, nil
}
