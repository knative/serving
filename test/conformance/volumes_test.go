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

package conformance

import (
	"path/filepath"
	"testing"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConfigMapVolume tests that we echo back the appropriate text from the ConfigMap volume.
func TestConfigMapVolume(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(t.Name())

	names := test.ResourceNames{
		Service: test.AppendRandomString("cm-volume-", logger),
		Image:   "hellovolume",
	}

	text := test.AppendRandomString("hello-volumes-", logger)

	// Create the ConfigMap with random text.
	configMap, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(test.ServingNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // Give it the same name as the service.
		},
		Data: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create configmap: %v", err)
	}
	logger.Info("Successfully created configMap: %v", configMap)

	cleanup := func() {
		tearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().ConfigMaps(test.ServingNamespace).Delete(configMap.Name, nil); err != nil {
			t.Errorf("ConfigMaps().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup, logger)

	addVolume := func(svc *v1alpha1.Service) {
		rt := &svc.Spec.RunLatest.Configuration.RevisionTemplate.Spec

		rt.Container.VolumeMounts = []corev1.VolumeMount{{
			Name:      "asdf",
			MountPath: filepath.Dir(test.HelloVolumePath),
		}}

		rt.Volumes = []corev1.Volume{{
			Name: "asdf",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMap.Name,
					},
				},
			},
		}}
	}

	// Setup initial Service
	if _, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{}, addVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(logger, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(logger, clients, names, text); err != nil {
		t.Error(err)
	}
}

// TestSecretVolume tests that we echo back the appropriate text from the Secret volume.
func TestSecretVolume(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(t.Name())

	names := test.ResourceNames{
		Service: test.AppendRandomString("cm-volume-", logger),
		Image:   "hellovolume",
	}

	text := test.AppendRandomString("hello-volumes-", logger)

	// Create the Secret with random text.
	secret, err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // name the Secret the same as the Service.
		},
		StringData: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}
	logger.Info("Successfully created secret: %v", secret)

	cleanup := func() {
		tearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(secret.Name, nil); err != nil {
			t.Errorf("Secrets().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup, logger)

	addVolume := func(svc *v1alpha1.Service) {
		rt := &svc.Spec.RunLatest.Configuration.RevisionTemplate.Spec

		rt.Container.VolumeMounts = []corev1.VolumeMount{{
			Name:      "asdf",
			MountPath: filepath.Dir(test.HelloVolumePath),
		}}

		rt.Volumes = []corev1.Volume{{
			Name: "asdf",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secret.Name,
				},
			},
		}}
	}

	// Setup initial Service
	if _, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{}, addVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation

	if err = validateRunLatestControlPlane(logger, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(logger, clients, names, text); err != nil {
		t.Error(err)
	}
}
