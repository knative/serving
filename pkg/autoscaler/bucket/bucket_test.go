/*
Copyright 2020 The Knative Authors

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

package bucket

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"knative.dev/pkg/reconciler"
)

func TestIsBucketHost(t *testing.T) {
	if got, want := IsBucketHost("autoscaler-bucket-00-of-03"), true; got != want {
		t.Errorf("IsBucketHost = %v, want = %v", got, want)
	}

	if got, want := IsBucketHost("autoscaler"), false; got != want {
		t.Errorf("IsBucketHost = %v, want = %v", got, want)
	}

	if got, want := IsBucketHost("autoscaler-bucket-non-exits"), true; got != want {
		t.Errorf("IsBucketHost = %v, want = %v", got, want)
	}
}

func TestAutoscalerBucketName(t *testing.T) {
	if got, want := AutoscalerBucketName(0, 10), "autoscaler-bucket-00-of-10"; got != want {
		t.Errorf("AutoscalerBucketName = %v, want = %v", got, want)
	}

	if got, want := AutoscalerBucketName(10, 10), "autoscaler-bucket-10-of-10"; got != want {
		t.Errorf("AutoscalerBucketName = %v, want = %v", got, want)
	}

	if got, want := AutoscalerBucketName(10, 1), "autoscaler-bucket-10-of-01"; got != want {
		t.Errorf("AutoscalerBucketName = %v, want = %v", got, want)
	}
}

func TestAutoscalerBucketSet(t *testing.T) {
	want := []string{}
	if got := bucketNames(AutoscalerBucketSet(0).Buckets()); !cmp.Equal(got, want) {
		t.Errorf("AutoscalerBucketSet = %v, want = %v", got, want)
	}

	want = []string{"autoscaler-bucket-00-of-01"}
	if got := bucketNames(AutoscalerBucketSet(1).Buckets()); !cmp.Equal(got, want) {
		t.Errorf("AutoscalerBucketSet = %v, want = %v", got, want)
	}

	want = []string{
		"autoscaler-bucket-00-of-03", "autoscaler-bucket-01-of-03", "autoscaler-bucket-02-of-03"}
	if got := bucketNames(AutoscalerBucketSet(3).Buckets()); !cmp.Equal(got, want) {
		t.Errorf("AutoscalerBucketSet = %v, want = %v", got, want)
	}
}

func TestIdentity(t *testing.T) {
	testcases := []struct {
		name      string
		noSetIP   bool
		noSetName bool
		podIP     string
		podName   string
		want      string
		wantErr   string
	}{{
		name:    "happy case",
		podIP:   "1.2.3.4",
		podName: "as",
		want:    "as_1.2.3.4",
	}, {
		name:    "another happy case",
		podIP:   "1.2.3.400",
		podName: "autoscaler-abcd-efead",
		want:    "autoscaler-abcd-efead_1.2.3.400",
	}, {
		name:    "empty pod IP",
		podIP:   "",
		podName: "autoscaler-abcd-efead",
		wantErr: "POD_IP environment variable is empty",
	}, {
		name:    "empty pod name",
		podIP:   "1.2.3.4",
		podName: "",
		wantErr: "POD_NAME environment variable is empty",
	}, {
		name:    "pod IP not set",
		noSetIP: true,
		podName: "autoscaler-abcd-efead",
		wantErr: "POD_IP environment variable not set",
	}, {
		name:      "pod name not set",
		podIP:     "1.2.3.4",
		noSetName: true,
		wantErr:   "POD_NAME environment variable not set",
	}}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.noSetIP {
				os.Setenv("POD_IP", tc.podIP)
				defer os.Unsetenv("POD_IP")
			}
			if !tc.noSetName {
				os.Setenv("POD_NAME", tc.podName)
				defer os.Unsetenv("POD_NAME")
			}

			got, err := Identity()
			if err != nil {
				if tc.wantErr == "" {
					t.Fatalf("Unexpect error from Identity(): %v", err)
				}
				if got := err.Error(); got != tc.wantErr {
					t.Errorf("got := %v, want = %v", got, tc.wantErr)
				}
			} else {
				if tc.wantErr != "" {
					t.Fatal("Expect error from Identity() but got nil")
				}
				if got != tc.want {
					t.Errorf("got := %v, want = %v", got, tc.want)
				}
			}
		})
	}
}

func TestExtractPodNameAndIP(t *testing.T) {
	gotName, gotIP, err := ExtractPodNameAndIP("as_1.2.3.4")
	if err != nil {
		t.Fatalf("Unexpect error from Identity(): %v", err)
	}
	if wantName := "as"; gotName != wantName {
		t.Errorf("got := %v, want = %v", gotName, wantName)
	}
	if wantIP := "1.2.3.4"; gotIP != wantIP {
		t.Errorf("got := %v, want = %v", gotIP, wantIP)
	}

	_, _, err = ExtractPodNameAndIP("1.2.3.4")
	if err == nil {
		t.Fatal("Expect error from Identity() but got nil")
	}
}

func bucketNames(bkts []reconciler.Bucket) []string {
	ret := make([]string, len(bkts))
	for i, bkt := range bkts {
		ret[i] = bkt.Name()
	}

	return ret
}
