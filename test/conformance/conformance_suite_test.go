/*
Copyright 2018 Google Inc. All Rights Reserved.
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

// conformance_suite_test contains the logic required across all conformance test specs.
package conformance

import (
	"flag"
	"os"
	"os/user"
	"path"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// Mysteriously required to support GCP auth (currently used for accessing already build image). Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var (
	cluster          string
	dockerRepo       string
	kubeconfig       string
	resolvableDomain bool
)

func init() {
	defaultCluster := os.Getenv("K8S_CLUSTER_OVERRIDE")
	flag.StringVar(&cluster, "cluster", defaultCluster,
		"Provide the cluster to test against. Defaults to $K8S_CLUSTER_OVERRIDE, then current cluster in kubeconfig if $K8S_CLUSTER_OVERRIDE is unset.")

	defaultRepo := os.Getenv("DOCKER_REPO_OVERRIDE")
	flag.StringVar(&dockerRepo, "dockerrepo", defaultRepo,
		"Provide the uri of the docker repo you have uploaded the test image to using `uploadtestimage.sh`. Defaults to $DOCKER_REPO_OVERRIDE")

	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Provide the path to the `kubeconfig` file you'd like to use for these tests. The `current-context` will be used.")

	if kubeconfig == "" {
		usr, _ := user.Current()
		kubeconfig = path.Join(usr.HomeDir, ".kube/config")
	}

	flag.BoolVar(&resolvableDomain, "resolvabledomain", false,
		"Set this flag to true if you have configured the `domainSuffix` on your Route controller to a domain that will resolve to your test cluster.")
}

func TestConformance(t *testing.T) {
	testing.Verbose()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Conformance Suite")
}
