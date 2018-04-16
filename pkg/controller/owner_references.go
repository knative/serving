/*
Copyright 2018 Google LLC

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

package controller

import "github.com/elafros/elafros/pkg/apis/ela/v1alpha1"

// ConfigurationControllerKind is used to create a owner reference pointing to a Configuration.
var ConfigurationControllerKind = v1alpha1.SchemeGroupVersion.WithKind("Configuration")

// LookupRevisionOwner returns the configuration that created the given revision.
func LookupRevisionOwner(revision *v1alpha1.Revision) string {
	// See if there's a 'configuration' owner reference on this object.
	for _, ownerReference := range revision.OwnerReferences {
		if ownerReference.Kind == ConfigurationControllerKind.Kind {
			return ownerReference.Name
		}
	}
	return ""
}
