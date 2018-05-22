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

package logkey

const (
	// ControllerType is the key used for controller type in structured logs
	ControllerType = "elafros.dev/controller"

	// Namespace is the key used for namespace in structured logs
	Namespace = "elafros.dev/namespace"

	// Revision is the key used for revision name in structured logs
	Revision = "elafros.dev/revision"

	// Route is the key used for route name in structured logs
	Route = "elafros.dev/route"

	// JSONConfig is the key used for JSON configurations (not to be confused by the Configuration object)
	JSONConfig = "elafros.dev/jsonconfig"
)
