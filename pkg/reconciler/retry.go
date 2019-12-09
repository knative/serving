/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconciler

import apierrs "k8s.io/apimachinery/pkg/api/errors"

// RetryUpdateConflicts retries the inner function if it returns conflict errors.
// This can be used to retry status updates without constantly reenqueuing keys.
func RetryUpdateConflicts(updater func(int) error) error {
	var err error
	for attempts := 0; attempts < 4; attempts++ {
		err = updater(attempts)
		if err == nil || !apierrs.IsConflict(err) {
			return err
		}
	}
	return err
}
