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

package resources

import (
	"context"
	"fmt"

	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// MakeDesiredSecrets makes copies of the Secrets referenced by the given ClusterIngress.
func MakeDesiredSecrets(ctx context.Context, ci *v1alpha1.ClusterIngress, secretLister corev1listers.SecretLister) ([]*corev1.Secret, error) {
	gatewaySvcNamespaces := getAllGatewaySvcNamespaces(ctx)
	secrets := []*corev1.Secret{}
	for _, tls := range ci.Spec.TLS {
		originSecret, err := secretLister.Secrets(tls.SecretNamespace).Get(tls.SecretName)
		if err != nil {
			return nil, err
		}
		for _, ns := range gatewaySvcNamespaces {
			if ns == tls.SecretNamespace {
				// no need to copy secret when the target namespace is the same
				// as the origin namespace
				continue
			}
			secrets = append(secrets, makeDesiredSecret(originSecret, ns))
		}
	}
	return secrets, nil
}

func makeDesiredSecret(originSecret *corev1.Secret, targetNamespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetSecret(originSecret.Namespace, originSecret.Name),
			Namespace: targetNamespace,
			Labels: map[string]string{
				networking.OriginSecretNameLabelKey:      originSecret.Name,
				networking.OriginSecretNamespaceLabelKey: originSecret.Namespace,
			},
		},
		Data: originSecret.Data,
		Type: originSecret.Type,
	}
}

// targetSecret returns the name of the Secret that is copied from the origin Secret
// with the given `namespace` and `name`.
func targetSecret(namespace, name string) string {
	return fmt.Sprintf("%s--%s", namespace, name)
}
