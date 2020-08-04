// +build probe

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

package upgrade

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/resources"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

func TestScaleToZero(t *testing.T) {
	t.Parallel()
	// Create a named pipe and wait for the upgrade script to write to it
	// to signal that we should stop testing.
	createPipe(pipe, t)

	clients := e2e.Setup(t)
	names := test.ResourceNames{
		Service: "scale-to-zero",
		Image:   test.PizzaPlanet1,
	}
	cleanup := func() {
		test.TearDown(clients, &names)
		os.Remove(pipe)
	}
	t.Cleanup(cleanup)
	test.CleanupOnInterrupt(cleanup)

	objects, err := v1test.CreateServiceReady(t, clients, &names,
		rtesting.WithConfigAnnotations(map[string]string{
			// Scale to zero as quick as possible
			autoscaling.TargetBurstCapacityKey: "-1",
			autoscaling.WindowAnnotationKey:    autoscaling.WindowMin.String(),
		}))
	if err != nil {
		t.Fatal("Failed to create Service:", err)
	}
	url := objects.Service.Status.URL.URL()

	epsL, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).List(metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,%s=%s",
			serving.RevisionLabelKey, objects.Revision.Name,
			networking.ServiceTypeKey, networking.ServiceTypePrivate,
		),
	})
	if err != nil || len(epsL.Items) == 0 {
		t.Fatal("No endpoints or error:", err)
	}

	epsN := epsL.Items[0].Name

	// This polls until we get a 200 with the right body.
	assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)

	stopCh := make(chan struct{})
	go func() {
		// e2e-upgrade-test.sh will close this pipe to signal the upgrade is
		// over, at which point we will finish the test and check the prober.
		ioutil.ReadFile(pipe)
		close(stopCh)
	}()

	for {
		t.Log("Waiting for ready pods")
		// There're some delays to populate the IP to the endpoints of private Service
		// after the pod is up to serve.
		if err := wait.PollImmediate(100*time.Millisecond, 10*time.Second, func() (bool, error) {
			eps, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(epsN, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return resources.ReadyAddressCount(eps) > 0, nil
		}); err != nil {
			t.Fatalf("Did not observe %q to have ready IPs", epsN)
		}

		t.Log("Waiting for scaling to zero")
		st := time.Now()
		stop := false
		// TODO(yanweiguo): In normal scaling to zero e2e test, we use 45 seconds as timeout.
		// Need to tweak the value both here and there.
		if err := wait.PollImmediate(500*time.Millisecond, 45*time.Second, func() (bool, error) {
			select {
			case <-stopCh:
				t.Log("Received stop signal")
				stop = true
				return true, nil
			default:
				eps, err := clients.KubeClient.Kube.CoreV1().Endpoints(test.ServingNamespace).Get(epsN, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				return resources.ReadyAddressCount(eps) == 0, nil
			}
		}); err != nil {
			t.Fatalf("Did not observe %q to actually be emptied", epsN)
		}

		if stop {
			break
		}

		t.Logf("Total time to scale down: %v", time.Since(st))
		t.Log("Waiting for scaling from zero")
		assertServiceResourcesUpdated(t, clients, names, url, test.PizzaPlanetText1)
	}
}
