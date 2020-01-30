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
	"knative.dev/pkg/system"
	"knative.dev/pkg/test/logging"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/test"
)

// CreateCertificate creates a Certificate with the given DNS names
func CreateCertificate(t *testing.T, clients *test.Clients, dnsNames []string) (*v1alpha1.Certificate, context.CancelFunc) {
	t.Helper()

	name := test.ObjectNameForTest(t)
	certClass, err := getCertClass(clients)
	if err != nil {
		t.Fatalf("failed to get config-network ConfigMap: %v", err)
	}

	cert := &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: certClass,
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: name,
		},
	}

	cleanup := func() {
		clients.NetworkingClient.Certificates.Delete(cert.Name, &metav1.DeleteOptions{})
		clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(cert.Spec.SecretName, &metav1.DeleteOptions{})
	}

	test.CleanupOnInterrupt(cleanup)

	cert, err = clients.NetworkingClient.Certificates.Create(cert)
	if err != nil {
		t.Fatalf("error creating Certificate: %v", err)
	}

	return cert, cleanup
}

// WaitForCertificateSecret polls the status of the Secret for the provided Certificate
// until it exists or the timeout is exceeded. It then validates its contents
func WaitForCertificateSecret(client *test.Clients, cert *v1alpha1.Certificate, desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForCertificateSecret/%s/%s", cert.Spec.SecretName, desc))
	defer span.End()

	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		secret, err := client.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Get(cert.Spec.SecretName, metav1.GetOptions{})

		if apierrs.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return true, fmt.Errorf("failed to get secret: %w", err)
		}

		block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
		if block == nil {
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

	if waitErr != nil {
		return fmt.Errorf("secret %q not found: %w", cert.Spec.SecretName, waitErr)
	}
	return nil
}

// WaitForCertificateState polls the status of the Certificate called name from client
// every PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForCertificateState(client *test.NetworkingClients, name string, inState func(r *v1alpha1.Certificate) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForCertificateState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Certificate
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Certificates.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("certificate %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

func getCertClass(client *test.Clients) (string, error) {
	configNetworkCM, err := client.KubeClient.Kube.CoreV1().ConfigMaps(system.Namespace()).Get("config-network", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return configNetworkCM.Data["certificate.class"], nil
}

// VerifyChallenges verifies that the given certificate has the correct number
// of HTTP01challenges and they contain valid data.
func VerifyChallenges(t *testing.T, client *test.Clients, cert *v1alpha1.Certificate) {
	t.Helper()

	certDomains := sets.NewString(cert.Spec.DNSNames...)

	for _, challenge := range cert.Status.HTTP01Challenges {
		if challenge.ServiceName == "" {
			t.Error("HTTP01 Challenge missing solver service name")
		}

		if !certDomains.Has(challenge.URL.Host) {
			t.Errorf("HTTP01 Challenge host %s is not one of: %v", challenge.URL.Host, cert.Spec.DNSNames)
		}
		_, err := client.KubeClient.Kube.CoreV1().Services(challenge.ServiceNamespace).Get(challenge.ServiceName, metav1.GetOptions{})
		if apierrs.IsNotFound(err) {
			t.Errorf("failed to find solver service for challenge: %v", err)
		}
	}

}
