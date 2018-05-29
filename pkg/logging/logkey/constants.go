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

	// Service is the key used for service name in structured logs
	Service = "elafros.dev/service"

	// Configuration is the key used for configuration name in structured logs
	Configuration = "elafros.dev/configuration"

	// Revision is the key used for revision name in structured logs
	Revision = "elafros.dev/revision"

	// Route is the key used for route name in structured logs
	Route = "elafros.dev/route"

	// Build is the key used for build name in structured logs
	Build = "elafros.dev/build"

	// JSONConfig is the key used for JSON configurations (not to be confused by the Configuration object)
	JSONConfig = "elafros.dev/jsonconfig"

	// Kind is the key used to represent kind of an object in logs
	Kind = "elafros.dev/kind"

	// Name is the key used to represent name of an object in logs
	Name = "elafros.dev/name"

	// Operation is the key used to represent an operation in logs
	Operation = "elafros.dev/operation"

	// Resource is the key used to represent a resource in logs
	Resource = "elafros.dev/resource"

	// SubResource is a generic key used to represent a sub-resource in logs
	SubResource = "elafros.dev/subresource"

	// UserInfo is the key used to represent a user information in logs
	UserInfo = "elafros.dev/userinfo"
)
