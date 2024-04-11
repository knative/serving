/*
Copyright 2020 The Knative Authors

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

package certificate

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/test"
	"knative.dev/pkg/test/logging"
)

// CreateCertificate creates a Certificate with the given DNS names. The
// certificate is automatically cleaned up when the test ends or is
// interrupted.
func CreateCertificate(ctx context.Context, t *testing.T, clients *test.Clients, dnsNames []string) *v1alpha1.Certificate {
	t.Helper()

	name := test.ObjectNameForTest(t)
	cert := &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: test.NetworkingFlags.CertificateClass,
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: name,
		},
	}

	test.EnsureCleanup(t, func() {
		clients.NetworkingClient.Certificates.Delete(ctx, cert.Name, metav1.DeleteOptions{})
		clients.KubeClient.CoreV1().Secrets(test.ServingNamespace).Delete(ctx, cert.Spec.SecretName, metav1.DeleteOptions{})
	})

	cert, err := clients.NetworkingClient.Certificates.Create(ctx, cert, metav1.CreateOptions{})
	if err != nil {
		t.Fatal("Error creating Certificate:", err)
	}

	return cert
}

// IsCertificateReady will check the status conditions of the certificate and return true if the certificate is
// ready.
func IsCertificateReady(c *v1alpha1.Certificate) (bool, error) {
	return c.IsReady(), nil
}

// WaitForCertificateSecret polls the status of the Secret for the provided Certificate
// until it exists or the timeout is exceeded. It then validates its contents
func WaitForCertificateSecret(ctx context.Context, t *testing.T, client *test.Clients, cert *v1alpha1.Certificate, desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForCertificateSecret/%s/%s", cert.Spec.SecretName, desc))
	defer span.End()

	return wait.PollUntilContextTimeout(ctx, test.PollInterval, test.PollTimeout, true, func(ctx context.Context) (bool, error) {
		secret, err := client.KubeClient.CoreV1().Secrets(test.ServingNamespace).Get(ctx, cert.Spec.SecretName, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return true, fmt.Errorf("failed to get secret: %w", err)
		}

		block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
		if block == nil {
			// PEM files are text, so just dump it here.
			t.Logf("Bad PEM file:\n%s", secret.Data[corev1.TLSCertKey])
			return true, errors.New("failed to decode PEM data")
		}

		certData, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return true, fmt.Errorf("failed to parse certificate: %w", err)
		}

		if got, want := certData.DNSNames, cert.Spec.DNSNames; !cmp.Equal(got, want) {
			return true, fmt.Errorf("incorrect DNSNames in secret. Got %v, want %v", got, want)
		}

		return true, nil
	})
}

// WaitForCertificateState polls the status of the Certificate called name from client
// every PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForCertificateState(ctx context.Context, client *test.NetworkingClients, name string, inState func(r *v1alpha1.Certificate) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForCertificateState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Certificate
	return wait.PollUntilContextTimeout(ctx, test.PollInterval, test.PollTimeout, true, func(ctx context.Context) (bool, error) {
		var err error
		lastState, err = client.Certificates.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})
}

// VerifyChallenges verifies that the given certificate has the correct number
// of HTTP01challenges and they contain valid data.
func VerifyChallenges(ctx context.Context, t *testing.T, client *test.Clients, cert *v1alpha1.Certificate) {
	t.Helper()

	certDomains := sets.New(cert.Spec.DNSNames...)

	for _, challenge := range cert.Status.HTTP01Challenges {
		if challenge.ServiceName == "" {
			t.Error("HTTP01 Challenge missing solver service name")
		}

		if !certDomains.Has(challenge.URL.Host) {
			t.Errorf("HTTP01 Challenge host %s is not one of: %v", challenge.URL.Host, cert.Spec.DNSNames)
		}
		_, err := client.KubeClient.CoreV1().Services(challenge.ServiceNamespace).Get(ctx, challenge.ServiceName, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			t.Error("failed to find solver service for challenge:", err)
		}
	}
}
