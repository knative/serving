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

package test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/signals"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
)

const (
	// PollInterval is how frequently e2e tests will poll for updates.
	PollInterval = 1 * time.Second
	// PollTimeout is how long e2e tests will wait for resource updates when polling.
	PollTimeout = 10 * time.Minute

	// HelloVolumePath is the path to the test volume.
	HelloVolumePath = "/hello/world"

	caSecretNamespace = "cert-manager"
	caSecretName      = "ca-key-pair"
)

// util.go provides shared utilities methods across knative serving test

// ListenAndServeGracefully calls into ListenAndServeGracefullyWithPattern
// by passing handler to handle requests for "/"
func ListenAndServeGracefully(addr string, handler func(w http.ResponseWriter, r *http.Request)) {
	ListenAndServeGracefullyWithHandler(addr, http.HandlerFunc(handler))
}

// ListenAndServeGracefullyWithHandler creates an HTTP server, listens on the defined address
// and handles incoming requests with the given handler.
// It blocks until SIGTERM is received and the underlying server has shutdown gracefully.
func ListenAndServeGracefullyWithHandler(addr string, handler http.Handler) {
	server := http.Server{Addr: addr, Handler: h2c.NewHandler(handler, &http2.Server{})}
	go server.ListenAndServe()

	<-signals.SetupSignalHandler()
	server.Shutdown(context.Background())
}

// AddRootCAtoTransport returns TransportOption when HTTPS option is true. Otherwise it returns plain spoof.TransportOption.
func AddRootCAtoTransport(ctx context.Context, logf logging.FormatLogger, clients *Clients, https bool) spoof.TransportOption {
	if !https {
		return func(transport *http.Transport) *http.Transport {
			return transport
		}
	}
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}
	if !rootCAs.AppendCertsFromPEM(PemDataFromSecret(ctx, logf, clients, caSecretNamespace, caSecretName)) {
		logf("Failed to add the certificate to the root CA")
	}
	return func(transport *http.Transport) *http.Transport {
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
		return transport
	}
}

// PemDataFromSecret gets pem data from secret.
func PemDataFromSecret(ctx context.Context, logf logging.FormatLogger, clients *Clients, ns, secretName string) []byte {
	secret, err := clients.KubeClient.CoreV1().Secrets(ns).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		logf("Failed to get Secret %s: %v", secretName, err)
		return []byte{}
	}
	return secret.Data[corev1.TLSCertKey]
}

// AddTestAnnotation adds the knative-e2e-test label to the resource.
func AddTestAnnotation(t pkgTest.T, m metav1.ObjectMeta) {
	kmeta.UnionMaps(m.Annotations, map[string]string{
		testAnnotation: t.Name(),
	})
}
