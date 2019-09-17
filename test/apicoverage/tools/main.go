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
	"os"
	"path"
	"strings"

	"knative.dev/pkg/test/webhook-apicoverage/coveragecalculator"
	"knative.dev/pkg/test/webhook-apicoverage/tools"
	"knative.dev/serving/test/apicoverage/image/common"
	"knative.dev/serving/test/apicoverage/image/rules"
	"knative.dev/test-infra/shared/prow"
)

var buildFailed = flag.Bool("build_failed", false,
	"Flag indicating if the apicoverage build failed.")

// Helper method to produce failed coverage results.
func getFailedResourceCoverages() *coveragecalculator.CoveragePercentages {
	percentCoverages := make(map[string]float64)
	for resourceKind := range common.ResourceMap {
		percentCoverages[resourceKind.Kind] = 0.0
	}
	percentCoverages["Overall"] = 0.0
	return &coveragecalculator.CoveragePercentages{
		ResourceCoverages: percentCoverages}
}

func main() {
	var (
		kubeConfigPath string
		serviceIP      string
		err            error
	)

	flag.Parse()
	// Ensure artifactsDir exist, in case not invoked from this script
	artifactsDir := prow.GetLocalArtifactsDir()
	if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
		if err = os.MkdirAll(artifactsDir, 0777); err != nil {
			log.Fatalf("Failed to create directory: %v", err)
		}
	}

	if *buildFailed {
		if err := tools.WriteResourcePercentages(path.Join(
			artifactsDir, "junit_bazel.xml"),
			getFailedResourceCoverages()); err != nil {
			log.Fatalf("Failed writing resource coverage percentages: %v",
				err)
		}
		return
	}

	if kubeConfigPath, err = tools.GetDefaultKubePath(); err != nil {
		log.Fatalf("Error retrieving kubeConfig path: %v", err)
	}

	if serviceIP, err = tools.GetWebhookServiceIP(kubeConfigPath, "",
		common.WebhookNamespace, common.CommonComponentName); err != nil {
		log.Fatalf("Error retrieving Service IP: %v", err)
	}

	for resource := range common.ResourceMap {
		err = tools.GetAndWriteResourceCoverage(serviceIP, resource.Kind,
			path.Join(artifactsDir, strings.ToLower(resource.Kind)+".html"),
			rules.GetDisplayRules())
		if err != nil {
			log.Printf("Failed retrieving resource coverage for"+
				" resource %s: %v ", resource.Kind, err)
		}
	}

	if err := tools.GetAndWriteTotalCoverage(serviceIP, path.Join(artifactsDir,
		"totalcoverage.html")); err != nil {
		log.Fatalf("total coverage retrieval failed: %v", err)
	}

	if coverage, err := tools.GetResourcePercentages(serviceIP); err != nil {
		log.Fatalf("Failed retrieving resource coverage percentages: %v",
			err)
	} else {
		if err = tools.WriteResourcePercentages(path.Join(
			artifactsDir, "junit_bazel.xml"), coverage); err != nil {
			log.Fatalf("Failed writing resource coverage percentages: %v",
				err)
		}
	}
}
