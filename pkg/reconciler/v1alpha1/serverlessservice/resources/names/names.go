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

package names

import netv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"

// PublicService returns the public service name for the `s`.
func PublicService(s *netv1alpha1.ServerlessService) string {
	// TODO(vagababov): evolve this use generateName (for #3236).
	// TODO(vagababov): remove "-pub", once revision stops creating the service.
	return s.Name + "-pub"
}

// PrivateService returns the private service name for the `s`.
func PrivateService(s *netv1alpha1.ServerlessService) string {
	// TODO(vagababov): evolve this use generateName (for #3236).
	return s.Name + "-priv"
}
