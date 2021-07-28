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
	"errors"
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/hash"
)

const prefix = "autoscaler-bucket"

// IsBucketHost returns true if the given host is a host of a K8S Service
// of a bucket.
func IsBucketHost(host string) bool {
	// Currently checking prefix is ok as only requests sent via bucket service
	// have host with the prefix. Maybe use regexp for improvement.
	return strings.HasPrefix(host, prefix)
}

// AutoscalerBucketName returns the name of the Autoscaler bucket with given `ordinal`
// and `total` bucket count.
func AutoscalerBucketName(ordinal, total uint32) string {
	return strings.ToLower(fmt.Sprintf("%s-%02d-of-%02d", prefix, ordinal, total))
}

// AutoscalerBucketSet returns a hash.BucketSet consisting of Autoscaler
// buckets with the given `total` count.
func AutoscalerBucketSet(total uint32) *hash.BucketSet {
	names := make(sets.String, total)
	for i := uint32(0); i < total; i++ {
		names.Insert(AutoscalerBucketName(i, total))
	}
	return hash.NewBucketSet(names)
}

// PodIP returns the IP address of the current pod, or an error
// if it wasn't properly projected.
func PodIP() (string, error) {
	selfIP, existing := os.LookupEnv("POD_IP")
	if !existing {
		return "", errors.New("POD_IP environment variable not set")
	}
	if selfIP == "" {
		return "", errors.New("POD_IP environment variable is empty")
	}
	return selfIP, nil
}

// Identity returns an identity for this Autoscaler pod used as the Lease holder
// identity. It's in the format of <POD-NAME>_<POD-IP> whose information is ready from
// environment variables.
func Identity() (string, error) {
	selfIP, err := PodIP()
	if err != nil {
		return "", err
	}

	podName, existing := os.LookupEnv("POD_NAME")
	if !existing {
		return "", errors.New("POD_NAME environment variable not set")
	}
	if podName == "" {
		return "", errors.New("POD_NAME environment variable is empty")
	}

	return podName + "_" + selfIP, nil
}

// ExtractPodNameAndIP extracts the pod name and IP from the given Lease holder
// identity.
func ExtractPodNameAndIP(id string) (string, string, error) {
	arr := strings.SplitN(id, "_", 2)
	if len(arr) < 2 {
		return "", "", errors.New("Failed to extract pod name and IP from " + id)
	}
	return arr[0], arr[1], nil
}
