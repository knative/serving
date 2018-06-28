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
	"os"
	"os/user"
	"path"
)

// Flags holds the command line flags or defaults for settings in the user's environment.
// See EnvironmentFlags for a list of supported fields.
var Flags = initializeFlags()

type EnvironmentFlags struct {
	Cluster          string // K8s cluster (defaults to $K8S_CLUSTER_OVERRIDE)
	DockerRepo       string // Docker repo (defaults to $DOCKER_REPO_OVERRIDE)
	Kubeconfig       string // Path to kubeconfig (defaults to ./kube/config)
	Namespace        string // K8s namespace (blank by default, to be overwritten by test suite)
	ResolvableDomain bool   // Resolve Route controller's `domainSuffix`
	LogVerbose       bool   // Enable verbose logging
	EmitMetrics      bool   // Emit metrics
}

func initializeFlags() *EnvironmentFlags {
	var f EnvironmentFlags
	defaultCluster := os.Getenv("K8S_CLUSTER_OVERRIDE")
	flag.StringVar(&f.Cluster, "cluster", defaultCluster,
		"Provide the cluster to test against. Defaults to $K8S_CLUSTER_OVERRIDE, then current cluster in kubeconfig if $K8S_CLUSTER_OVERRIDE is unset.")

	defaultRepo := os.Getenv("DOCKER_REPO_OVERRIDE")
	flag.StringVar(&f.DockerRepo, "dockerrepo", defaultRepo,
		"Provide the uri of the docker repo you have uploaded the test image to using `uploadtestimage.sh`. Defaults to $DOCKER_REPO_OVERRIDE")

	usr, _ := user.Current()
	defaultKubeconfig := path.Join(usr.HomeDir, ".kube/config")

	flag.StringVar(&f.Kubeconfig, "kubeconfig", defaultKubeconfig,
		"Provide the path to the `kubeconfig` file you'd like to use for these tests. The `current-context` will be used.")

	flag.StringVar(&f.Namespace, "namespace", "",
		"Provide the namespace you would like to use for these tests.")

	flag.BoolVar(&f.ResolvableDomain, "resolvabledomain", false,
		"Set this flag to true if you have configured the `domainSuffix` on your Route controller to a domain that will resolve to your test cluster.")

	flag.BoolVar(&f.LogVerbose, "logverbose", false,
		"Set this flag to true if you would like to see verbose logging.")

	flag.BoolVar(&f.EmitMetrics, "emitmetrics", false,
		"Set this flag to true if you would like tests to emit metrics, e.g. latency of resources being realized in the system.")

	flag.Parse()
	flag.Set("alsologtostderr", "true")
	initializeLogger(f.LogVerbose)

	if f.EmitMetrics {
		initializeMetricExporter()
	}
	return &f
}
