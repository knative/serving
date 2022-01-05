//go:build performance
// +build performance

/*
Copyright 2021 The Knative Authors

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

package performance

import (
	"flag"

	// Load the generic flags of knative.dev/pkg too.
	_ "knative.dev/pkg/test"
)

// KperfConfig holds the config or defaults for kperf performance test tool.
var KperfConfig = initializeKperfConfig()

// KperfEnvironmentConfig holds the config needed only by the kperf testing tool.
type KperfEnvironmentConfig struct {
	ServiceNamePrefix       string
	ServicesCount           int
	ServicesTimeout         int
	ServiceAverage          float64
	KperfOutput             string
	ServiceAccount          string
	BucketName              string
	GenerateCombinedResults bool
	UploadLatestResults     bool
}

func initializeKperfConfig() *KperfEnvironmentConfig {
	var f KperfEnvironmentConfig

	flag.StringVar(&f.ServiceNamePrefix, "service-prefix", "ksvc", "Set this flag for the kperf Service name prefix.")

	flag.IntVar(&f.ServicesCount, "services-count", 10, "Set this flag for the kperf Services count.")

	flag.StringVar(&f.KperfOutput, "kperf-output", "", "Set this flag for the kperf output file location.")

	flag.Float64Var(&f.ServiceAverage, "service-average", 20, "Set this flag for the time kperf average time for service creation.")

	flag.IntVar(&f.ServicesTimeout, "service-timeout", 30, "Set this flag for the kperf Service creation timeout.")

	flag.StringVar(&f.ServiceAccount, "service-account", "", "Path to service account file for GCP bucket access.")

	flag.StringVar(&f.BucketName, "bucket", "", "GCP bucket with reference results.")

	flag.BoolVar(&f.GenerateCombinedResults, "combine-results", false, "Set this flag to generate combined results of current run with reference latest run.")

	flag.BoolVar(&f.UploadLatestResults, "upload-results", false, "Set this flag to upload latest results of current to reference bucket.")

	return &f
}
