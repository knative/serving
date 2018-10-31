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

package autoscaling

import "strings"

// Inherit copies autoscaling annotations from map `from` into map `to`.
func Inherit(to, from map[string]string) map[string]string {
	for _, prefix := range []string{
		GroupName,
		InternalGroupName,
	} {
		for k, v := range from {
			if strings.HasPrefix(k, prefix) {
				if to == nil {
					to = make(map[string]string)
				}
				to[k] = v
			}
		}
	}
	return to
}

// IsInheritedEqual returns `true` if any autoscaling annotations need to
// be copied from map `from` into map `to`.
func IsInheritedEqual(to, from map[string]string) bool {
	for _, prefix := range []string{
		GroupName,
		InternalGroupName,
	} {
		for k, v := range from {
			if strings.HasPrefix(k, prefix) {
				if value, ok := to[k]; ok {
					if value != v {
						return false
					}
				} else {
					return false
				}
			}
		}
	}
	return true
}
