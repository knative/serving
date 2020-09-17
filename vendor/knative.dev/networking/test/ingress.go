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

package test

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/test/logging"
)

// WaitForIngressState polls the status of the Ingress called name from client every
// PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForIngressState(ctx context.Context, client *NetworkingClients, name string, inState func(r *v1alpha1.Ingress) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForIngressState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Ingress
	waitErr := wait.PollImmediate(PollInterval, PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Ingresses.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("ingress %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// IsIngressReady will check the status conditions of the ingress and return true if the ingress is
// ready.
func IsIngressReady(r *v1alpha1.Ingress) (bool, error) {
	return r.IsReady(), nil
}
