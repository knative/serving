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

func bucketNames(bkts []reconciler.Bucket) []string {
	ret := make([]string, len(bkts))
	for i, bkt := range bkts {
		ret[i] = bkt.Name()
	}

	return ret
}
