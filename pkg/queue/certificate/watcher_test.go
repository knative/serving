/*
Copyright 2023 The Knative Authors

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
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/certificates"
	ktesting "knative.dev/pkg/logging/testing"
)

const (
	initialSAN = "initial.knative"
	updatedSAN = "updated.knative"
)

func TestCertificateRotation(t *testing.T) {
	// Create initial certificate and key on disk
	dir := t.TempDir()

	err := createAndSaveCertificate(initialSAN, dir)
	if err != nil {
		t.Fatal("failed to create and save initial certificate", err)
	}

	// Watch the certificate files
	cw, err := NewCertWatcher(dir+"/"+certificates.CertName, dir+"/"+certificates.PrivateKeyName, 1*time.Second, ktesting.TestLogger(t))
	if err != nil {
		t.Fatal("failed to create CertWatcher", err)
	}
	defer cw.Stop()

	// CertWatcher should return the expected certificate
	c, err := cw.GetCertificate(nil)
	if err != nil {
		t.Fatal("failed to call GetCertificate on CertWatcher", err)
	}
	san, err := getSAN(c)
	if err != nil {
		t.Fatal("failed to parse SAN of certificate", err)
	}
	if san != initialSAN {
		t.Errorf("CertWatcher did not return the expected certificate. want: %s, got: %s", initialSAN, san)
	}

	// Update the certificate and key on disk
	err = createAndSaveCertificate(updatedSAN, dir)
	if err != nil {
		t.Fatal("failed to update and save initial certificate", err)
	}

	// CertWatcher should return the new certificate
	// Give CertWatcher some time to update the certificate
	if err := wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
		c, err = cw.GetCertificate(nil)
		if err != nil {
			return false, err
		}

		san, err = getSAN(c)
		if err != nil {
			return false, err
		}

		if san != updatedSAN {
			t.Logf("CertWatcher did not return the expected certificate. want: %s, got: %s", updatedSAN, san)
			return false, nil
		}

		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func createAndSaveCertificate(san, dir string) error {
	ca, err := certificates.CreateCACerts(1 * time.Hour)
	if err != nil {
		return err
	}

	caCert, caKey, err := ca.Parse()
	if err != nil {
		return err
	}

	cert, err := certificates.CreateCert(caKey, caCert, 1*time.Hour, san)
	if err != nil {
		return err
	}

	if err := os.WriteFile(dir+"/"+certificates.CertName, cert.CertBytes(), 0644); err != nil {
		return err
	}

	return os.WriteFile(dir+"/"+certificates.PrivateKeyName, cert.PrivateKeyBytes(), 0644)
}

func getSAN(c *tls.Certificate) (string, error) {
	parsed, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return "", err
	}
	return parsed.DNSNames[0], nil
}
