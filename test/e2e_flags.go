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

// This file contains logic to encapsulate flags which are needed to specify
// what cluster, etc. to use for e2e tests.

package test

import (
	"flag"

	// Load the generic flags of knative.dev/pkg too.
	_ "knative.dev/pkg/test"
)

// ServingFlags holds the flags or defaults for knative/serving settings in the user's environment.
var ServingFlags = initializeServingFlags()

// ServingEnvironmentFlags holds the e2e flags needed only by the serving repo.
type ServingEnvironmentFlags struct {
	ResolvableDomain    bool   // Resolve Route controller's `domainSuffix`
	CustomDomain        string // Indicates the `domainSuffix` for custom domain test.
	HTTPS               bool   // Indicates where the test service will be created with https
	Buckets             int    // The number of reconciler buckets configured.
	Replicas            int    // The number of controlplane replicas being run.
	EnableAlphaFeatures bool   // Indicates whether we run tests for alpha features
	EnableBetaFeatures  bool   // Indicates whether we run tests for beta features
	DisableLogStream    bool   // Indicates whether log streaming is disabled
}

func initializeServingFlags() *ServingEnvironmentFlags {
	var f ServingEnvironmentFlags

	flag.BoolVar(&f.ResolvableDomain, "resolvabledomain", false,
		"Set this flag to true if you have configured the `domainSuffix` on your Route controller to a domain that will resolve to your test cluster.")

	flag.StringVar(&f.CustomDomain, "customdomain", "",
		"Set this flag to the custom domain suffix for domainmapping test.")

	flag.BoolVar(&f.HTTPS, "https", false,
		"Set this flag to true to run all tests with https.")

	flag.IntVar(&f.Buckets, "buckets", 1,
		"Set this flag to the number of reconciler buckets configured.")

	flag.IntVar(&f.Replicas, "replicas", 1,
		"Set this flag to the number of controlplane replicas being run.")

	flag.BoolVar(&f.EnableAlphaFeatures, "enable-alpha", false,
		"Set this flag to run tests against alpha features")

	flag.BoolVar(&f.EnableBetaFeatures, "enable-beta", false,
		"Set this flag to run tests against beta features")

	flag.BoolVar(&f.DisableLogStream, "disable-logstream", false,
		"Set this flag to disable streaming logs from system components")

	return &f
}
