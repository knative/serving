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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/serving/pkg/apis/serving"

	"golang.org/x/sync/errgroup"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/performance/utils"
	v1test "knative.dev/serving/test/v1"
)

const (
	scaleFromZeroLatestDstName = "scale-from-zero-latest"
	scaleFromZeroTestName      = "scale-from-zero"
)

func TestPerformanceScaleFromZero(t *testing.T) {
	clients := test.Setup(t)
	ctx := context.Background()

	servicesCount := KperfConfig.ServicesCount
	servicesRange := fmt.Sprintf("1,%d", servicesCount)
	servicesTimeout := time.Duration(KperfConfig.ServicesTimeout) * time.Second

	namespaces := []string{}
	nsName := test.AppendRandomString("kperf")
	t.Log("Creating namespaces to be used by kperf.")
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
	defer cleanupNamespaces(t, clients, namespaces)

	t.Log("Generating services by kperf.")
	// kperf service generate -n 10 -b 30 -c 5 -i 15 --namespace default  --svc-prefix ktest --wait --timeout 30s --max-scale 3 --min-scale 0
	generateCmd := exec.Command("kperf", "service", "generate", "-n", strconv.Itoa(servicesCount),
		"-i", "10", "-b", "10",
		"--min-scale", "0", "--max-scale", "2",
		"--namespace", nsName,
		"--svc-prefix", KperfConfig.ServiceNamePrefix,
		"--timeout", servicesTimeout.String())
	var out bytes.Buffer
	generateCmd.Stdout = &out
	err = generateCmd.Run()
	if err != nil {
		t.Fatalf("Failed to execute kperf service generate command %#v", err)
	}
	fmt.Println(out.String())
	defer cleanupServices(t, KperfConfig.ServiceNamePrefix, nsName)

	t.Log("Waiting for Services.")
	servingClientInNamespace, err := servingClientForNamespace(nsName)
	if err != nil {
		t.Fatalf("Failed to get serving client %#v", err)
	}

	objs := []*v1.Service{}
	for i := 0; i < servicesCount; i++ {
		svcName := fmt.Sprintf("%s-%d", KperfConfig.ServiceNamePrefix, i)
		if err := v1test.WaitForServiceState(servingClientInNamespace, fmt.Sprintf("%s-%d", KperfConfig.ServiceNamePrefix, i), v1test.IsServiceReady, "ServiceIsReady"); err != nil {
			t.Fatalf("Failed waiting for serving to get Ready %#v", err)
		}
		svc, err := servingClientInNamespace.Services.Get(ctx, svcName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get service %#v", err)
		}
		objs = append(objs, svc)
	}

	t.Log("Wait for Services to scale down to 0.")
	err = waitForScaleToZero(ctx, clients, nsName, objs)
	if err != nil {
		t.Fatalf("Failed to wait for services to scale down to 0 %#v", err)
	}

	t.Log("Scale and measure results by kperf.")
	output := KperfConfig.KperfOutput
	if output == "" {
		output, err = os.MkdirTemp("", "kperfscaleoutput")
		if err != nil {
			t.Fatalf("Failed creating temp folder %#v", err)
		}
		defer os.RemoveAll(output)
	}

	// kperf service scale  --namespace default --svc-prefix ktest --range 0,9  --verbose --output /tmp
	scaleCmd := exec.Command("kperf", "service", "scale",
		"--verbose",
		"--namespace", nsName,
		"--svc-prefix", KperfConfig.ServiceNamePrefix,
		"--range", servicesRange,
		"--output", output)
	var outScale bytes.Buffer
	scaleCmd.Stdout = &outScale
	err = scaleCmd.Run()
	if err != nil {
		t.Fatalf("Failed to execute kperf service scale command %#v", err)
	}
	fmt.Println(outScale.String())

	result := ScaleResult{}
	err = readResultFromJsonFile(output, "", &result)
	if err != nil {
		t.Fatalf("Failed to parse kperf service scale result %#v", err)
	}
	if len(result.Measurment) == 0 {
		t.Fatal("kperf Services didn't scale")
	}

	if KperfConfig.UploadLatestResults {
		if KperfConfig.ServiceAccount == "" {
			t.Fatal("Service Account required for generating combined results")
		}
		if KperfConfig.BucketName == "" {
			t.Fatal("Bucket name required for generating combined results")
		}

		err = uploadLatestResults(ctx, KperfConfig.ServiceAccount, KperfConfig.BucketName, scaleFromZeroLatestDstName, output)
		if err != nil {
			t.Fatalf("uploading of latest results failed %#v", err)
		}
	}

	if KperfConfig.GenerateCombinedResults {
		if KperfConfig.ServiceAccount == "" {
			t.Fatal("Service Account required for generating combined results")
		}
		if KperfConfig.BucketName == "" {
			t.Fatal("Bucket name required for generating combined results")
		}

		downloadFailed := false
		objPath, err := getFilenameFromBucket(ctx, KperfConfig.ServiceAccount, KperfConfig.BucketName, scaleFromZeroLatestDstName, "json")
		if err != nil {
			log.Printf("getting filename from bucket of latest results failed %#v", err)
		}
		latestFilename := fmt.Sprintf("%s-latest.json", scaleFromZeroTestName)
		err = downloadLatestResults(ctx, output, KperfConfig.ServiceAccount, KperfConfig.BucketName, objPath, latestFilename)
		if err != nil {
			log.Printf("downloading of latest results failed %#v", err)
			downloadFailed = true
		}
		if !downloadFailed {
			latestResult := ScaleResult{}
			err = readResultFromJsonFile(output, latestFilename, &latestResult)
			if err != nil {
				t.Fatalf("Failed to parse kperf service scale reference result %#v", err)
			}
			d, err := os.Open(output)
			if err != nil {
				t.Fatalf("Failed to parse kperf service scale reference result %#v", err)
			}
			defer d.Close()

			files, err := d.Readdir(-1)
			if err != nil {
				t.Fatalf("Failed to parse kperf service scale reference result %#v", err)
			}
			var csvFilename string
			for _, file := range files {
				if file.Mode().IsRegular() {
					if filepath.Ext(file.Name()) == ".csv" && strings.Contains(file.Name(), "scaling") {
						csvFilename = file.Name()
						break
					}
				}
			}

			// Open CSV file
			f, err := os.Open(fmt.Sprintf("%s/%s", output, csvFilename))
			if err != nil {
				t.Fatalf("Failed to parse kperf service scale reference result %#v", err)
			}
			defer f.Close()

			// Read File into *lines* variable
			lines, err := csv.NewReader(f).ReadAll()
			if err != nil {
				t.Fatalf("Failed to parse kperf service scale reference result %#v", err)
			}
			for index, line := range lines {
				if index == 0 {
					lines[index] = append(line, "latest_service_latency", "latest_deployment_latency")
				} else {
					for _, s := range latestResult.Measurment {
						if line[0] == s.ServiceName {
							lines[index] = append(line, fmt.Sprintf("%f", s.ServiceLatency), fmt.Sprintf("%f", s.DeploymentLatency))
							break
						}
					}
				}
			}

			ff, err := os.Create(fmt.Sprintf("%s/%s-results-combined.csv", output, scaleFromZeroTestName))
			if err != nil {
				t.Fatalf("failed to create csv file %s\n", err)
			}
			defer ff.Close()

			csvWriter := csv.NewWriter(ff)
			csvWriter.WriteAll(lines)
			csvWriter.Flush()

			err = utils.GenerateHTMLFile(fmt.Sprintf("%s/%s-results-combined.csv", output, scaleFromZeroTestName), fmt.Sprintf("%s/%s-results-combined.html", output, scaleFromZeroTestName))
			if err != nil {
				t.Fatalf("failed to create html file %s\n", err)
			}
			log.Printf("CSV combined result saved in csv file %s/scale-from-zero-results-combined.csv \n", output)
			log.Printf("HTML combined result saved in html file %s/scale-from-zero-results-combined.html \n", output)
		}
	}
}

func waitForScaleToZero(ctx context.Context, client *test.Clients, namespace string, services []*v1.Service) error {
	g := errgroup.Group{}
	for _, ro := range services {
		ro := ro // avoid data race
		g.Go(func() error {
			selector := labels.SelectorFromSet(labels.Set{
				serving.ServiceLabelKey: ro.Name,
			})
			begin := time.Now()
			err := wait.PollImmediate(5*time.Second, 5*time.Minute, func() (bool, error) {
				pods, err := client.KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
				if err != nil {
					return false, err
				}
				for _, pod := range pods.Items {
					// Pending or Running w/o deletion timestamp (i.e. terminating).
					if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning && pod.ObjectMeta.DeletionTimestamp == nil {
						return false, nil
					}
				}
				log.Print("All pods are done or terminating after ", time.Since(begin))
				return true, nil
			})

			return err
		})
	}
	return g.Wait()
}
