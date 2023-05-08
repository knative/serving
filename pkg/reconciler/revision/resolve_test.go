/*
Copyright 2018 The Knative Authors

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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fakeclient "k8s.io/client-go/kubernetes/fake"
)

var emptyRegistrySet = sets.NewString()

func mustDigest(t *testing.T, img v1.Image) v1.Hash {
	h, err := img.Digest()
	if err != nil {
		t.Fatal("Digest() =", err)
	}
	return h
}

func fakeRegistry(t *testing.T, repo, username, password, ua string, img v1.Image) *httptest.Server {
	manifestPath := fmt.Sprintf("/v2/%s/manifests/latest", repo)
	const basicAuth = "Basic "
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), ua; !strings.Contains(got, want) {
			t.Errorf("Header.Get(User-Agent) = %q, want Contains(%q)", got, want)
		}
		switch r.URL.Path {
		case "/v2/":
			// Issue a "Basic" auth challenge, so we can check the auth sent to the registry.
			w.Header().Set("WWW-Authenticate", basicAuth)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		case manifestPath:
			// Check that we get an auth header with base64 encoded username:password
			hdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(hdr, basicAuth) {
				t.Errorf("Header.Get(Authorization) = %q, want %q prefix", hdr, basicAuth)
			}
			if want := base64.StdEncoding.EncodeToString([]byte(username + ":" + password)); !strings.HasSuffix(hdr, want) {
				t.Errorf("Header.Get(Authorization) = %q, want suffix %q", hdr, want)
			}
			if r.Method != http.MethodHead {
				t.Errorf("Method = %v, want %v", r.Method, http.MethodHead)
			}
			mt, err := img.MediaType()
			if err != nil {
				t.Error("MediaType() =", err)
			}
			sz, err := img.Size()
			if err != nil {
				t.Error("Size() =", err)
			}
			digest, err := img.Digest()
			if err != nil {
				t.Error("Digest() =", err)
			}
			w.Header().Set("Content-Type", string(mt))
			w.Header().Set("Content-Length", fmt.Sprint(sz))
			w.Header().Set("Docker-Content-Digest", digest.String())
		default:
			t.Error("Unexpected path:", r.URL.Path)
		}
	}))
}

func fakeRegistryPingFailure(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			http.Error(w, "Oops", http.StatusInternalServerError)
		default:
			t.Error("Unexpected path:", r.URL.Path)
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
			t.Error("Unexpected path:", r.URL.Path)
		}
	}))
}

func fakeRegistryBlocking(t *testing.T) (ts *httptest.Server, cancel func()) {
	ch := make(chan struct{})
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.Header().Set("WWW-Authenticate", `Basic `)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		default:
			<-ch
		}
	}))

	return ts, func() {
		close(ch)
		ts.Close()
	}
}

func TestResolve(t *testing.T) {
	const (
		ns           = "user-project"
		svcacct      = "user-robot"
		username     = "foo"
		password     = "bar"
		sname        = "secret"
		expectedRepo = "booger/nose"
		ua           = "unique-identifier"
	)

	img, err := random.Image(3, 1024)
	if err != nil {
		t.Fatal("random.Image() =", err)
	}

	// Stand up a fake registry.
	server := fakeRegistry(t, expectedRepo, username, password, ua, img)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("url.Parse() =", err)
	}

	// Create a tag pointing to an image on our fake registry.
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	// Set up a fake service account with pull secrets for our fake registry.
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: sname,
		}},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sname,
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

	// Resolve our tag on the fake registry to the digest of the random.Image().
	dr := &digestResolver{client: client, transport: http.DefaultTransport, userAgent: ua}
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}
	resolvedDigest, err := dr.Resolve(context.Background(), tag.String(), opt, emptyRegistrySet)
	if err != nil {
		t.Fatal("Resolve() =", err)
	}

	// Make sure that we get back the appropriate digest.
	digest, err := name.NewDigest(resolvedDigest, name.WeakValidation)
	if err != nil {
		t.Fatal("NewDigest() =", err)
	}
	if got, want := digest.DigestStr(), mustDigest(t, img).String(); got != want {
		t.Fatalf("Resolve() = %v, want %v", got, want)
	}
}

func TestResolveWithDigest(t *testing.T) {
	const (
		ns      = "foo"
		svcacct = "default"
	)
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
	resolvedDigest, err := dr.Resolve(context.Background(), originalDigest, opt, emptyRegistrySet)
	if err != nil {
		t.Fatal("Resolve() =", err)
	}

	if diff := cmp.Diff(originalDigest, resolvedDigest); diff != "" {
		t.Errorf("Digest should not change (-want +got):\n%s", diff)
	}
}

func TestResolveWithBadTag(t *testing.T) {
	const (
		ns      = "foo"
		svcacct = "default"
	)
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
	})
	dr := &digestResolver{client: client, transport: http.DefaultTransport}

	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}

	// Invalid character.
	invalidImage := "ubuntu%latest"
	if resolvedDigest, err := dr.Resolve(context.Background(), invalidImage, opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() succeeded with %q, want error", resolvedDigest)
	}
}

func TestResolveWithPingFailure(t *testing.T) {
	const (
		ns           = "user-project"
		svcacct      = "user-robot"
		expectedRepo = "booger/nose"
	)
	server := fakeRegistryPingFailure(t)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("url.Parse() =", err)
	}

	// Create a tag pointing to an image on our fake registry
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatal("NewTag() =", err)
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
	if resolvedDigest, err := dr.Resolve(context.Background(), tag.String(), opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
	}
}

func TestResolveWithManifestFailure(t *testing.T) {
	const (
		ns           = "user-project"
		svcacct      = "user-robot"
		expectedRepo = "booger/nose"
	)

	// Stand up a fake registry
	server := fakeRegistryManifestFailure(t, expectedRepo)
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("url.Parse() =", err)
	}

	// Create a tag pointing to an image on our fake registry
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo), name.WeakValidation)
	if err != nil {
		t.Fatal("NewTag() =", err)
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
	if resolvedDigest, err := dr.Resolve(context.Background(), tag.String(), opt, emptyRegistrySet); err == nil {
		t.Fatalf("Resolve() = %v, want error", resolvedDigest)
	}
}

func TestResolveTimeout(t *testing.T) {
	// Stand up a fake registry which blocks until cancelled.
	server, cancel := fakeRegistryBlocking(t)
	t.Cleanup(cancel)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("url.Parse() =", err)
	}

	// Create a tag pointing to an image on our fake registry.
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, "doesnt/matter"), name.WeakValidation)
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	// Set up a fake service account with pull secrets for our fake registry.
	const (
		ns      = "user-project"
		svcacct = "user-robot"
	)
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
	})

	// Time out after 200ms (long enough to be sure we're testing cancelling of
	// digest lookup, rather than just credential lookup).
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)

	// Resolve the digest. The endpoint will never resolve, but we
	// should give up anyway due the context timeout above.
	dr := &digestResolver{client: client, transport: http.DefaultTransport}
	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}

	_, err = dr.Resolve(ctx, tag.String(), opt, emptyRegistrySet)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Expected Resolve() to fail via timeout, but failed with", err)
	}
}

func TestResolveSkippingRegistry(t *testing.T) {
	const (
		ns       = "user-project"
		svcacct  = "user-robot"
		username = "foo"
		password = "bar"
		name     = "secret"
	)

	// Set up a fake service account with pull secrets for our fake registry
	client := fakeclient.NewSimpleClientset(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcacct,
			Namespace: ns,
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: name,
		}},
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
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

	registriesToSkip := sets.NewString("localhost:5000")

	opt := k8schain.Options{
		Namespace:          ns,
		ServiceAccountName: svcacct,
	}

	resolvedDigest, err := dr.Resolve(context.Background(), "localhost:5000/ubuntu:latest", opt, registriesToSkip)
	if err != nil {
		t.Fatal("Resolve() =", err)
	}

	if got, want := resolvedDigest, ""; got != want {
		t.Fatalf("Resolve() got %q want of %q", got, want)
	}
}

func TestNewResolverTransport(t *testing.T) {
	cases := []struct {
		name               string
		certBundle         string
		certBundleContents []byte

		wantErr bool
	}{{
		name:               "valid cert",
		certBundle:         "valid-cert.crt",
		certBundleContents: []byte(certPEM),
		wantErr:            false,
	}, {
		// Fails with file not found for path.
		name:               "cert not found",
		certBundle:         "not-found.crt",
		certBundleContents: nil,
		wantErr:            true,
	}, {
		// Fails with invalid cert for path.
		name:               "invalid cert",
		certBundle:         "invalid-cert.crt",
		certBundleContents: nil,
		wantErr:            true,
	}}

	tmpDir := t.TempDir()

	for i, tc := range cases {
		i, tc := i, tc
		t.Run(fmt.Sprintf("cases[%d]", i), func(t *testing.T) {
			// Setup.
			path, err := writeCertFile(tmpDir, tc.certBundle, tc.certBundleContents)
			if err != nil {
				t.Fatal("Failed to write cert bundle file:", err)
			}

			// The actual test.
			if tr, err := newResolverTransport(path, 100, 100); err != nil && !tc.wantErr {
				t.Error("Got unexpected err:", err)
			} else if tc.wantErr && err == nil {
				t.Error("Didn't get an error when we wanted it")
			} else if err == nil {

				// If we didn't get an error, make sure everything we wanted to happen happened.
				//nolint:staticcheck // ignore deprecation since we're not asserting system roots
				subjects := tr.TLSClientConfig.RootCAs.Subjects()

				if !containsSubject(t, subjects, tc.certBundleContents) {
					t.Error("Cert pool does not contain certBundleContents")
				}
			}
		})
	}
}
func TestNewResolverTransport_TLSMinVersion(t *testing.T) {
	cases := []struct {
		name           string
		envOverride    string
		expectedMinTLS uint16
		expectedPanic  bool
	}{{
		name:           "TLS 1.2",
		envOverride:    "1.2",
		expectedMinTLS: tls.VersionTLS12,
	}, {
		name:           "TLS 1.3",
		envOverride:    "1.3",
		expectedMinTLS: tls.VersionTLS13,
	}, {
		name:           "default TLS 1.2",
		envOverride:    "",
		expectedMinTLS: tls.VersionTLS12,
	}}

	tmpDir := t.TempDir()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tlsMinVersionEnvKey, tc.envOverride)

			// noop for this test
			path, err := writeCertFile(tmpDir, "cert.pem", []byte(certPEM))
			if err != nil {
				t.Fatal("Failed to write cert bundle file:", err)
			}

			// The actual test.
			if tr, err := newResolverTransport(path, 100, 100); err != nil {
				t.Error("Got unexpected err:", err)
			} else if err == nil {

				if diff := cmp.Diff(tc.expectedMinTLS, tr.TLSClientConfig.MinVersion); diff != "" {
					t.Errorf("expected min TLS version does not match: %s", diff)
				}
			}
		})
	}
}

func writeCertFile(dir, path string, contents []byte) (string, error) {
	fp := filepath.Join(dir, path)
	if contents != nil {
		if err := os.WriteFile(fp, contents, os.ModePerm); err != nil {
			return "", err
		}
	}
	return fp, nil
}

func containsSubject(t *testing.T, subjects [][]byte, contents []byte) bool {
	block, _ := pem.Decode(contents)
	if block == nil {
		t.Fatal("Failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal("Failed to parse certificate:", err)
	}

	for _, b := range subjects {
		if bytes.EqualFold(b, cert.RawSubject) {
			return true
		}
	}

	return false
}

// Cert stolen from crypto/x509/example_test.go
const certPEM = `
-----BEGIN CERTIFICATE-----
MIIDujCCAqKgAwIBAgIIE31FZVaPXTUwDQYJKoZIhvcNAQEFBQAwSTELMAkGA1UE
BhMCVVMxEzARBgNVBAoTCkdvb2dsZSBJbmMxJTAjBgNVBAMTHEdvb2dsZSBJbnRl
cm5ldCBBdXRob3JpdHkgRzIwHhcNMTQwMTI5MTMyNzQzWhcNMTQwNTI5MDAwMDAw
WjBpMQswCQYDVQQGEwJVUzETMBEGA1UECAwKQ2FsaWZvcm5pYTEWMBQGA1UEBwwN
TW91bnRhaW4gVmlldzETMBEGA1UECgwKR29vZ2xlIEluYzEYMBYGA1UEAwwPbWFp
bC5nb29nbGUuY29tMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEfRrObuSW5T7q
5CnSEqefEmtH4CCv6+5EckuriNr1CjfVvqzwfAhopXkLrq45EQm8vkmf7W96XJhC
7ZM0dYi1/qOCAU8wggFLMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjAa
BgNVHREEEzARgg9tYWlsLmdvb2dsZS5jb20wCwYDVR0PBAQDAgeAMGgGCCsGAQUF
BwEBBFwwWjArBggrBgEFBQcwAoYfaHR0cDovL3BraS5nb29nbGUuY29tL0dJQUcy
LmNydDArBggrBgEFBQcwAYYfaHR0cDovL2NsaWVudHMxLmdvb2dsZS5jb20vb2Nz
cDAdBgNVHQ4EFgQUiJxtimAuTfwb+aUtBn5UYKreKvMwDAYDVR0TAQH/BAIwADAf
BgNVHSMEGDAWgBRK3QYWG7z2aLV29YG2u2IaulqBLzAXBgNVHSAEEDAOMAwGCisG
AQQB1nkCBQEwMAYDVR0fBCkwJzAloCOgIYYfaHR0cDovL3BraS5nb29nbGUuY29t
L0dJQUcyLmNybDANBgkqhkiG9w0BAQUFAAOCAQEAH6RYHxHdcGpMpFE3oxDoFnP+
gtuBCHan2yE2GRbJ2Cw8Lw0MmuKqHlf9RSeYfd3BXeKkj1qO6TVKwCh+0HdZk283
TZZyzmEOyclm3UGFYe82P/iDFt+CeQ3NpmBg+GoaVCuWAARJN/KfglbLyyYygcQq
0SgeDh8dRKUiaW3HQSoYvTvdTuqzwK4CXsr3b5/dAOY8uMuG/IAR3FgwTbZ1dtoW
RvOTa8hYiU6A475WuZKyEHcwnGYe57u2I2KbMgcKjPniocj4QzgYsVAVKW3IwaOh
yE+vPxsiUkvQHdO2fojCkY8jg70jxM+gu59tPDNbw3Uh/2Ij310FgTHsnGQMyA==
-----END CERTIFICATE-----`
