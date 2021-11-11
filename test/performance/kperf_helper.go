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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/gcs"
	"knative.dev/serving/pkg/client/clientset/versioned"
	"knative.dev/serving/test"
)

func readResultFromJsonFile(tmpFolder, filename string, result interface{}) error {
	d, err := os.Open(tmpFolder)
	if err != nil {
		return err
	}
	defer d.Close()

	files, err := d.Readdir(-1)
	if err != nil {
		return err
	}

	var jsonFile []byte
	if filename == "" {
		for _, file := range files {
			if file.Mode().IsRegular() {
				if filepath.Ext(file.Name()) == ".json" {
					f, err := os.Open(fmt.Sprintf("%s/%s", tmpFolder, file.Name()))
					jsonFile, _ = ioutil.ReadAll(f)
					if err != nil {
						return err
					}
					break
				}
			}
		}
	} else {
		f, err := os.Open(fmt.Sprintf("%s/%s", tmpFolder, filename))
		jsonFile, _ = ioutil.ReadAll(f)
		if err != nil {
			return err
		}
	}

	json.Unmarshal([]byte(jsonFile), &result)
	return nil
}

func getFilenameFromBucket(ctx context.Context, serviceAccountFile, bucketName, dirPath, matchingExtenstion string) (string, error) {
	gcsClient, err := gcs.NewClient(ctx, serviceAccountFile)
	if err != nil {
		return "", err
	}

	files, err := gcsClient.ListChildrenFiles(ctx, bucketName, dirPath)
	if err != nil {
		return "", err
	}

	for _, f := range files {
		if strings.HasSuffix(f, matchingExtenstion) {
			return f, nil
		}
	}

	return "", nil
}

func downloadLatestResults(ctx context.Context, outputLocation, serviceAccountFile, bucketName, objPath, fileName string) error {
	gcsClient, err := gcs.NewClient(ctx, serviceAccountFile)
	if err != nil {
		return err
	}

	dst := fmt.Sprintf("%s/%s", outputLocation, fileName)
	err = gcsClient.Download(ctx, bucketName, objPath, dst)
	if err != nil {
		return err
	}

	return nil
}

func uploadLatestResults(ctx context.Context, serviceAccountFile, bucketName, objPath, srcPath string) error {
	gcsClient, err := gcs.NewClient(ctx, serviceAccountFile)
	if err != nil {
		return err
	}

	objs, err := gcsClient.ListChildrenFiles(ctx, bucketName, objPath)
	if err != nil {
		return err
	}

	for _, obj := range objs {
		err = gcsClient.DeleteObject(ctx, bucketName, obj)
		if err != nil {
			fmt.Printf(" %s not found, nothing to delete. \n", objPath)
		}
	}

	d, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer d.Close()

	files, err := d.Readdir(-1)
	if err != nil {
		return err
	}

	for _, file := range files {
		src := fmt.Sprintf("%s/%s", srcPath, file.Name())
		dst := fmt.Sprintf("%s/%s", objPath, file.Name())
		err = gcsClient.Upload(ctx, bucketName, dst, src)
		if err != nil {
			return err
		}
	}

	return nil
}

func servingClientForNamespace(namespace string) (*test.ServingClients, error) {
	cfg, err := pkgTest.Flags.GetRESTConfig()
	if err != nil {
		return nil, err
	}

	cs, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &test.ServingClients{
		Configs:   cs.ServingV1().Configurations(namespace),
		Revisions: cs.ServingV1().Revisions(namespace),
		Routes:    cs.ServingV1().Routes(namespace),
		Services:  cs.ServingV1().Services(namespace),
	}, nil
}

func cleanupNamespaces(t *testing.T, clients *test.Clients, namespaces []string) {
	for _, ns := range namespaces {
		err := clients.KubeClient.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Failed to delete namespaces %#v", err)
		}
	}
}

func cleanupServices(t *testing.T, prefix, namespace string) {
	// kperf service clean --namespace test --svc-prefix ktest
	cmd := exec.Command("kperf", "service", "clean", "--namespace", namespace, "--svc-prefix", prefix)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		t.Fatalf("Failed to execute kperf service clean command %#v", err)
	}
	fmt.Println(out.String())
}
