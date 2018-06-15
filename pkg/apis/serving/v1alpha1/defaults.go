/*
Copyright 2018 Google LLC.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// Use these defaulting functions by calling the Default() method on the Scheme
// var in the generated clientset scheme package.
//
// Example:
//
// import (
//   "thisrepo/pkg/client/clientset/versioned/scheme"
//   "thisrepo/pkg/apis/whatever/v1"
// )
// func main() {
//   obj := &v1.SomeObject{}
//   scheme.Scheme.Default(obj)
// }

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_Revision(rev *Revision) {
	if rev.Spec.ServingState == "" {
		rev.Spec.ServingState = RevisionServingStateActive
	}
	if rev.Spec.ConcurrencyModel == "" {
		rev.Spec.ConcurrencyModel = RevisionRequestConcurrencyModelMulti
	}
}
