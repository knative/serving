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
	"net/http"
	"strings"

	// Load the generic flags of knative.dev/pkg too.
	_ "knative.dev/pkg/test"
)

// ServingFlags holds the flags or defaults for knative/serving settings in the user's environment.
var ServingFlags = initializeServingFlags()

// ServingEnvironmentFlags holds the e2e flags needed only by the serving repo.
type ServingEnvironmentFlags struct {
	ResolvableDomain         bool   // Resolve Route controller's `domainSuffix`
	CustomDomain             string // Indicates the `domainSuffix` for custom domain test.
	HTTPS                    bool   // Indicates where the test service will be created with https
	Buckets                  int    // The number of reconciler buckets configured.
	Replicas                 int    // The number of controlplane replicas being run.
	EnableAlphaFeatures      bool   // Indicates whether we run tests for alpha features
	EnableBetaFeatures       bool   // Indicates whether we run tests for beta features
	DisableLogStream         bool   // Indicates whether log streaming is disabled
	DisableOptionalAPI       bool   // Indicates whether to skip conformance tests against optional API
	SkipCleanupOnFail        bool   // Indicates whether to skip cleanup if test fails
	TestNamespace            string // Default namespace for Serving E2E/Conformance tests
	AltTestNamespace         string // Alternative namespace for running cross-namespace tests in
	TLSTestNamespace         string // Namespace for Serving TLS tests
	ExceedingMemoryLimitSize int    // Memory size used to trigger a non-200 response when the service is set with 300MB memory limit.
	RequestHeaders           string // Extra HTTP request headers sent to the testing deployed KServices.
	IngressClass             string // Ingress class used for serving.
	CustomMemoryRequests     string // Memory requests used for services with a specific size.
	CustomMemoryLimits       string // Memory limits used for services with a specific size.
	CustomCPURequests        string // CPU requests used for services with a specific size.
	CustomCPULimits          string // CPU limits used for services with a specific size.
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

	flag.BoolVar(&f.DisableOptionalAPI, "disable-optional-api", false,
		"Set this flag to skip conformance tests against optional API.")

	flag.BoolVar(&f.SkipCleanupOnFail, "skip-cleanup-on-fail", false, "Set this flag to skip cleanup if test fails.")

	flag.StringVar(&f.TestNamespace, "test-namespace", "serving-tests",
		"Set this flag to change the default namespace for running tests.")

	flag.StringVar(&f.AltTestNamespace, "alt-test-namespace", "serving-tests-alt",
		"Set this flag to change the alternative namespace for running tests.")

	flag.StringVar(&f.TLSTestNamespace, "tls-test-namespace", "tls",
		"Set this flag to change the namespace for running TLS tests.")

	flag.IntVar(&f.ExceedingMemoryLimitSize, "exceeding-memory-limit-size", 500,
		"Set this flag to the MB of memory consumed by your service in resource limit tests. "+
			"You service is set with 300 MB memory limit and shoud return a non-200 response when consuming such amount of memory.")

	flag.StringVar(&f.RequestHeaders, "request-headers", "",
		"Set this flag to add extra HTTP request headers sent to the testing deployed KServices. "+
			"Format: -request-headers=key1,value1,key2,value2")

	flag.StringVar(&f.IngressClass, "ingress-class", "",
		"Set the ingress class in use to this flag in order to skip non-compatible test")

	flag.StringVar(&f.CustomMemoryRequests, "custom-memory-requests", "",
		"Set this flag to the custom memory request for tests with specific memory request values."+
			"This should differ from what is used as default. The flag accepts a value acceptable to resource.MustParse.")

	flag.StringVar(&f.CustomMemoryLimits, "custom-memory-limits", "",
		"Set this flag to the custom memory limit for tests with specific memory limit values."+
			"This should differ from what is used as default. The flag accepts a value acceptable to resource.MustParse.")

	flag.StringVar(&f.CustomCPURequests, "custom-cpu-requests", "",
		"Set this flag to the custom cpu request for tests with specific cpu request values."+
			"This should differ from what is used as default. The flag accepts a value acceptable to resource.MustParse.")

	flag.StringVar(&f.CustomCPULimits, "custom-cpu-limits", "",
		"Set this flag to the custom cpu limit for tests with specific cpu limit values."+
			"This should differ from what is used as default. The flag accepts a value acceptable to resource.MustParse.")
	return &f
}

// RequestHeader returns a http.Header object including key-value header pairs passed via testing flag.
func (f *ServingEnvironmentFlags) RequestHeader() http.Header {
	header := make(http.Header)

	if f.RequestHeaders != "" {
		headers := strings.Split(f.RequestHeaders, ",")
		if len(headers)%2 != 0 {
			panic("incorrect input of request headers: " + f.RequestHeaders)
		}

		for i := 0; i < len(headers); i += 2 {
			header.Add(headers[i], headers[i+1])
		}
	}

	return header
}
