// +build e2e

/*
Copyright 2021 The Knative Authors

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

package autotls

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log"
	"math/big"
	"net/url"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/kelseyhightower/envconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/reconciler"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/test"
	"knative.dev/serving/test/conformance/api/shared"
	"knative.dev/serving/test/e2e"
	v1test "knative.dev/serving/test/v1"
)

type dmConfig struct {
	// TLSServiceName is the name of testing Knative Service.
	// It is not required for self-signed CA or for the HTTP01 challenge when wildcard domain
	// is mapped to the Ingress IP.
	TLSServiceName string `envconfig:"tls_service_name" required:"false"`
	// TLSTestNamespace is the namespace of where the tls tests run.
	TLSTestNamespace string `envconfig:"tls_test_namespace" required:"false"`
	// CustomDomainSuffix is the custom domain used for the domainMapping.
	CustomDomainSuffix string `envconfig:"custom_domain_suffix" required:"false"`
}

func TestDomainMappingAutoTLS(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	t.Parallel()

	var env dmConfig
	if err := envconfig.Process("", &env); err != nil {
		t.Fatalf("Failed to process environment variable: %v", err)
	}

	ctx := context.Background()

	clients := e2e.SetupWithNamespace(t, test.TLSNamespace)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "runtime",
	}

	if len(env.TLSServiceName) != 0 {
		names.Service = env.TLSServiceName + "-dmtls"
	}

	// Clean up on test failure or interrupt.
	test.EnsureTearDown(t, clients, &names)

	// Set up initial Service.
	svc, err := v1test.CreateServiceReady(t, clients, &names,
		func(service *v1.Service) {
			service.Annotations = map[string]string{"networking.knative.dev/disableAutoTLS": "True"}
		})
	if err != nil {
		t.Fatalf("Failed to create initial Service %q: %v", names.Service, err)
	}

	// Using fixed hostnames can lead to conflicts when multiple tests run at
	// once, so include the svc name to avoid collisions.
	suffix := "example.com"

	if env.CustomDomainSuffix != "" {
		suffix = env.CustomDomainSuffix
	}

	if env.TLSTestNamespace != "" {
		suffix = env.TLSTestNamespace + "." + suffix
	}

	host := "dm." + suffix

	// Point DomainMapping at our service.
	var dm *v1alpha1.DomainMapping
	if err := reconciler.RetryTestErrors(func(int) error {
		dm, err = clients.ServingAlphaClient.DomainMappings.Create(ctx, &v1alpha1.DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      host,
				Namespace: svc.Service.Namespace,
			},
			Spec: v1alpha1.DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace:  svc.Service.Namespace,
					Name:       svc.Service.Name,
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		}, metav1.CreateOptions{})
		return err
	}); err != nil {
		t.Fatalf("Create(DomainMapping) = %v, expected no error", err)
	}

	t.Cleanup(func() {
		clients.ServingAlphaClient.DomainMappings.Delete(ctx, dm.Name, metav1.DeleteOptions{})
	})

	// Wait for DomainMapping to go Ready.
	if waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		state, err := clients.ServingAlphaClient.DomainMappings.Get(ctx, dm.Name, metav1.GetOptions{})

		// DomainMapping can go Ready if only http is available.
		// Hence the checking for the URL scheme to make sure it is ready for https
		dmTLSReady := state.IsReady() && state.Status.URL != nil && state.Status.URL.Scheme == "https"

		return dmTLSReady, err
	}); waitErr != nil {
		t.Fatalf("The DomainMapping %q was not marked as Ready: %v", dm.Name, waitErr)
	}

	certName := dm.Name
	rootCAs := createRootCAs(t, clients, svc.Route.Namespace, certName)
	httpsClient := createHTTPSClient(t, clients, svc, rootCAs)
	RuntimeRequest(ctx, t, httpsClient, "https://"+host)
}

func TestBYOCertificate(t *testing.T) {
	if !test.ServingFlags.EnableAlphaFeatures {
		t.Skip("Alpha features not enabled")
	}
	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   test.PizzaPlanet1,
	}

	test.EnsureTearDown(t, clients, &names)

	// service to be mapped
	ksvc, err := v1test.CreateServiceReady(t, clients, &names)
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	host := ksvc.Service.Name + ".example.org"
	// set resolvabledomain for custom domain to false by default.
	resolvableCustomDomain := false

	if test.ServingFlags.CustomDomain != "" {
		host = ksvc.Service.Name + "." + test.ServingFlags.CustomDomain
		resolvableCustomDomain = true
	}

	cert, key := makeCertificateForDomain(t, host)
	secret, err := clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: test.AppendRandomString("imasecret"),
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert,
			corev1.TLSPrivateKeyKey: key,
		},
	}, metav1.CreateOptions{})

	if err != nil {
		t.Fatalf("Domainmapping creation could not be completed: %v", err)
	}

	t.Cleanup(func() {
		err = clients.KubeClient.CoreV1().Secrets(secret.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
		if err != nil {
			t.Fatalf("Secret %s/%s could not be deleted: %v", secret.Namespace, secret.Name, err)
		}
	})

	dm := v1alpha1.DomainMapping{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      host,
			Namespace: ksvc.Service.Namespace,
		},
		Spec: v1alpha1.DomainMappingSpec{
			Ref: duckv1.KReference{
				APIVersion: "serving.knative.dev/v1",
				Name:       ksvc.Service.Name,
				Namespace:  ksvc.Service.Namespace,
				Kind:       "Service",
			}, TLS: &v1alpha1.SecretTLS{
				SecretName:      secret.Name,
				SecretNamespace: secret.Namespace,
			}},
		Status: v1alpha1.DomainMappingStatus{},
	}

	t.Log("HOST===>", host)
	_, err = clients.ServingAlphaClient.DomainMappings.Create(context.Background(), &dm, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Problem creating domainmapping : %v", err)
	}

	if waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		ingresses, err := clients.NetworkingClient.Ingresses.Get(context.Background(), ksvc.Service.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range ingresses.Status.Conditions {
			if cond.Type == apis.ConditionReady {
				return cond.IsTrue(), nil
			}
		}
		return false, nil
	}); waitErr != nil {
		t.Fatalf("Ingresses %s never became ready: %v", ksvc.Service.Name, waitErr)
	}

	err = shared.CheckDistribution(context.Background(), t, clients,
		&url.URL{
			Host:   host,
			Scheme: "HTTPS",
		}, 1, 1, []string{test.PizzaPlanetText1}, resolvableCustomDomain)
	if err != nil {
		t.Fatalf("Hitting %v didn't return the expected response: %v", dm.Name, err)
	}
}

func makeCertificateForDomain(t *testing.T, domainName string) (cert []byte, key []byte) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		t.Fatalf("Failed to generate serial number: %v", err)
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	public := &priv.PublicKey
	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore: now,
		NotAfter:  now.Add(time.Hour * 12),

		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domainName},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, public, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}
	var certOut bytes.Buffer
	pem.Encode(&certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatalf("Failed to marshal private key: %v", err)
	}
	var keyOut bytes.Buffer
	pem.Encode(&keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	return certOut.Bytes(), keyOut.Bytes()
}
