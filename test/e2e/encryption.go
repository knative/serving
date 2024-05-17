//go:build e2e
// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/system"
	"knative.dev/serving/test"
)

// this needs to match the ClusterIssuer in third_parth/cert-manager-latest/net-certmanager.yaml
const (
	certManagerCASecret  = "knative-selfsigned-ca"
	certManagerNamespace = "cert-manager"
)

// GetCASecret returns the Secret that is used by the CA to issue KnativeCertificates.
func GetCASecret(clients *test.Clients) (*corev1.Secret, error) {
	cm, err := clients.KubeClient.CoreV1().ConfigMaps(system.Namespace()).
		Get(context.Background(), netcfg.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap config-network: %w", err)
	}

	// CA is only needed when encryption on the cluster is enabled
	if !strings.EqualFold(cm.Data[netcfg.ClusterLocalDomainTLSKey], string(netcfg.EncryptionEnabled)) &&
		!strings.EqualFold(cm.Data[netcfg.SystemInternalTLSKey], string(netcfg.EncryptionEnabled)) {
		return nil, nil
	}

	class := getCertificateClass(cm)
	switch class {
	case netcfg.CertManagerCertificateClassName:
		return getCertManagerCA(clients)
	default:
		return nil, fmt.Errorf("invalid %s: %s", netcfg.DefaultCertificateClassKey, class)
	}
}

// getCertificateClass returns the currently configured certificate-class.
func getCertificateClass(cm *corev1.ConfigMap) string {
	// if not specified, we fall back to our default, which is cert-manager
	if class, ok := cm.Data[netcfg.DefaultCertificateClassKey]; ok {
		return class
	}

	return netcfg.CertManagerCertificateClassName
}

func getCertManagerCA(clients *test.Clients) (*corev1.Secret, error) {
	var secret *corev1.Secret
	err := wait.PollUntilContextTimeout(context.Background(), test.PollInterval, test.PollTimeout, true, func(context.Context) (bool, error) {
		caSecret, err := clients.KubeClient.CoreV1().Secrets(certManagerNamespace).Get(context.Background(), certManagerCASecret, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		// CA not yet populated
		if len(caSecret.Data[certificates.CertName]) == 0 {
			return false, nil
		}

		secret = caSecret
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("error while waiting for cert-manager self-signed CA to be populated: %w", err)
	}

	return secret, nil
}
