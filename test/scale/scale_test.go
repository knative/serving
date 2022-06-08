//go:build e2e
// +build e2e

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

package scale

import (
	"flag"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"knative.dev/serving/test"
	"knative.dev/serving/test/watch"
)

var csvOutputDir = flag.String("readiness-csv-output", "", "Output dir for readiness csv file")

type nopLatencies struct {
	t *testing.T
}

var _ Latencies = (*nopLatencies)(nil)

func (nl *nopLatencies) Add(metric string, start time.Time) {
	duration := time.Since(start)

	nl.t.Logf("%q took %v", metric, duration)
}

const (
	// Limit for scale in -short mode
	shortModeMaxScale = 10

	// Timeout for each worker task
	workerTimeout = 3 * time.Minute
)

var (
	NameExtractorRegexp = regexp.MustCompile(`\d+-of-\d+`)
)

// While redundant, we run two versions of this by default:
// 1. TestScaleToN/size-10: a developer smoke test that's useful when changing this to assess whether
//   things have gone horribly wrong.  This should take about 12-20 seconds total.
// 2. TestScaleToN/scale-200: a more proper execution of the test, which verifies a slightly more
//   interesting burst of deployments, but low enough to complete in a reasonable window.
func TestScaleToN(t *testing.T) {
	// Run each of these variations.
	tests := []int{10, 200}

	for _, size := range tests {
		clients := test.Setup(t, test.Options{DisableLogStream: true})
		t.Log("start capture")
		stop := watch.StartCapture(t, clients)
		testName := fmt.Sprint("scale-", size)

		t.Run(testName, func(t *testing.T) {
			if testing.Short() && size > shortModeMaxScale {
				t.Skip("Skipping test in short mode")
			}
			ScaleToWithin(t, size, workerTimeout, &nopLatencies{t})
		})

		path := filepath.Join(*csvOutputDir, testName)
		filter := fmt.Sprintf("%s/scale-to-n-%s", test.ServingFlags.TestNamespace, testName)
		t.Log("writing readiness csv at", path)
		csvOutput := watch.CSVWriter{
			Directory:              path,
			ObjectNamePrefixFilter: filter,
			AdditionalColumnTitles: func() []string {
				return []string{"shortName"}
			},
			AdditionalRowFields: func(u *unstructured.Unstructured) []string {
				return []string{NameExtractorRegexp.FindString(u.GetName())}
			},
		}
		csvOutput.WriteHistory(stop())
	}
}
