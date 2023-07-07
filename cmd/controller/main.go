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
	"flag"

	certificate "knative.dev/networking/pkg/certificates/reconciler"
	"knative.dev/pkg/reconciler"
	"knative.dev/pkg/signals"
	"knative.dev/serving/pkg/reconciler/configuration"
	"knative.dev/serving/pkg/reconciler/gc"
	"knative.dev/serving/pkg/reconciler/labeler"
	"knative.dev/serving/pkg/reconciler/nscert"
	"knative.dev/serving/pkg/reconciler/revision"
	"knative.dev/serving/pkg/reconciler/route"
	"knative.dev/serving/pkg/reconciler/serverlessservice"
	"knative.dev/serving/pkg/reconciler/service"

	// This defines the shared main for injected controllers.
	filteredFactory "knative.dev/pkg/client/injection/kube/informers/factory/filtered"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/domainmapping"
)

const (
	secretLabelNamePostfix = "-ctrl"
)

var ctors = []injection.ControllerConstructor{
	configuration.NewController,
	labeler.NewController,
	revision.NewController,
	route.NewController,
	serverlessservice.NewController,
	service.NewController,
	gc.NewController,
	nscert.NewController,
	certificate.NewControllerFactory(networking.ServingCertName),
	domainmapping.NewController,
}

func main() {
	flag.DurationVar(&reconciler.DefaultTimeout,
		"reconciliation-timeout", reconciler.DefaultTimeout,
		"The amount of time to give each reconciliation of a resource to complete before its context is canceled.")

	labelName := networking.ServingCertName + secretLabelNamePostfix
	ctx := filteredFactory.WithSelectors(signals.NewContext(), labelName)
	sharedmain.MainWithContext(ctx, "controller", ctors...)
}
