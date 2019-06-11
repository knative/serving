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

package main

import (
	// The set of controllers this controller process runs.
	"github.com/knative/serving/pkg/reconciler/configuration"
	"github.com/knative/serving/pkg/reconciler/labeler"
	"github.com/knative/serving/pkg/reconciler/revision"
	"github.com/knative/serving/pkg/reconciler/route"
	"github.com/knative/serving/pkg/reconciler/serverlessservice"
	"github.com/knative/serving/pkg/reconciler/service"

	// This defines the shared main for injected controllers.
	"github.com/knative/pkg/injection/sharedmain"
)

func main() {
	sharedmain.Main("controller",
		configuration.NewController,
		labeler.NewRouteToConfigurationController,
		revision.NewController,
		route.NewController,
		serverlessservice.NewController,
		service.NewController,
	)
}
