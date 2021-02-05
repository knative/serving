/*
Copyright 2019 The Knative Authors

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

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@.
	// https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	appsv1 "k8s.io/api/apps/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/system"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	asconfig "knative.dev/serving/pkg/autoscaler/config"
	"knative.dev/serving/pkg/autoscaler/config/autoscalerconfig"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"
)

// Setup creates the client objects needed in the e2e tests.
func Setup(t *testing.T) *test.Clients {
	return SetupWithNamespace(t, test.ServingNamespace)
}

// SetupAlternativeNamespace creates the client objects needed in e2e tests
// under the alternative namespace.
func SetupAlternativeNamespace(t *testing.T) *test.Clients {
	return SetupWithNamespace(t, test.AlternativeServingNamespace)
}

// SetupWithNamespace creates the client objects needed in the e2e tests under the specified namespace.
func SetupWithNamespace(t *testing.T, namespace string) *test.Clients {
	t.Helper()
	pkgTest.SetupLoggingFlags()

	cancel := logstream.Start(t)
	t.Cleanup(cancel)

	clients, err := test.NewClients(
		pkgTest.Flags.Kubeconfig,
		pkgTest.Flags.Cluster,
		namespace)
	if err != nil {
		t.Fatal("Couldn't initialize clients:", err)
	}
	return clients
}

// autoscalerCM returns the current autoscaler config map deployed to the
// test cluster.
func autoscalerCM(clients *test.Clients) (*autoscalerconfig.Config, error) {
	autoscalerCM, err := clients.KubeClient.CoreV1().ConfigMaps(system.Namespace()).Get(
		context.Background(), asconfig.ConfigName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return asconfig.NewConfigFromMap(autoscalerCM.Data)
}

// WaitForScaleToZero will wait for the specified deployment to scale to 0 replicas.
// Will wait up to 6 times the configured ScaleToZeroGracePeriod before failing.
func WaitForScaleToZero(t *testing.T, deploymentName string, clients *test.Clients) error {
	t.Helper()
	t.Logf("Waiting for %q to scale to zero", deploymentName)

	cfg, err := autoscalerCM(clients)
	if err != nil {
		return fmt.Errorf("failed to get autoscaler configmap: %w", err)
	}

	return pkgTest.WaitForDeploymentState(
		context.Background(),
		clients.KubeClient,
		deploymentName,
		func(d *appsv1.Deployment) (bool, error) {
			return d.Status.ReadyReplicas == 0, nil
		},
		"DeploymentIsScaledDown",
		test.ServingNamespace,
		cfg.ScaleToZeroGracePeriod*6,
	)
}

// waitForActivatorEndpoints waits for the Service endpoints to match that of activator.
func waitForActivatorEndpoints(ctx *TestContext) error {
	var (
		aset, svcSet sets.String
		wantAct      int
	)

	if rerr := wait.Poll(250*time.Millisecond, time.Minute, func() (bool, error) {
		// We need to fetch the activator endpoints at every check, since it can change.
		actEps, err := ctx.clients.KubeClient.CoreV1().Endpoints(
			system.Namespace()).Get(context.Background(), networking.ActivatorServiceName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		sks, err := ctx.clients.NetworkingClient.ServerlessServices.Get(context.Background(), ctx.resources.Revision.Name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		svcEps, err := ctx.clients.KubeClient.CoreV1().Endpoints(test.ServingNamespace).Get(
			context.Background(), ctx.resources.Revision.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		wantAct = int(sks.Spec.NumActivators)
		aset = make(sets.String, wantAct)
		for _, ss := range actEps.Subsets {
			for i := 0; i < len(ss.Addresses); i++ {
				aset.Insert(ss.Addresses[i].IP)
			}
		}
		svcSet = make(sets.String, wantAct)
		for _, ss := range svcEps.Subsets {
			for i := 0; i < len(ss.Addresses); i++ {
				svcSet.Insert(ss.Addresses[i].IP)
			}
		}
		// Subset wants this many activators, but there might not be as many,
		// so reduce the expectation.
		if aset.Len() < wantAct {
			wantAct = aset.Len()
		}
		// If public endpoints have not yet been updated with the new values.
		if svcSet.Len() < wantAct {
			return false, nil
		}
		return svcSet.Intersection(aset).Len() > 0, nil
	}); rerr != nil {
		ctx.t.Logf("Did not see activator endpoints in public service for %s."+
			"Last received values: Activator: %v "+
			"PubSvc: %v, WantActivators %d",
			ctx.resources.Revision.Name, aset.List(), svcSet.List(), wantAct)
		return rerr
	}
	return nil
}

// CreateAndVerifyInitialScaleConfiguration creates a Configuration with the
// `initialScale` annotation set and validates the `wantPods` number of pods
// are created.
func CreateAndVerifyInitialScaleConfiguration(t *testing.T, clients *test.Clients, names test.ResourceNames, wantPods int) {
	t.Log("Creating a new Configuration.", "configuration", names.Config)
	_, err := v1test.CreateConfiguration(t, clients, names, func(configuration *v1.Configuration) {
		configuration.Spec.Template.Annotations = kmeta.UnionMaps(
			configuration.Spec.Template.Annotations, map[string]string{
				autoscaling.InitialScaleAnnotationKey: strconv.Itoa(wantPods),
			})
	})
	if err != nil {
		t.Fatal("Failed creating initial configuration:", err)
	}

	t.Logf("Waiting for Configuration %q to transition to Ready with %d number of pods.", names.Config, wantPods)
	selector := fmt.Sprintf("%s=%s", serving.ConfigurationLabelKey, names.Config)
	if err := v1test.WaitForConfigurationState(clients.ServingClient, names.Config, func(s *v1.Configuration) (b bool, e error) {
		pods := clients.KubeClient.CoreV1().Pods(test.ServingNamespace)
		podList, err := pods.List(context.Background(), metav1.ListOptions{
			LabelSelector: selector,
			// Include both running and terminating pods, because we will scale down from
			// initial scale immediately if there's no traffic coming in.
			FieldSelector: "status.phase!=Pending",
		})
		if err != nil {
			return false, err
		}
		gotPods := len(podList.Items)
		switch {
		case gotPods == wantPods:
			return s.IsReady(), nil
		case gotPods > wantPods:
			return false, fmt.Errorf("expected %d pods created, got %d", wantPods, gotPods)
		default:
			return false, nil
		}
	}, "ConfigurationIsReadyWithWantPods"); err != nil {
		t.Fatal("Configuration does not have the desired number of pods running:", err)
	}
}
