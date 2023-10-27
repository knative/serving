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

package v1

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/apis/duck"
	"knative.dev/pkg/reconciler"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
)

// CreateConfiguration create a configuration resource in namespace with the name names.Config
// that uses the image specified by names.Image.
func CreateConfiguration(t testing.TB, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ConfigOption) (cfg *v1.Configuration, err error) {
	config := Configuration(names, fopt...)
	test.AddTestAnnotation(t, config.ObjectMeta)
	LogResourceObject(t, ResourceObjects{Config: config})
	return cfg, reconciler.RetryTestErrors(func(int) (err error) {
		cfg, err = clients.ServingClient.Configs.Create(context.Background(), config, metav1.CreateOptions{})
		return err
	})
}

// PatchConfig patches the existing configuration passed in with the applied mutations.
// Returns the latest configuration object
func PatchConfig(t testing.TB, clients *test.Clients, config *v1.Configuration, fopt ...rtesting.ConfigOption) (cfg *v1.Configuration, err error) {
	newConfig := config.DeepCopy()
	for _, opt := range fopt {
		opt(newConfig)
	}
	LogResourceObject(t, ResourceObjects{Config: newConfig})
	patchBytes, err := duck.CreateBytePatch(config, newConfig)
	if err != nil {
		return nil, err
	}
	return cfg, reconciler.RetryTestErrors(func(int) (err error) {
		cfg, err = clients.ServingClient.Configs.Patch(context.Background(), config.ObjectMeta.Name, types.JSONPatchType, patchBytes, metav1.PatchOptions{})
		return err
	})
}

// WaitForConfigLatestPinnedRevision enables the check for pinned revision in WaitForConfigLatestRevision.
func WaitForConfigLatestPinnedRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	return WaitForConfigLatestRevision(clients, names, true /*wait for pinned revision*/)
}

// WaitForConfigLatestUnpinnedRevision disables the check for pinned revision in WaitForConfigLatestRevision.
func WaitForConfigLatestUnpinnedRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	return WaitForConfigLatestRevision(clients, names, false /*wait for unpinned revision*/)
}

// WaitForConfigLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Configuration.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
// Make sure to enable ensurePinned flag if the revision has an associated Route.
func WaitForConfigLatestRevision(clients *test.Clients, names test.ResourceNames, ensurePinned bool) (string, error) {
	var revisionName string
	err := WaitForConfigurationState(clients.ServingClient, names.Config, func(c *v1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = c.Status.LatestCreatedRevisionName
			if ensurePinned {
				// Without this it might happen that the latest created revision is later overridden by a newer one
				// that is pinned and the following check for LatestReadyRevisionName would fail.
				return CheckRevisionState(clients.ServingClient, revisionName, IsRevisionRoutingActive) == nil, nil
			}
			return true, nil
		}
		return false, nil
	}, "ConfigurationUpdatedWithRevision")
	if err != nil {
		return "", fmt.Errorf("LatestCreatedRevisionName not updated: %w", err)
	}
	if err = WaitForConfigurationState(clients.ServingClient, names.Config, func(c *v1.Configuration) (bool, error) {
		return (c.Status.LatestReadyRevisionName == revisionName), nil
	}, "ConfigurationReadyWithRevision"); err != nil {
		return "", fmt.Errorf("LatestReadyRevisionName not updated with %s: %w", revisionName, err)
	}

	return revisionName, nil
}

// ConfigurationSpec returns the spec of a configuration to be used throughout different
// CRD helpers.
func ConfigurationSpec(imagePath string) *v1.ConfigurationSpec {
	c := &v1.ConfigurationSpec{
		Template: v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: imagePath,
					}},
				},
			},
		},
	}

	if !test.ServingFlags.DisableOptionalAPI {
		// Container.imagePullPolicy is not required by Knative
		// Serving API Specification.
		//
		// Kubernetes default pull policy is IfNotPresent unless
		// the :latest tag (== no tag) is used, in which case it
		// is Always.  To support e2e testing on KinD, we want to
		// explicitly disable image pulls when present because we
		// side-load the test images onto all nodes and never push
		// them to a registry.
		c.Template.Spec.PodSpec.Containers[0].ImagePullPolicy = corev1.PullIfNotPresent
	}

	return c
}

// Configuration returns a Configuration object in namespace with the name names.Config
// that uses the image specified by names.Image
func Configuration(names test.ResourceNames, fopt ...rtesting.ConfigOption) *v1.Configuration {
	config := &v1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Config,
		},
		Spec: *ConfigurationSpec(pkgTest.ImagePath(names.Image)),
	}

	for _, opt := range fopt {
		opt(config)
	}

	return config
}

// WaitForConfigurationState polls the status of the Configuration called name
// from client every PollInterval until inState returns `true` indicating it
// is done, returns an error or PollTimeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForConfigurationState(client *test.ServingClients, name string, inState func(c *v1.Configuration) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForConfigurationState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1.Configuration
	waitErr := wait.PollUntilContextTimeout(context.Background(), test.PollInterval, test.PollTimeout, true, func(context.Context) (bool, error) {
		err := reconciler.RetryTestErrors(func(int) (err error) {
			lastState, err = client.Configs.Get(context.Background(), name, metav1.GetOptions{})
			return err
		})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("configuration %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// CheckConfigurationState verifies the status of the Configuration called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForConfigurationState
func CheckConfigurationState(client *test.ServingClients, name string, inState func(r *v1.Configuration) (bool, error)) error {
	var c *v1.Configuration
	err := reconciler.RetryTestErrors(func(int) (err error) {
		c, err = client.Configs.Get(context.Background(), name, metav1.GetOptions{})
		return err
	})
	if err != nil {
		return err
	}
	if done, err := inState(c); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("configuration %q is not in desired state, got: %+v", name, c)
	}
	return nil
}

// IsConfigurationReady will check the status conditions of the config and return true if the config is
// ready. This means it has at least created one revision and that has become ready.
func IsConfigurationReady(c *v1.Configuration) (bool, error) {
	return c.IsReady(), nil
}

// GetConfigurations returns all the available configurations
func GetConfigurations(clients *test.Clients) (list *v1.ConfigurationList, err error) {
	return list, reconciler.RetryTestErrors(func(int) (err error) {
		list, err = clients.ServingClient.Configs.List(context.Background(), metav1.ListOptions{})
		return err
	})
}
