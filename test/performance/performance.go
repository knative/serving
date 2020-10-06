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

package performance

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	"knative.dev/pkg/test/spoof"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// ProbeTargetTillReady will probe the target once per second for the given duration, until it's ready or error happens
func ProbeTargetTillReady(target string, duration time.Duration) error {
	// Make sure the target is ready before sending the large amount of requests.
	spoofingClient := spoof.SpoofingClient{
		Client:          &http.Client{},
		RequestInterval: 1 * time.Second,
		RequestTimeout:  duration,
		Logf: func(fmt string, args ...interface{}) {
			log.Printf(fmt, args)
		},
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("target %q is invalid, cannot probe: %w", target, err)
	}
	if _, err = spoofingClient.Poll(req, func(resp *spoof.Response) (done bool, err error) {
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to get target %q ready: %w", target, err)
	}
	return nil
}

// WaitForScaleToZero will wait for the deployments in the indexer to scale to 0
func WaitForScaleToZero(ctx context.Context, namespace string, selector labels.Selector, duration time.Duration) error {
	pl := podinformer.Get(ctx).Lister()
	begin := time.Now()
	return wait.PollImmediate(time.Second, duration, func() (bool, error) {
		pods, err := pl.Pods(namespace).List(selector)
		if err != nil {
			return false, err
		}
		for _, pod := range pods {
			// Pending or Running w/o deletion timestamp (i.e. terminating).
			if pod.Status.Phase == v1.PodPending || pod.Status.Phase == v1.PodRunning && pod.ObjectMeta.DeletionTimestamp == nil {
				return false, nil
			}
		}
		log.Print("All pods are done or terminating after ", time.Since(begin))
		return true, nil
	})
}
