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

package logkey

const (
	// Service is the key used for service name in structured logs
	Service = "knative.dev/service"

	// Configuration is the key used for configuration name in structured logs
	Configuration = "knative.dev/configuration"

	// Revision is the key used for revision name in structured logs
	Revision = "knative.dev/revision"

	// Route is the key used for route name in structured logs
	Route = "knative.dev/route"

	// Build is the key used for build name in structured logs
	Build = "knative.dev/build"
)
