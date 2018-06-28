/*
Copyright 2018 The Knative Authors.

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

package revision

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1"
	"github.com/google/go-containerregistry/v1/random"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "k8s.io/client-go/kubernetes/fake"
)

func mustDigest(t *testing.T, img v1.Image) v1.Hash {
	h, err := img.Digest()
	if err != nil {
		t.Fatalf("Digest() = %v", err)
	}
	return h
}

func mustRawManifest(t *testing.T, img v1.Image) []byte {
	m, err := img.RawManifest()
	if err != nil {
		t.Fatalf("RawManifest() = %v", err)
	}
	return m
}

func fakeRegistry(t *testing.T, repo, username, password string, img v1.Image) *httptest.Server {
	manifestPath := fmt.Sprintf("/v2/%s/manifests/latest", repo)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			// Issue a "Basic" auth challenge, so we can check the auth sent to the registry.
			w.Header().Set("WWW-Authenticate", `Basic `)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		case manifestPath:
			// Check that we get an auth header with base64 encoded username:password
			hdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(hdr, "Basic ") {
				t.Errorf("Header.Get(Authorization); got %v, want Basic prefix", hdr)
			}
			if want := base64.StdEncoding.EncodeToString([]byte(username + ":" + password)); !strings.HasSuffix(hdr, want) {
				t.Errorf("Header.Get(Authorization); got %v, want suffix %v", hdr, want)
			}
			if r.Method != http.MethodGet {
				t.Errorf("Method; got %v, want %v", r.Method, http.MethodGet)
			}
			w.Write(mustRawManifest(t, img))
		default:
			t.Fatalf("Unexpected path: %v", r.URL.Path)
		}
	}))
}

func fakeRegistryPingFailure(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			http.Error(w, "Oops", http.StatusInternalServerError)
		default:
			t.Fatalf("Unexpected path: %v", r.URL.Path)
		}
	}))
}

func fakeRegistryManifestFailure(t *testing.T, repo string) *httptest.Server {
	manifestPath := fmt.Sprintf("/v2/%s/manifests/latest", repo)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			// Issue a "Basic" auth challenge, so we can check the auth sent to the registry.
			w.Header().Set("WWW-Authenticate", `Basic `)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		case manifestPath:
			http.Error(w, "Boom", http.StatusInternalServerError)
		default:
			t.Fatalf("Unexpected path: %v", r.URL.Path)
		}
	}))
}

func TestResolve(t *testing.T) {
	username, password := "foo", "bar"
	ns, svcacct := "user-project", "user-robot"

	img, err := random.Image(3, 1024)
	if err != nil {
		t.Fatalf("random.Image() = %v", err)
	}

	// Stand up a fake registry
	expectedRepo := "booger/nose"
	server := fakeRegistry(t, expectedRepo, username, password, img)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}

	// Create a tag pointing to an image on our fake registry
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatalf("NewTag() = %v", err)
	}

	// Set up a fake service account with pull secrets for our fake registry
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: "secret",
		}},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: ns,
		},
		Type: corev1.SecretTypeDockercfg,
		Data: map[string][]byte{
			corev1.DockerConfigKey: []byte(
				fmt.Sprintf(`{%q: {"username": %q, "password": %q}}`,
					tag.RegistryStr(), username, password),
			),
		},
	})

	// Resolve our tag on the fake registry to the digest of the random.Image()
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: svcacct,
					Containers: []corev1.Container{{
						Name:  userContainerName,
						Image: tag.String(),
					}},
				},
			},
		},
	}
	if err := dr.Resolve(deploy); err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	// Make sure that we get back the appropriate digest.
	digest, err := name.NewDigest(deploy.Spec.Template.Spec.Containers[0].Image, name.WeakValidation)
	if err != nil {
		t.Fatalf("NewDigest() = %v", err)
	}
	if got, want := digest.DigestStr(), mustDigest(t, img).String(); got != want {
		t.Fatalf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolveWithDigest(t *testing.T) {
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "foo",
		},
	})
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	original := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: "foo",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "default",
					Containers: []corev1.Container{{
						Name:  userContainerName,
						Image: "ubuntu@sha256:e7def0d56013d50204d73bb588d99e0baa7d69ea1bc1157549b898eb67287612",
					}},
				},
			},
		},
	}
	deploy := original.DeepCopy()
	if err := dr.Resolve(deploy); err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	if diff := cmp.Diff(original, deploy); diff != "" {
		t.Errorf("Deployment should not change (-want +got): %s", diff)
	}
}

func TestResolveWithBadTag(t *testing.T) {
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: "foo",
		},
	})
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: "foo",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "default",
					Containers: []corev1.Container{{
						Name: userContainerName,
						// Invalid character
						Image: "ubuntu%latest",
					}},
				},
			},
		},
	}
	if err := dr.Resolve(deploy); err == nil {
		t.Fatalf("Resolve() = %v, want error", deploy)
	}
}

func TestResolveWithPingFailure(t *testing.T) {
	ns, svcacct := "user-project", "user-robot"

	// Stand up a fake registry
	expectedRepo := "booger/nose"
	server := fakeRegistryPingFailure(t)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}

	// Create a tag pointing to an image on our fake registry
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatalf("NewTag() = %v", err)
	}

	// Set up a fake service account with pull secrets for our fake registry
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
	})

	// Resolve our tag on the fake registry to the digest of the random.Image()
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: svcacct,
					Containers: []corev1.Container{{
						Name:  userContainerName,
						Image: tag.String(),
					}},
				},
			},
		},
	}
	if err := dr.Resolve(deploy); err == nil {
		t.Fatalf("Resolve() = %v, want error", deploy)
	}
}

func TestResolveWithManifestFailure(t *testing.T) {
	ns, svcacct := "user-project", "user-robot"

	// Stand up a fake registry
	expectedRepo := "booger/nose"
	server := fakeRegistryManifestFailure(t, expectedRepo)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}

	// Create a tag pointing to an image on our fake registry
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatalf("NewTag() = %v", err)
	}

	// Set up a fake service account with pull secrets for our fake registry
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
	})

	// Resolve our tag on the fake registry to the digest of the random.Image()
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: svcacct,
					Containers: []corev1.Container{{
						Name:  userContainerName,
						Image: tag.String(),
					}},
				},
			},
		},
	}
	if err := dr.Resolve(deploy); err == nil {
		t.Fatalf("Resolve() = %v, want error", deploy)
	}
}

func TestResolveNoAccess(t *testing.T) {
	client := fakeclient.NewSimpleClientset()
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blah",
			Namespace: "foo",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "default",
					Containers: []corev1.Container{{
						Name:  userContainerName,
						Image: "ubuntu:latest",
					}},
				},
			},
		},
	}
	// If there is a failure accessing the ServiceAccount for this Pod, then we should see an error.
	if err := dr.Resolve(deploy); err == nil {
		t.Fatalf("Resolve() = %v, want error", deploy)
	}
}
