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

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// GetSecrets gets the all of the secrets referenced by the given ClusterIngress, and
// returns a map whose key is the a secret namespace/name key and value is pointer of the secret.
func GetSecrets(ci *v1alpha1.ClusterIngress, secretLister corev1listers.SecretLister) (map[string]*corev1.Secret, error) {
	secrets := map[string]*corev1.Secret{}
	for _, tls := range ci.Spec.TLS {
		ref := secretKey(tls)
		if _, ok := secrets[ref]; ok {
			continue
		}
		secret, err := secretLister.Secrets(tls.SecretNamespace).Get(tls.SecretName)
		if err != nil {
			return nil, err
		}
		secrets[ref] = secret
	}
	return secrets, nil
}

// MakeSecrets makes copies of the origin Secrets under the namespace of Istio gateway service.
func MakeSecrets(ctx context.Context, originSecrets map[string]*corev1.Secret, ci *v1alpha1.ClusterIngress) []*corev1.Secret {
	gatewaySvcNamespaces := getAllGatewaySvcNamespaces(ctx)
	secrets := []*corev1.Secret{}
	for _, originSecret := range originSecrets {
		for _, ns := range gatewaySvcNamespaces {
			if ns == originSecret.Namespace {
				// no need to copy secret when the target namespace is the same
				// as the origin namespace
				continue
			}
			secrets = append(secrets, makeSecret(originSecret, ns, ci))
		}
	}
	return secrets
}

func makeSecret(originSecret *corev1.Secret, targetNamespace string, ci *v1alpha1.ClusterIngress) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetSecret(originSecret, ci),
			Namespace: targetNamespace,
			Labels: map[string]string{
				networking.OriginSecretNameLabelKey:      originSecret.Name,
				networking.OriginSecretNamespaceLabelKey: originSecret.Namespace,
			},
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ci)},
		},
		Data: originSecret.Data,
		Type: originSecret.Type,
	}
}

// targetSecret returns the name of the Secret that is copied from the origin Secret.
func targetSecret(originSecret *corev1.Secret, ci *v1alpha1.ClusterIngress) string {
	return fmt.Sprintf("%s-%s", ci.Name, originSecret.UID)
}

// SecretRef returns the ObjectReference of a secret given the namespace and name of the secret.
func SecretRef(namespace, name string) corev1.ObjectReference {
	gvk := corev1.SchemeGroupVersion.WithKind("Secret")
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}

// Generates the k8s secret key with the given TLS.
func secretKey(tls v1alpha1.ClusterIngressTLS) string {
	return fmt.Sprintf("%s/%s", tls.SecretNamespace, tls.SecretName)
}
