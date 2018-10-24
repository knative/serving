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
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeclient "k8s.io/client-go/kubernetes/fake"
)

var emptyRegistrySet = map[string]struct{}{}

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
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	resolvedDigest, err := dr.Resolve(tag.String(), opt, emptyRegistrySet)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	// Make sure that we get back the appropriate digest.
	digest, err := name.NewDigest(resolvedDigest, name.WeakValidation)
	if err != nil {
		t.Fatalf("NewDigest() = %v", err)
	}
	if got, want := digest.DigestStr(), mustDigest(t, img).String(); got != want {
		t.Fatalf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolveWithDigest(t *testing.T) {
	ns, svcacct := "foo", "default"
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns,
		},
	})
	originalDigest := "ubuntu@sha256:e7def0d56013d50204d73bb588d99e0baa7d69ea1bc1157549b898eb67287612"
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	resolvedDigest, err := dr.Resolve(originalDigest, opt, emptyRegistrySet)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	if diff := cmp.Diff(originalDigest, resolvedDigest); diff != "" {
		t.Errorf("Digest should not change (-want +got): %s", diff)
	}
}

func TestResolveWithBadTag(t *testing.T) {
	ns, svcacct := "foo", "default"
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns,
		},
	})
	dr := &digestResolver{client: client, transport: http.DefaultTransport}

	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}

	// Invalid character
	invalidImage := "ubuntu%latest"
	if resolvedDigest, err := dr.Resolve(invalidImage, opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
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
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	if resolvedDigest, err := dr.Resolve(tag.String(), opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
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
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	if resolvedDigest, err := dr.Resolve(tag.String(), opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
	}
}

func TestResolveNoAccess(t *testing.T) {
	ns, svcacct := "foo", "default"
	client := fakeclient.NewSimpleClientset()
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	// If there is a failure accessing the ServiceAccount for this Pod, then we should see an error.
	if resolvedDigest, err := dr.Resolve("ubuntu:latest", opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
	}
}

func TestResolveSkippingRegistry(t *testing.T) {
	username, password := "foo", "bar"
	ns, svcacct := "user-project", "user-robot"

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
					"localhost:5000", username, password),
			),
		},
	})
	dr := &digestResolver{
		client:    client,
		transport: http.DefaultTransport,
	}

	registriesToSkip := map[string]struct{}{
		"localhost:5000": {},
	}

	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}

	resolvedDigest, err := dr.Resolve("localhost:5000/ubuntu:latest", opt, registriesToSkip)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	if got, want := resolvedDigest, ""; got != want {
		t.Fatalf("Resolve() got %q want of %q", got, want)
	}
}
