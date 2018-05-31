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
package v1alpha1

import (
	internalinterfaces "github.com/knative/serving/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Configurations returns a ConfigurationInformer.
	Configurations() ConfigurationInformer
	// Revisions returns a RevisionInformer.
	Revisions() RevisionInformer
	// Routes returns a RouteInformer.
	Routes() RouteInformer
	// Services returns a ServiceInformer.
	Services() ServiceInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Configurations returns a ConfigurationInformer.
func (v *version) Configurations() ConfigurationInformer {
	return &configurationInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// Revisions returns a RevisionInformer.
func (v *version) Revisions() RevisionInformer {
	return &revisionInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// Routes returns a RouteInformer.
func (v *version) Routes() RouteInformer {
	return &routeInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}

// Services returns a ServiceInformer.
func (v *version) Services() ServiceInformer {
	return &serviceInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
