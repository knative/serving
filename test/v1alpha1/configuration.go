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

// configuration.go provides methods to perform actions on the configuration object.

package v1alpha1

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/test/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"

	ptest "knative.dev/pkg/test"
	rtesting "knative.dev/serving/pkg/testing/v1alpha1"
	v1alpha1testing "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

const (
	interval = 1 * time.Second
	timeout  = 10 * time.Minute
)

// CreateConfiguration create a configuration resource in namespace with the name names.Config
// that uses the image specified by names.Image.
func CreateConfiguration(t *testing.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.ConfigOption) (*v1alpha1.Configuration, error) {
	config := Configuration(names, fopt...)
	LogResourceObject(t, ResourceObjects{Config: config})
	return clients.ServingAlphaClient.Configs.Create(config)
}

// PatchConfigImage patches the existing config passed in with a new imagePath. Returns the latest Configuration object
func PatchConfigImage(clients *test.Clients, cfg *v1alpha1.Configuration, imagePath string) (*v1alpha1.Configuration, error) {
	newCfg := cfg.DeepCopy()
	newCfg.Spec.GetTemplate().Spec.GetContainer().Image = imagePath
	patchBytes, err := createPatch(cfg, newCfg)
	if err != nil {
		return nil, err
	}
	return clients.ServingAlphaClient.Configs.Patch(cfg.ObjectMeta.Name, types.JSONPatchType, patchBytes, "")
}

// WaitForConfigLatestRevision takes a revision in through names and compares it to the current state of LatestCreatedRevisionName in Configuration.
// Once an update is detected in the LatestCreatedRevisionName, the function waits for the created revision to be set in LatestReadyRevisionName
// before returning the name of the revision.
func WaitForConfigLatestRevision(clients *test.Clients, names test.ResourceNames) (string, error) {
	var revisionName string
	err := WaitForConfigurationState(clients.ServingAlphaClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		if c.Status.LatestCreatedRevisionName != names.Revision {
			revisionName = c.Status.LatestCreatedRevisionName
			return true, nil
		}
		return false, nil
	}, "ConfigurationUpdatedWithRevision")
	if err != nil {
		return "", err
	}
	err = WaitForConfigurationState(clients.ServingAlphaClient, names.Config, func(c *v1alpha1.Configuration) (bool, error) {
		return (c.Status.LatestReadyRevisionName == revisionName), nil
	}, "ConfigurationReadyWithRevision")

	return revisionName, err
}

// ConfigurationSpec returns the spec of a configuration to be used throughout different
// CRD helpers.
func ConfigurationSpec(imagePath string) *v1alpha1.ConfigurationSpec {
	return &v1alpha1.ConfigurationSpec{
		Template: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1.RevisionSpec{
					PodSpec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Image: imagePath,
						}},
					},
				},
			},
		},
	}
}

// LegacyConfigurationSpec returns the spec of a configuration to be used throughout different
// CRD helpers.
func LegacyConfigurationSpec(imagePath string) *v1alpha1.ConfigurationSpec {
	return &v1alpha1.ConfigurationSpec{
		DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: imagePath,
				},
				RevisionSpec: v1.RevisionSpec{},
			},
		},
	}
}

// Configuration returns a Configuration object in namespace with the name names.Config
// that uses the image specified by names.Image
func Configuration(names test.ResourceNames, fopt ...v1alpha1testing.ConfigOption) *v1alpha1.Configuration {
	config := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Config,
		},
		Spec: *ConfigurationSpec(ptest.ImagePath(names.Image)),
	}

	for _, opt := range fopt {
		opt(config)
	}

	return config
}

// WaitForConfigurationState polls the status of the Configuration called name
// from client every interval until inState returns `true` indicating it
// is done, returns an error or timeout. desc will be used to name the metric
// that is emitted to track how long it took for name to get into the state checked by inState.
func WaitForConfigurationState(client *test.ServingAlphaClients, name string, inState func(c *v1alpha1.Configuration) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForConfigurationState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Configuration
	waitErr := wait.PollImmediate(interval, timeout, func() (bool, error) {
		var err error
		lastState, err = client.Configs.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return errors.Wrapf(waitErr, "configuration %q is not in desired state, got: %+v", name, lastState)
	}
	return nil
}

// CheckConfigurationState verifies the status of the Configuration called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForConfigurationState
func CheckConfigurationState(client *test.ServingAlphaClients, name string, inState func(r *v1alpha1.Configuration) (bool, error)) error {
	c, err := client.Configs.Get(name, metav1.GetOptions{})
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

// ConfigurationHasCreatedRevision returns whether the Configuration has created a Revision.
func ConfigurationHasCreatedRevision(c *v1alpha1.Configuration) (bool, error) {
	return c.Status.LatestCreatedRevisionName != "", nil
}

// IsConfigRevisionCreationFailed will check the status conditions of the
// configuration and return true if the configuration's revision failed to
// create.
func IsConfigRevisionCreationFailed(c *v1alpha1.Configuration) (bool, error) {
	if cond := c.Status.GetCondition(v1alpha1.ConfigurationConditionReady); cond != nil {
		return cond.Status == corev1.ConditionFalse && cond.Reason == "RevisionFailed", nil
	}
	return false, nil
}
