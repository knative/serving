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

package v1alpha1

import (
	"path"
	"path/filepath"
	"testing"

	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	v1a1test "knative.dev/serving/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "knative.dev/serving/pkg/testing/v1alpha1"
)

// TestConfigMapVolume tests that we echo back the appropriate text from the ConfigMap volume.
func TestConfigMapVolume(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloVolume,
	}

	text := test.AppendRandomString("hello-volumes-")

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
	t.Logf("Successfully created configMap: %v", configMap)

	cleanup := func() {
		test.TearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().ConfigMaps(test.ServingNamespace).Delete(configMap.Name, nil); err != nil {
			t.Errorf("ConfigMaps().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup)

	withVolume := WithVolume("asdf", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: configMap.Name,
			},
			Optional: ptr.Bool(false),
		},
	})

	withOptionalBadVolume := WithVolume("blah", "/does/not/matter", corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "does-not-exist",
			},
			Optional: ptr.Bool(true),
		},
	})

	// Setup initial Service
	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withVolume, withOptionalBadVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, text); err != nil {
		t.Error(err)
	}
}

// TestProjectedConfigMapVolume tests that we echo back the appropriate text from the ConfigMap volume.
func TestProjectedConfigMapVolume(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellovolume",
	}

	text := test.AppendRandomString("hello-volumes-")

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
	t.Logf("Successfully created configMap: %v", configMap)

	cleanup := func() {
		test.TearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().ConfigMaps(test.ServingNamespace).Delete(configMap.Name, nil); err != nil {
			t.Errorf("ConfigMaps().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup)

	withVolume := WithVolume("asdf", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{
				ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMap.Name,
					},
					Optional: ptr.Bool(false),
				},
			}, {
				ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "does-not-matter",
					},
					Optional: ptr.Bool(true),
				},
			}},
		},
	})

	// Setup initial Service
	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, text); err != nil {
		t.Error(err)
	}
}

// TestSecretVolume tests that we echo back the appropriate text from the Secret volume.
func TestSecretVolume(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.HelloVolume,
	}

	text := test.ObjectNameForTest(t)

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
	t.Logf("Successfully created secret: %v", secret)

	cleanup := func() {
		test.TearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(secret.Name, nil); err != nil {
			t.Errorf("Secrets().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup)

	withVolume := WithVolume("asdf", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: secret.Name,
			Optional:   ptr.Bool(false),
		},
	})

	withOptionalBadVolume := WithVolume("blah", "/does/not/matter", corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: "does-not-exist",
			Optional:   ptr.Bool(true),
		},
	})

	// Setup initial Service
	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withVolume, withOptionalBadVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, text); err != nil {
		t.Error(err)
	}
}

// TestProjectedSecretVolume tests that we echo back the appropriate text from the Secret volume.
func TestProjectedSecretVolume(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellovolume",
	}

	text := test.ObjectNameForTest(t)

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
	t.Logf("Successfully created secret: %v", secret)

	cleanup := func() {
		test.TearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(secret.Name, nil); err != nil {
			t.Errorf("Secrets().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup)

	withVolume := WithVolume("asdf", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secret.Name,
					},
					Optional: ptr.Bool(false),
				},
			}},
		},
	})
	withSubpath := func(svc *v1alpha1.Service) {
		vm := &svc.Spec.Template.Spec.Containers[0].VolumeMounts[0]
		vm.MountPath = test.HelloVolumePath
		vm.SubPath = filepath.Base(test.HelloVolumePath)
	}

	// Setup initial Service
	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withVolume, withSubpath); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateRunLatestDataPlane(t, clients, names, text); err != nil {
		t.Error(err)
	}
}

// TestProjectedComplex tests that we echo back the appropriate text from the complex Projected volume.
func TestProjectedComplex(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellovolume",
	}

	text1 := test.ObjectNameForTest(t)
	text2 := test.ObjectNameForTest(t)
	text3 := test.ObjectNameForTest(t)

	// Create the ConfigMap with random text.
	configMap, err := clients.KubeClient.Kube.CoreV1().ConfigMaps(test.ServingNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // Give it the same name as the service.
		},
		Data: map[string]string{
			filepath.Base(test.HelloVolumePath): text1,
			"other":                             text2,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create configmap: %v", err)
	}
	t.Logf("Successfully created configMap: %v", configMap)

	// Create the Secret with random text.
	secret, err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // name the Secret the same as the Service.
		},
		StringData: map[string]string{
			filepath.Base(test.HelloVolumePath): text3,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}
	t.Logf("Successfully created secret: %v", secret)

	cleanup := func() {
		test.TearDown(clients, names)
		if err := clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(secret.Name, nil); err != nil {
			t.Errorf("Secrets().Delete() = %v", err)
		}
	}

	// Clean up on test failure or interrupt
	defer cleanup()
	test.CleanupOnInterrupt(cleanup)

	withVolume := WithVolume("asdf", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{
				ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMap.Name,
					},
					Items: []corev1.KeyToPath{{
						Key:  "other",
						Path: "another",
					}},
					Optional: ptr.Bool(false),
				},
			}, {
				Secret: &corev1.SecretProjection{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secret.Name,
					},
					Items: []corev1.KeyToPath{{
						Key:  filepath.Base(test.HelloVolumePath),
						Path: filepath.Base(test.HelloVolumePath),
					}},
					Optional: ptr.Bool(false),
				},
			}},
		},
	})

	// Setup initial Service
	if _, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
		false, /* https TODO(taragu) turn this on after helloworld test running with https */
		withVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateRunLatestControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	// Observation shows that when keys collide, the last source listed wins,
	// so for the main key, we should get back text3 (vs. text1)
	if err = validateRunLatestDataPlane(t, clients, names, text3); err != nil {
		t.Error(err)
	}

	// Verify that we get multiple files mounted in, in this case from the
	// second source, which was partially shadowed in our check above.
	names.URL.Path = path.Join(names.URL.Path, "another")
	if err = validateRunLatestDataPlane(t, clients, names, text2); err != nil {
		t.Error(err)
	}
}
