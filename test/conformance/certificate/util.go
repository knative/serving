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
	"fmt"
	"strings"
	"testing"

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

func createCertificate(t *testing.T, clients *test.Clients, dnsNames []string) (*v1alpha1.Certificate, context.CancelFunc) {
	t.Helper()
	name := test.ObjectNameForTest(t)

	cert := &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: test.ServingNamespace,
			Annotations: map[string]string{
				networking.CertificateClassAnnotationKey: getCertClass(t, clients),
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: name,
		},
	}

	test.CleanupOnInterrupt(func() {
		clients.NetworkingClient.Certificates.Delete(cert.Name, &metav1.DeleteOptions{})
		clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(cert.Spec.SecretName, &metav1.DeleteOptions{})
	})
	cert, err := clients.NetworkingClient.Certificates.Create(cert)
	if err != nil {
		t.Fatalf("Error creating Certificate: %v", err)
	}

	return cert, func() {
		err := clients.NetworkingClient.Certificates.Delete(cert.Name, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Certificate %s: %v", cert.Name, err)
		}
		secretName := cert.Spec.SecretName
		err = clients.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Delete(secretName, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Error cleaning up Secret %s for Certificate %s: %v", secretName, cert.Name, err)
		}

	}
}

// WaitForCertificateState polls the status of the Certificate called name from client
// every PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func waitForCertificateState(client *test.NetworkingClients, name string, inState func(r *v1alpha1.Certificate) (bool, error), desc string) error {
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

func waitForSecret(client *test.Clients, name string, desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("waitForSecret/%s/%s", name, desc))
	defer span.End()

	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		_, err := client.KubeClient.Kube.CoreV1().Secrets(test.ServingNamespace).Get(name, metav1.GetOptions{})

		if apierrs.IsNotFound(err) {
			return false, nil
		}

		return true, err
	})

	if waitErr != nil {
		return fmt.Errorf("secret %q not found: %w", name, waitErr)
	}
	return nil
}

func certHasHTTP01Challenges(cert *v1alpha1.Certificate) (bool, error) {
	return len(cert.Status.HTTP01Challenges) == len(cert.Spec.DNSNames), nil
}

func getCertClass(t *testing.T, client *test.Clients) string {
	configNetworkCM, err := client.KubeClient.Kube.CoreV1().ConfigMaps(system.Namespace()).Get("config-network", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get config-network ConfigMap: %v", err)
		return ""
	}
	return configNetworkCM.Data["certificate.class"]
}

func verifyChallenges(t *testing.T, cert *v1alpha1.Certificate) {
	certDomains := sets.NewString(cert.Spec.DNSNames...)

	for _, challenge := range cert.Status.HTTP01Challenges {
		if challenge.ServiceName == "" {
			t.Error("HTTP01 Challenge missing solver service name")
		}

		if !certDomains.Has(challenge.URL.Host) {
			t.Errorf("HTTP01 Challenge host %s is not one of: %s", challenge.URL.Host, strings.Join(cert.Spec.DNSNames, ","))
		}
	}
}
