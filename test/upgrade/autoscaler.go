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

package upgrade

import (
	pkgupgrade "knative.dev/pkg/test/upgrade"
	"knative.dev/serving/pkg/apis/autoscaling"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
)

const (
	containerConcurrency = 6
	targetUtilization    = 0.7
	curPods              = 1
	targetPods           = 10
)

// AutoscaleSustainingTest checks that when traffic increases a knative app
// scales up and sustains the scale as long as the traffic sustains, despite whether
// it is switching modes between normal and panic.
func AutoscaleSustainingTest() pkgupgrade.BackgroundOperation {
	var ctx *e2e.TestContext
	var autoscaler *e2e.Autoscaler
	return pkgupgrade.NewBackgroundVerification("AutoscaleSustainingTest",
		func(c pkgupgrade.Context) {
			// Setup
			ctx = e2e.SetupSvc(c.T, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
				rtesting.WithConfigAnnotations(map[string]string{
					autoscaling.TargetBurstCapacityKey: "0", // Not let Activator in the path.
				}))
			ctx.SetLogger(c.Log.Infof)
			autoscaler = e2e.AutoscaleUpToNumPods(ctx, curPods, targetPods, false /* quick */)
		},
		func(c pkgupgrade.Context) {
			test.EnsureTearDown(c.T, ctx.Clients(), ctx.Names())
			e2e.AssertAutoscaleNoError(ctx, autoscaler)
		},
	)
}

// AutoscaleSustainingWithTBCTest checks that when traffic increases and the activator is
// in the path a knative app scales up and sustains the scale.
func AutoscaleSustainingWithTBCTest() pkgupgrade.BackgroundOperation {
	var ctx *e2e.TestContext
	var autoscaler *e2e.Autoscaler
	return pkgupgrade.NewBackgroundVerification("AutoscaleSustainingWithTBCTest",
		func(c pkgupgrade.Context) {
			// Setup
			ctx = e2e.SetupSvc(c.T, autoscaling.KPA, autoscaling.Concurrency, containerConcurrency, targetUtilization,
				rtesting.WithConfigAnnotations(map[string]string{
					autoscaling.TargetBurstCapacityKey: "-1", // Put Activator always in the path.
				}))
			ctx.SetLogger(c.Log.Infof)
			autoscaler = e2e.AutoscaleUpToNumPods(ctx, curPods, targetPods, false /* quick */)
		},
		func(c pkgupgrade.Context) {
			test.EnsureTearDown(c.T, ctx.Clients(), ctx.Names())
			e2e.AssertAutoscaleNoError(ctx, autoscaler)
		},
	)
}
