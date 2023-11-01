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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/networking/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/system"
	"knative.dev/serving/test"
)

// KnativeCASecretName needs to match config/core/300-secret.yaml
const KnativeCASecretName = "serving-certs-cluster-local-domain-ca"

// GetCASecret returns the Secret that is used by the CA to issue
// KnativeCertificates.
// Note: this process can be omitted when https://github.com/knative/serving/issues/14196 is implemented.
// Then httpproxy.go should get the CA to trust from the Addressable.
func GetCASecret(clients *test.Clients) (*corev1.Secret, error) {
	cm, err := clients.KubeClient.CoreV1().ConfigMaps(system.Namespace()).
		Get(context.Background(), netcfg.ConfigMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap config-network: %w", err)
	}

	// CA is only needed when encryption on the cluster is enabled
	if !strings.EqualFold(cm.Data[netcfg.ClusterLocalDomainTLSKey], string(netcfg.EncryptionEnabled)) {
		return nil, nil
	}

	switch cm.Data[netcfg.DefaultCertificateClassKey] {
	case netcfg.KnativeSelfSignedCertificateClassName:
		return getKnativeSelfSignedCA(clients)

		// TODO: add this for net-certmanager as well
	default:
		return nil, fmt.Errorf("invalid %s: %s", netcfg.DefaultCertificateClassKey, cm.Data[netcfg.DefaultIngressClassKey])
	}
}

func getKnativeSelfSignedCA(clients *test.Clients) (*corev1.Secret, error) {
	var secret *corev1.Secret
	err := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		caSecret, err := clients.KubeClient.CoreV1().Secrets(system.Namespace()).Get(context.Background(), KnativeCASecretName, metav1.GetOptions{})
		if err != nil {
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
		return nil, fmt.Errorf("error while waiting for Knative self-signed CA to be popluated: %w", err)
	}

	return secret, nil
}
