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

package v1

import (
	"context"
	"path"
	"path/filepath"
	"testing"

	"github.com/form3tech-oss/jwt-go"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	v1test "knative.dev/serving/test/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "knative.dev/serving/pkg/testing/v1"
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
	configMap, err := clients.KubeClient.CoreV1().ConfigMaps(test.ServingNamespace).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // Give it the same name as the service.
		},
		Data: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create configmap:", err)
	}
	t.Log("Successfully created configMap:", configMap)

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().ConfigMaps(test.ServingNamespace).Delete(context.Background(), configMap.Name, metav1.DeleteOptions{}); err != nil {
			t.Error("ConfigMaps().Delete() =", err)
		}
	})

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
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
	if _, err := v1test.CreateServiceReady(t, clients, &names, withVolume, withOptionalBadVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateDataPlane(t, clients, names, text); err != nil {
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
	configMap, err := clients.KubeClient.CoreV1().ConfigMaps(test.ServingNamespace).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // Give it the same name as the service.
		},
		Data: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create configmap:", err)
	}
	t.Log("Successfully created configMap:", configMap)

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().ConfigMaps(test.ServingNamespace).Delete(context.Background(), configMap.Name, metav1.DeleteOptions{}); err != nil {
			t.Error("ConfigMaps().Delete() =", err)
		}
	})

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
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
	if _, err := v1test.CreateServiceReady(t, clients, &names, withVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateDataPlane(t, clients, names, text); err != nil {
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
	secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // name the Secret the same as the Service.
		},
		StringData: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create secret:", err)
	}
	t.Log("Successfully created secret:", secret)

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{}); err != nil {
			t.Error("Secrets().Delete() =", err)
		}
	})

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
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
	if _, err := v1test.CreateServiceReady(t, clients, &names, withVolume, withOptionalBadVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateDataPlane(t, clients, names, text); err != nil {
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
	secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // name the Secret the same as the Service.
		},
		StringData: map[string]string{
			filepath.Base(test.HelloVolumePath): text,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create secret:", err)
	}
	t.Log("Successfully created secret:", secret)

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{}); err != nil {
			t.Error("Secrets().Delete() =", err)
		}
	})

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
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
	withSubpath := func(svc *v1.Service) {
		vm := &svc.Spec.Template.Spec.Containers[0].VolumeMounts[0]
		vm.MountPath = test.HelloVolumePath
		vm.SubPath = filepath.Base(test.HelloVolumePath)
	}

	// Setup initial Service
	if _, err := v1test.CreateServiceReady(t, clients, &names, withVolume, withSubpath); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	if err = validateDataPlane(t, clients, names, text); err != nil {
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
	configMap, err := clients.KubeClient.CoreV1().ConfigMaps(test.ServingNamespace).Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // Give it the same name as the service.
		},
		Data: map[string]string{
			filepath.Base(test.HelloVolumePath): text1,
			"other":                             text2,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create configmap:", err)
	}
	t.Log("Successfully created configMap:", configMap)

	// Create the Secret with random text.
	secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Service, // name the Secret the same as the Service.
		},
		StringData: map[string]string{
			filepath.Base(test.HelloVolumePath): text3,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Failed to create secret:", err)
	}
	t.Log("Successfully created secret:", secret)

	// Clean up on test failure or interrupt
	test.EnsureCleanup(t, func() {
		test.TearDown(clients, &names)
		if err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{}); err != nil {
			t.Error("Secrets().Delete() =", err)
		}
	})

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
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
	if _, err := v1test.CreateServiceReady(t, clients, &names, withVolume); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err = validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}

	// Observation shows that when keys collide, the last source listed wins,
	// so for the main key, we should get back text3 (vs. text1)
	if err = validateDataPlane(t, clients, names, text3); err != nil {
		t.Error(err)
	}

	// Verify that we get multiple files mounted in, in this case from the
	// second source, which was partially shadowed in our check above.
	names.URL.Path = path.Join(names.URL.Path, "another")
	if err = validateDataPlane(t, clients, names, text2); err != nil {
		t.Error(err)
	}
}

// TestProjectedServiceAccountToken tests that a valid JWT service account token can be mounted.
func TestProjectedServiceAccountToken(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "hellovolume",
	}

	const tokenPath = "token"
	saPath := filepath.Join(filepath.Dir(test.HelloVolumePath), tokenPath)

	withVolume := WithVolume("projectedv", filepath.Dir(test.HelloVolumePath), corev1.VolumeSource{
		Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{
				ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					Audience: "api",
					Path:     tokenPath,
				},
			}},
		},
	})
	withSubpath := func(svc *v1.Service) {
		vm := &svc.Spec.Template.Spec.Containers[0].VolumeMounts[0]
		vm.MountPath = saPath
		vm.SubPath = filepath.Base(saPath)
	}

	withRunAsUser := func(svc *v1.Service) {
		svc.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			// The token will be mounted owned by root, so we need the container to
			// run as root to be able to read it.
			RunAsUser: ptr.Int64(0),
		}
	}

	serviceOpts := []ServiceOption{withVolume, withSubpath, withRunAsUser}

	// Setup initial Service
	if _, err := v1test.CreateServiceReady(t, clients, &names, serviceOpts...); err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate State after Creation
	if err := validateControlPlane(t, clients, names, "1"); err != nil {
		t.Error(err)
	}
	names.URL.Path = path.Join(names.URL.Path, tokenPath)
	var parsesToken = func(resp *spoof.Response) (bool, error) {
		jwtToken := string(resp.Body)
		parser := &jwt.Parser{}
		if _, _, err := parser.ParseUnverified(jwtToken, jwt.MapClaims{}); err != nil {
			return false, err
		}
		return true, nil
	}

	if _, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		names.URL,
		v1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, parsesToken)),
		"WaitForEndpointToServeTheToken",
		test.ServingFlags.ResolvableDomain,
		test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS)); err != nil {
		t.Error(err)
	}
}
