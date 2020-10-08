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
package environment

import "flag"

type Ingress struct {
	EndpointOverride string // Host to use for overriding the endpoint
	Name             string // K8s Service name acting as an ingress
	Namespace        string // K8s Service namespace acting as an ingress
}

func (i *Ingress) AddFlags(fs *flag.FlagSet) {
	fs.StringVar(&i.EndpointOverride, "env.ingress.endpoint", "",
		"Provide a static endpoint url to the ingress server used during tests.")

	fs.StringVar(&i.Name, "env.ingress.name", "",
		"Provide a k8s service name as a target ingress used during tests.")

	fs.StringVar(&i.Namespace, "env.ingress.namespace", "",
		"Provide a k8s service namespace as a target ingress used during tests.")
}
