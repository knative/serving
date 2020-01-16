/*
Copyright 2019 The Knative Authors

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
	"flag"
	"log"

	testPkg "knative.dev/pkg/testutils/clustermanager/perf-tests/pkg"
)

// flags supported by this tool
var (
	isRecreate          bool
	isReconcile         bool
	gcpProjectName      string
	repoName            string
	benchmarkRootFolder string
	gkeEnvironment      string
)

func main() {
	flag.StringVar(&gcpProjectName, "gcp-project", "", "name of the GCP project for cluster operations")
	flag.StringVar(&gkeEnvironment, "gke-environment", "prod", "Container API endpoint to use, one of 'test', 'staging', 'staging2', 'prod', or a custom https:// URL. Default to be prod.")
	flag.StringVar(&repoName, "repository", "", "name of the repository")
	flag.StringVar(&benchmarkRootFolder, "benchmark-root", "", "root folder of the benchmarks")
	flag.BoolVar(&isRecreate, "recreate", false, "is recreate operation or not")
	flag.BoolVar(&isReconcile, "reconcile", false, "is reconcile operation or not")
	flag.Parse()

	if isRecreate && isReconcile {
		log.Fatal("Only one operation can be specified, either recreate or reconcile")
	}

	client, err := testPkg.NewClient(gkeEnvironment)
	if err != nil {
		log.Fatalf("Failed setting up GKE client, cannot proceed: %v", err)
	}
	switch {
	case isRecreate:
		if err := client.RecreateClusters(gcpProjectName, repoName, benchmarkRootFolder); err != nil {
			log.Fatalf("Failed recreating clusters for repo %q: %v", repoName, err)
		}
		log.Printf("Done with recreating clusters for repo %q", repoName)
	case isReconcile:
		if err := client.ReconcileClusters(gcpProjectName, repoName, benchmarkRootFolder); err != nil {
			log.Fatalf("Failed reconciling clusters for repo %q: %v", repoName, err)
		}
		log.Printf("Done with reconciling clusters for repo %q", repoName)
	default:
		log.Fatal("One operation must be specified, either recreate or reconcile")
	}
}
