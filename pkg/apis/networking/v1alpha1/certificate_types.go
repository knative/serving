/*
Copyright 2018 The Knative Authors.

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

package v1alpha1

import (
	"github.com/knative/pkg/apis"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Certificate is responsible for provisioning a SSL certificate for the
// given hosts. It is a Knative abstraction for various SSL certificate
// provisioning solutions (such as cert-manager or self-signed SSL certificate).
type Certificate struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the Certificate.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec CertificateSpec `json:"spec,omitempty"`

	// Status is the current state of the Certificate.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Status CertificateStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CertificateList is a collection of Certificate.
type CertificateList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of Certificate.
	Items []Certificate `json:"items"`
}

// CertificateSpec defines the desired state of Certificate.
type CertificateSpec struct {
	// TODO: Generation does not work correctly with CRD. They are scrubbed
	// by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
	// So, we add Generation here. Once that gets fixed, remove this and use
	// ObjectMeta.Generation instead.
	// +optional
	Generation int64 `json:"generation,omitempty"`

	// DNSNames is a list of DNS names the Certificate could support.
	DNSNames []string `json:"dnsNames"`

	// SecretName is the name of the secret resource to store the SSL certificate in.
	SecretName string `json:"secretName"`
}

// CertificateStatus defines the observed state of Certificate.
type CertificateStatus struct {

	// The detailed information related to the provisioned certificate.
	// +optional
	CertificateInfo CertificateInfo `json:"certificateInfo,omitempty"`

	// +optional
	Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`
}

// InitializeConditions initializes the certificate conditions.
func (cs *CertificateStatus) InitializeConditions() {
	certificateCondSet.Manage(cs).InitializeConditions()
}

// MarkReady marks the certificate as ready to use.
func (cs *CertificateStatus) MarkReady() {
	certificateCondSet.Manage(cs).MarkTrue(CertificateCondidtionReady)
}

// GetConditions returns the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cs *CertificateStatus) GetConditions() duckv1alpha1.Conditions {
	return cs.Conditions
}

// SetConditions sets the Conditions array. This enables generic handling of
// conditions by implementing the duckv1alpha1.Conditions interface.
func (cs *CertificateStatus) SetConditions(conditions duckv1alpha1.Conditions) {
	cs.Conditions = conditions
}

// GetCondition gets a speicifc condition of the Certificate status.
func (cs *CertificateStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
	return certificateCondSet.Manage(cs).GetCondition(t)
}

// ConditionType represents a Certificate condition value
const (
	// CertificateConditionReady is set when the requested certificate
	// is provioned and valid.
	CertificateCondidtionReady = duckv1alpha1.ConditionReady
)

// CertificateInfo defines the detailed information related to the provisioned
// certificate.
type CertificateInfo struct {

	// The DNS Names that the certificate actually supported.
	// SupportedDNSNames could be the super set of the DNSNames in
	// the CertificateSpec.
	// +optional
	SupportedDNSNames []string `json:"supportedDNSNames,omitempty"`

	// The expiration time of the certificate stored in the secret named
	// by this resource in spec.secretName.
	// +optional
	NotAfter *metav1.Time `json:"notAfter,omitempty"`
}

var certificateCondSet = duckv1alpha1.NewLivingConditionSet(CertificateCondidtionReady)

var _ apis.Validatable = (*Certificate)(nil)

func (c *Certificate) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Certificate")
}
