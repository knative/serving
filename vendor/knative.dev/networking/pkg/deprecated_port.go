/*
Copyright 2022 The Knative Authors

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

package pkg

import "knative.dev/networking/pkg/k8s"

// NameForPortNumber finds the name for a given port as defined by a Service.
//
// Deprecated: use knative.dev/networking/pkg/k8s.NameForPortNumber
var NameForPortNumber = k8s.NameForPortNumber

// PortNumberForName resolves a given name to a portNumber as defined by an EndpointSubset.
//
// Deprecated: use knative.dev/networking/pkg/k8s.PortNumberForName
var PortNumberForName = k8s.PortNumberForName
