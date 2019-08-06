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

package issue

import (
	"fmt"
	"strings"
)

// combineErrors combines slice of errors and return a single error
func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	var errStrs []string
	for _, err := range errs {
		errStrs = append(errStrs, err.Error())
	}
	return fmt.Errorf(strings.Join(errStrs, "\n"))
}
