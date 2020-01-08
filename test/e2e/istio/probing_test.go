// +build e2e istio

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
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/watch"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logstream"
	"knative.dev/pkg/test/spoof"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/test"
	"knative.dev/serving/test/e2e"
	v1a1test "knative.dev/serving/test/v1alpha1"
)

var (
	namespace = "knative-serving"
)

func TestIstioProbing(t *testing.T) {
	cancel := logstream.Start(t)
	defer cancel()

	clients := e2e.Setup(t)

	// Save the current Gateway to restore it after the test
	oldGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(networking.KnativeIngressGateway, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Gateway %s/%s", namespace, networking.KnativeIngressGateway)
	}
	restore := func() {
		curGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(networking.KnativeIngressGateway, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get Gateway %s/%s", namespace, networking.KnativeIngressGateway)
		}
		curGateway.Spec.Servers = oldGateway.Spec.Servers
		if _, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Update(curGateway); err != nil {
			t.Fatalf("Failed to restore Gateway %s/%s: %v", namespace, networking.KnativeIngressGateway, err)
		}
	}
	test.CleanupOnInterrupt(restore)
	defer restore()

	// Create a dummy service to get the domain name
	var domain string
	func() {
		names := test.ResourceNames{
			Service: test.ObjectNameForTest(t),
			Image:   "helloworld",
		}
		test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
		defer test.TearDown(clients, names)
		objects, _, err := v1a1test.CreateRunLatestServiceReady(t, clients, &names,
			false /* https TODO(taragu) turn this on after helloworld test running with https */)
		if err != nil {
			t.Fatalf("Failed to create Service %s: %v", names.Service, err)
		}
		domain = strings.SplitN(objects.Route.Status.URL.Host, ".", 2)[1]
	}()

	tlsOptions := &istiov1alpha3.Server_TLSOptions{
		Mode:              istiov1alpha3.Server_TLSOptions_SIMPLE,
		PrivateKey:        "/etc/istio/ingressgateway-certs/tls.key",
		ServerCertificate: "/etc/istio/ingressgateway-certs/tls.crt",
	}

	cases := []struct {
		name    string
		servers []*istiov1alpha3.Server
		urls    []string
	}{{
		name: "HTTP",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP2",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http2",
				Number:   80,
				Protocol: "HTTP2",
			},
		}},
		urls: []string{"http://%s/"},
	}, {
		name: "HTTP custom port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   443,
				Protocol: "HTTP",
			},
		}},
		urls: []string{"http://%s:443/"},
	}, {
		name: "HTTP & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTP redirect & HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-http",
				Number:   80,
				Protocol: "HTTP",
			},
		}, {
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"http://%s/", "https://%s/"},
	}, {
		name: "HTTPS",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "standard-https",
				Number:   443,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s/"},
	}, {
		name: "HTTPS non standard port",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-https",
				Number:   80,
				Protocol: "HTTPS",
			},
			Tls: tlsOptions,
		}},
		urls: []string{"https://%s:80/"},
	}, {
		name: "unsupported protocol (GRPC)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-grpc",
				Number:   80,
				Protocol: "GRPC",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (TCP)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-tcp",
				Number:   80,
				Protocol: "TCP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "unsupported protocol (Mongo)",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-mongo",
				Number:   80,
				Protocol: "Mongo",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}, {
		name: "port not present in service",
		servers: []*istiov1alpha3.Server{{
			Hosts: []string{"*"},
			Port: &istiov1alpha3.Port{
				Name:     "custom-http",
				Number:   8090,
				Protocol: "HTTP",
			},
		}},
		// No URLs to probe, just validates the Knative Service is Ready instead of stuck in NotReady
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   "helloworld",
			}

			var transportOptions []interface{}
			if hasHTTPS(c.servers) {
				transportOptions = append(transportOptions, setupHTTPS(t, clients.KubeClient, []string{names.Service + "." + domain}))
			}

			setupGateway(t, clients, names, domain, c.servers)

			// Create the service and wait for it to be ready
			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)
			_, _, err = v1a1test.CreateRunLatestServiceReady(t, clients, &names, false /* https */)
			if err != nil {
				t.Fatalf("Failed to create Service %s: %v", names.Service, err)
			}

			// Probe the Service on all endpoints
			var g errgroup.Group
			for _, tmpl := range c.urls {
				tmpl := tmpl
				g.Go(func() error {
					u, err := url.Parse(fmt.Sprintf(tmpl, names.Service+"."+domain))
					if err != nil {
						return fmt.Errorf("failed to parse URL: %w", err)
					}
					if _, err := pkgTest.WaitForEndpointStateWithTimeout(
						clients.KubeClient,
						t.Logf,
						u,
						v1a1test.RetryingRouteInconsistency(pkgTest.MatchesAllOf(pkgTest.IsStatusOK, pkgTest.MatchesBody(test.HelloWorldText))),
						"HelloWorldServesText",
						test.ServingFlags.ResolvableDomain,
						1*time.Minute,
						transportOptions...); err != nil {
						return fmt.Errorf("failed to probe %s: %w", u, err)
					}
					return nil
				})
			}
			err = g.Wait()
			if err != nil {
				t.Fatalf("Failed to probe the Service: %v", err)
			}
		})
	}
}

func hasHTTPS(servers []*istiov1alpha3.Server) bool {
	for _, server := range servers {
		if server.Port.Protocol == "HTTPS" {
			return true
		}
	}
	return false
}

// setupGateway updates the ingress Gateway to the provided Servers and waits until all Envoy pods have been updated.
func setupGateway(t *testing.T, clients *test.Clients, names test.ResourceNames, domain string, servers []*istiov1alpha3.Server) {
	// Get the current Gateway
	curGateway, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Get(networking.KnativeIngressGateway, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Gateway %s/%s: %v", namespace, networking.KnativeIngressGateway, err)
	}

	// Update its Spec
	newGateway := curGateway.DeepCopy()
	newGateway.Spec.Servers = servers

	// Update the Gateway
	gw, err := clients.IstioClient.NetworkingV1alpha3().Gateways(namespace).Update(newGateway)
	if err != nil {
		t.Fatalf("Failed to update Gateway %s/%s: %v", namespace, networking.KnativeIngressGateway, err)
	}

	var selectors []string
	for k, v := range gw.Spec.Selector {
		selectors = append(selectors, k+"="+v)
	}
	selector := strings.Join(selectors, ",")

	// Restart the Gateway pods: this is needed because Istio without SDS won't refresh the cert when the secret is updated
	pods, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").List(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatalf("Failed to list Gateway pods: %v", err)
	}

	// TODO(bancel): there is a race condition here if a pod listed in the call above is deleted before calling watch below

	var wg sync.WaitGroup
	wg.Add(len(pods.Items))
	wtch, err := clients.KubeClient.Kube.CoreV1().Pods("istio-system").Watch(metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatalf("Failed to watch Gateway pods: %v", err)
	}
	defer wtch.Stop()

	done := make(chan struct{})
	go func() {
		for {
			select {
			case event := <-wtch.ResultChan():
				if event.Type == watch.Deleted {
					wg.Done()
				}
			case <-done:
				return
			}
		}
	}()

	err = clients.KubeClient.Kube.CoreV1().Pods("istio-system").DeleteCollection(&metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		t.Fatalf("Failed to delete Gateway pods: %v", err)
	}

	wg.Wait()
	done <- struct{}{}
}

// setupHTTPS creates a self-signed certificate, installs it as a Secret and returns an *http.Transport
// trusting the certificate as a root CA.
func setupHTTPS(t *testing.T, kubeClient *pkgTest.KubeClient, hosts []string) spoof.TransportOption {
	t.Helper()

	cert, key, err := generateCertificate(hosts)
	if err != nil {
		t.Fatalf("Failed to generate the certificate: %v", err)
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if ok := rootCAs.AppendCertsFromPEM(cert); !ok {
		t.Fatalf("Failed to add the certificate to the root CA")
	}

	kubeClient.Kube.CoreV1().Secrets("istio-system").Delete("istio-ingressgateway-certs", &metav1.DeleteOptions{})
	_, err = kubeClient.Kube.CoreV1().Secrets("istio-system").Create(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "istio-system",
			Name:      "istio-ingressgateway-certs",
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.key": key,
			"tls.crt": cert,
		},
	})
	if err != nil {
		t.Fatalf("Failed to set Secret %s/%s: %v", "istio-system", "istio-ingressgateway-certs", err)
	}

	return func(transport *http.Transport) *http.Transport {
		transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
		return transport
	}
}

// generateCertificate generates a self-signed certificate for the provided hosts and returns
// the PEM encoded certificate and private key.
func generateCertificate(hosts []string) ([]byte, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	notBefore := time.Now().Add(-5 * time.Minute)
	notAfter := notBefore.Add(2 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Knative Serving"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create the certificate: %w", err)
	}

	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the certificate: %w", err)
	}

	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return nil, nil, fmt.Errorf("failed to encode the private key: %w", err)
	}

	return certBuf.Bytes(), keyBuf.Bytes(), nil
}
