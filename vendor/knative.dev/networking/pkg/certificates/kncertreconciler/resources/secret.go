package resources

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/networking/pkg/certificates"
	"knative.dev/pkg/kmeta"
)

func MakeSecret(knCert *v1alpha1.Certificate, cert *certificates.KeyPair, caCert []byte, labelName string) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{
			Name:            knCert.Spec.SecretName,
			Namespace:       knCert.Namespace,
			OwnerReferences: []v1.OwnerReference{*kmeta.NewControllerRef(knCert)},
			Labels:          knCert.Labels,
			Annotations:     knCert.Annotations,
		},
	}

	if s.Labels == nil {
		s.Labels = make(map[string]string, 2)
	}
	s.Labels[networking.CertificateUIDLabelKey] = string(knCert.GetUID())
	s.Labels[labelName] = ""

	s.Data = make(map[string][]byte, 3)
	s.Data[certificates.CertName] = cert.CertBytes()
	s.Data[certificates.PrivateKeyName] = cert.PrivateKeyBytes()
	s.Data[certificates.CaCertName] = caCert

	return s
}
