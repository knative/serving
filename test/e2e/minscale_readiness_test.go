// +build e2e

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
	"strconv"
	"testing"

	"github.com/knative/serving/pkg/apis/autoscaling"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMinScale(t *testing.T) {
	const minScale = 4

	clients := Setup(t)

	names := test.ResourceNames{
		Config: test.ObjectNameForTest(t),
		Image:  "helloworld",
	}

	if _, err := test.CreateConfiguration(t, clients, names, &test.Options{}, func(cfg *v1alpha1.Configuration) {
		if cfg.Spec.Template.Annotations == nil {
			cfg.Spec.Template.Annotations = make(map[string]string)
		}

		cfg.Spec.Template.Annotations[autoscaling.MinScaleAnnotationKey] = strconv.Itoa(minScale)

	}); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	// Wait for the Config have a LatestCreatedRevisionName
	if err := test.WaitForConfigurationState(clients.ServingClient, names.Config, test.ConfigurationHasCreatedRevision, "ConfigurationHasCreatedRevision"); err != nil {
		t.Fatalf("The Configuration %q does not have a LatestCreatedRevisionName: %v", names.Config, err)
	}

	config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
	}

	revName := config.Status.LatestCreatedRevisionName

	if err = test.WaitForRevisionState(clients.ServingClient, revName, test.IsRevisionReady, "RevisionIsReady"); err != nil {
		t.Fatal("Revision did not become ready.")
	}

	deployment, err := clients.KubeClient.Kube.ExtensionsV1beta1().Deployments(test.ServingNamespace).Get(revName+"-deployment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Deployment for Revision %s, err: %v", revName, err)
	}

	if deployment.Status.AvailableReplicas < int32(minScale) {
		t.Fatalf("Reported ready with %d replicas when minScale was %d", deployment.Status.AvailableReplicas, minScale)
	}
}
