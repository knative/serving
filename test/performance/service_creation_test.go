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
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/signals"
	"knative.dev/serving/test"
	"knative.dev/serving/test/performance/utils"
	v1test "knative.dev/serving/test/v1"
)

const (
	servicesCreateLatestDstName = "services-create-latest"
	servicesCreateTestName      = "services-create"
)

func TestPerformanceServiceCreation(t *testing.T) {
	clients := test.Setup(t)
	ctx := signals.NewContext()

	servicesCount := KperfFlags.ServicesCount
	servicesRange := fmt.Sprintf("1,%d", servicesCount)
	servicesTimeout := time.Duration(KperfFlags.ServicesTimeout) * time.Second

	namespaces := []string{}
	nsNamePrefix := test.AppendRandomString("kperf")
	t.Log("Creating namespaces to be used by kperf.")
	for i := 1; i < servicesCount+1; i++ {
		nsName := fmt.Sprintf("%s-%d", nsNamePrefix, i)
		namespaces = append(namespaces, nsName)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		_, err := clients.KubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create namespace %q: %#v", nsName, err)
		}
	}
	defer cleanupNamespaces(t, clients, namespaces)

	t.Log("Generating services by kperf.")
	// kperf service generate -n 10 -b 30 -c 5 -i 15 --namespace default  --svc-prefix ktest --wait --timeout 30s --max-scale 3 --min-scale 0
	generateCmd := exec.Command("kperf", "service", "generate", "-n", strconv.Itoa(servicesCount),
		"-i", "10", "-b", "10",
		"--min-scale", "1", "--max-scale", "2",
		"--namespace-prefix", nsNamePrefix,
		"--namespace-range", servicesRange,
		"--svc-prefix", KperfFlags.ServiceNamePrefix,
		"--wait", "true",
		"--timeout", servicesTimeout.String())
	var out bytes.Buffer
	generateCmd.Stdout = &out
	err := generateCmd.Run()
	if err != nil {
		t.Fatalf("Failed to execute kperf service generate command %#v", err)
	}
	fmt.Println(out.String())

	t.Log("Waiting for Services.")
	index := 0
	for _, ns := range namespaces {
		servingClientInNamespace, err := servingClientForNamespace(ns)
		if err != nil {
			t.Fatalf("Failed to get serving client %#v", err)
		}
		if err := v1test.WaitForServiceState(servingClientInNamespace, fmt.Sprintf("%s-%d", KperfFlags.ServiceNamePrefix, index), v1test.IsServiceReady, "ServiceIsReady"); err != nil {
			t.Fatalf("Failed waiting for serving to get Ready %#v", err)
		}
		index++
	}

	t.Log("Measuring results by kperf.")
	output := KperfFlags.KperfOutput
	if output == "" {
		output, err = os.MkdirTemp("", "kperfoutput")
		if err != nil {
			t.Fatalf("Failed creating temp folder %#v", err)
		}
		defer os.RemoveAll(output)
	}

	// kperf service measure --namespace kref-test-1 --svc-prefix ksvc --range 0,9  --verbose --output /tmp
	measureCmd := exec.Command("kperf", "service", "measure",
		"--verbose",
		"--namespace-prefix", nsNamePrefix,
		"--namespace-range", servicesRange,
		"--svc-prefix", KperfFlags.ServiceNamePrefix,
		"--range", servicesRange,
		"--output", output)

	var stdoutBuf, stderrBuf bytes.Buffer
	measureCmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	measureCmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	err = measureCmd.Run()
	if err != nil {
		t.Fatalf("Failed to execute kperf service measure command %#v", err)
	}
	outStr, errStr := string(stdoutBuf.Bytes()), string(stderrBuf.Bytes())
	fmt.Printf("\nout:\n%s\nerr:\n%s\n", outStr, errStr)

	measurment := MeasureResult{}
	err = readResultFromJsonFile(output, "", &measurment)
	if err != nil {
		t.Fatalf("Failed to parse kperf service measurment result %#v", err)
	}

	if measurment.Result.OverallAverage > KperfFlags.ServiceAverage {
		t.Fatal("kperf Services creation took too long")
	}

	if KperfFlags.UploadLatestResults {

		if KperfFlags.ServiceAccount == "" {
			t.Fatal("Service Account required for generating combined results")
		}
		if KperfFlags.BucketName == "" {
			t.Fatal("Bucket name required for generating combined results")
		}

		err = uploadLatestResults(ctx, KperfFlags.ServiceAccount, KperfFlags.BucketName, servicesCreateLatestDstName, output)
		if err != nil {
			t.Fatalf("uploading of latest results failed %#v", err)
		}
	}

	if KperfFlags.GenerateCombinedResults {
		if KperfFlags.ServiceAccount == "" {
			t.Fatal("Service Account required for generating combined results")
		}
		if KperfFlags.BucketName == "" {
			t.Fatal("Bucket name required for generating combined results")
		}

		downloadFailed := false
		objPath, err := getFilenameFromBucket(ctx, KperfFlags.ServiceAccount, KperfFlags.BucketName, servicesCreateLatestDstName, "csv")
		if err != nil {
			log.Printf("getting filename from bucket of latest results failed %#v", err)
		}
		latestCsvFile := fmt.Sprintf("%s-latest.csv", servicesCreateTestName)
		err = downloadLatestResults(ctx, output, KperfFlags.ServiceAccount, KperfFlags.BucketName, objPath, latestCsvFile)
		if err != nil {
			log.Printf("downloading of latest results failed %#v", err)
			downloadFailed = true
		}
		if !downloadFailed {
			//read latest results csv file
			latestFile, err := os.Open(fmt.Sprintf("%s/%s", output, latestCsvFile))
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}
			defer latestFile.Close()

			latestLines, err := csv.NewReader(latestFile).ReadAll()
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}

			d, err := os.Open(output)
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}
			defer d.Close()

			files, err := d.Readdir(-1)
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}
			var csvFilename string
			for _, file := range files {
				if file.Mode().IsRegular() {
					if filepath.Ext(file.Name()) == ".csv" && strings.Contains(file.Name(), "creation") {
						csvFilename = file.Name()
						break
					}
				}
			}

			// Open CSV file
			f, err := os.Open(fmt.Sprintf("%s/%s", output, csvFilename))
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}
			defer f.Close()

			// Read File into *lines* variable
			lines, err := csv.NewReader(f).ReadAll()
			if err != nil {
				t.Fatalf("Failed to parse kperf services create reference result %#v", err)
			}
			for index, line := range lines {
				if index == 0 {
					lines[index] = append(line, "latest_configuration_ready", "latest_revision_ready", "latest_deployment_created", "latest_pod_scheduled",
						"latest_containers_ready", "latest_queue-proxy_started", "latest_user-container_started", "latest_route_ready", "latest_kpa_active",
						"latest_sks_ready", "latest_sks_activator_endpoints_populated", "latest_sks_endpoints_populated", "latest_ingress_ready",
						"latest_ingress_config_ready", "latest_ingress_lb_ready", "latest_overall_ready")
				} else {
					lines[index] = append(line, latestLines[index][2:]...)
				}
			}

			ff, err := os.Create(fmt.Sprintf("%s/%s-results-combined.csv", output, servicesCreateTestName))
			if err != nil {
				t.Fatalf("failed to create csv file %s\n", err)
			}
			defer ff.Close()

			csvWriter := csv.NewWriter(ff)
			csvWriter.WriteAll(lines)
			csvWriter.Flush()

			err = utils.GenerateHTMLFile(fmt.Sprintf("%s/%s-results-combined.csv", output, servicesCreateTestName), fmt.Sprintf("%s/%s-results-combined.html", output, servicesCreateTestName))
			if err != nil {
				t.Fatalf("failed to create html file %s\n", err)
			}
			log.Printf("CSV combined result saved in csv file %s/services-create-results-combined.csv \n", output)
			log.Printf("HTML combined result saved in html file %s/services-create-results-combined.html \n", output)
		}
	}
}
