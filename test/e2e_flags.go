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

	network "knative.dev/networking/pkg"

	// Load the generic flags of knative.dev/pkg too.
	_ "knative.dev/pkg/test"
)

// ServingFlags holds the flags or defaults for knative/serving settings in the user's environment.
var ServingFlags = initializeServingFlags()

// ServingEnvironmentFlags holds the e2e flags needed only by the serving repo.
type ServingEnvironmentFlags struct {
	ResolvableDomain    bool   // Resolve Route controller's `domainSuffix`
	CustomDomain        string // Indicaates the `domainSuffix` for custom domain test.
	HTTPS               bool   // Indicates where the test service will be created with https
	IngressClass        string // Indicates the class of Ingress provider to test.
	CertificateClass    string // Indicates the class of Certificate provider to test.
	Buckets             int    // The number of reconciler buckets configured.
	Replicas            int    // The number of controlplane replicas being run.
	EnableAlphaFeatures bool   // Indicates whether we run tests for alpha features
	EnableBetaFeatures  bool   // Indicates whether we run tests for beta features
}

func initializeServingFlags() *ServingEnvironmentFlags {
	var f ServingEnvironmentFlags

	// Only define and set flags here. Flag values cannot be read at package init time.
	if fl := flag.Lookup("resolvabledomain"); fl == nil {
		// Only define and set flags here. Flag values cannot be read at package init time.
		flag.BoolVar(&f.ResolvableDomain,
			"resolvabledomain",
			false,
			"Set this flag to true if you have configured the `domainSuffix` on your Route controller to a domain that will resolve to your test cluster.")
	} else {
		f.ResolvableDomain = fl.Value.(flag.Getter).Get().(bool)
	}

	if fl := flag.Lookup("customdomain"); fl == nil {
		flag.StringVar(&f.CustomDomain,
			"customdomain",
			"",
			"Set this flag to the custom domain suffix for domainmapping test.")
	} else {
		f.CustomDomain = fl.Value.String()
	}

	if fl := flag.Lookup("https"); fl == nil {
		flag.BoolVar(&f.HTTPS,
			"https",
			false,
			"Set this flag to true to run all tests with https.")
	} else {
		f.HTTPS = fl.Value.(flag.Getter).Get().(bool)
	}

	if fl := flag.Lookup("ingressClass"); fl == nil {
		flag.StringVar(&f.IngressClass,
			"ingressClass",
			network.IstioIngressClassName,
			"Set this flag to the ingress class to test against.")
	} else {
		f.IngressClass = fl.Value.String()
	}

	if fl := flag.Lookup("certificateClass"); fl == nil {
		flag.StringVar(&f.CertificateClass,
			"certificateClass",
			network.CertManagerCertificateClassName,
			"Set this flag to the certificate class to test against.")
	} else {
		f.IngressClass = fl.Value.String()
	}

	if fl := flag.Lookup("buckets"); fl == nil {
		flag.IntVar(&f.Buckets,
			"buckets",
			1,
			"Set this flag to the number of reconciler buckets configured.")
	} else {
		f.Buckets = fl.Value.(flag.Getter).Get().(int)
	}

	if fl := flag.Lookup("replicas"); fl == nil {
		flag.IntVar(&f.Replicas,
			"replicas",
			1,
			"Set this flag to the number of controlplane replicas being run.")
	} else {
		f.Replicas = fl.Value.(flag.Getter).Get().(int)
	}

	if fl := flag.Lookup("enable-alpha"); fl == nil {
		flag.BoolVar(&f.EnableAlphaFeatures,
			"enable-alpha",
			false,
			"Set this flag to run tests against alpha features")
	} else {
		f.EnableAlphaFeatures = fl.Value.(flag.Getter).Get().(bool)
	}

	if fl := flag.Lookup("enable-beta"); fl == nil {
		flag.BoolVar(&f.EnableBetaFeatures,
			"enable-beta",
			false,
			"Set this flag to run tests against beta features")
	} else {
		f.EnableBetaFeatures = fl.Value.(flag.Getter).Get().(bool)
	}

	return &f
}
