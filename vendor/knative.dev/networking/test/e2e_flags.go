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

	"knative.dev/networking/pkg/config"
)

// NetworkingFlags holds the flags or defaults for knative/networking settings in the user's environment.
var NetworkingFlags = initializeNetworkingFlags()

// ServingFlags is an alias of NetworkingFlags.
// TODO: Delete this variable once all downstream migrate it to NetworkingFlags.
var ServingFlags = NetworkingFlags

// NetworkingEnvironmentFlags holds the e2e flags needed only by the networking repo.
type NetworkingEnvironmentFlags struct {
	ResolvableDomain    bool   // Resolve Route controller's `domainSuffix`
	HTTPS               bool   // Indicates where the test service will be created with https
	IngressClass        string // Indicates the class of Ingress provider to test.
	CertificateClass    string // Indicates the class of Certificate provider to test.
	Buckets             int    // The number of reconciler buckets configured.
	Replicas            int    // The number of controlplane replicas being run.
	EnableAlphaFeatures bool   // Indicates whether we run tests for alpha features
	EnableBetaFeatures  bool   // Indicates whether we run tests for beta features
	SkipTests           string // Indicates the test names we want to skip in alpha or beta features.
	ClusterSuffix       string // Specifies the cluster DNS suffix to be used in tests.
}

func initializeNetworkingFlags() *NetworkingEnvironmentFlags {
	var f NetworkingEnvironmentFlags

	// Only define and set flags here. Flag values cannot be read at package init time.
	flag.BoolVar(&f.ResolvableDomain,
		"resolvabledomain",
		false,
		"Set this flag to true if you have configured the `domainSuffix` on your Route controller to a domain that will resolve to your test cluster.")

	flag.BoolVar(&f.HTTPS,
		"https",
		false,
		"Set this flag to true to run all tests with https.")

	flag.StringVar(&f.IngressClass,
		"ingressClass",
		config.IstioIngressClassName,
		"Set this flag to the ingress class to test against.")

	flag.StringVar(&f.CertificateClass,
		"certificateClass",
		config.CertManagerCertificateClassName,
		"Set this flag to the certificate class to test against.")

	flag.IntVar(&f.Buckets,
		"buckets",
		1,
		"Set this flag to the number of reconciler buckets configured.")

	flag.IntVar(&f.Replicas,
		"replicas",
		1,
		"Set this flag to the number of controlplane replicas being run.")

	flag.BoolVar(&f.EnableAlphaFeatures,
		"enable-alpha",
		false,
		"Set this flag to run tests against alpha features.")

	flag.BoolVar(&f.EnableBetaFeatures,
		"enable-beta",
		false,
		"Set this flag to run tests against beta features.")

	flag.StringVar(&f.SkipTests,
		"skip-tests",
		"",
		"Set this flag to the tests you want to skip in alpha or beta features. Accepts a comma separated list.")

	flag.StringVar(&f.ClusterSuffix,
		"cluster-suffix",
		"cluster.local",
		"Set this flag to the cluster suffix to be used in tests.")

	return &f
}
