/*
Copyright 2019 The Knative Authors.

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
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the Certificate.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Spec CertificateSpec `json:"spec,omitempty"`

	// Status is the current state of the Certificate.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status CertificateStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CertificateList is a collection of `Certificate`.
type CertificateList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of `Certificate`.
	Items []Certificate `json:"items"`
}

// CertificateSpec defines the desired state of Certificate.
type CertificateSpec struct {
	// DNSNames is a list of DNS names the Certificate could support.
	// The wildcard format of DNSNames (e.g. *.default.example.com) is supported.
	DNSNames []string `json:"dnsNames"`

	// SecretName is the name of the secret resource to store the SSL certificate in.
	SecretName string `json:"secretName"`
}

// CertificateStatus defines the observed state of Certificate.
type CertificateStatus struct {
	// The expiration time of the TLS certificate stored in the secret named
	// by this resource in spec.secretName.
	// +optional
	NotAfter *metav1.Time `json:"notAfter,omitempty"`

	// +optional
	Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`

	// ObservedGeneration is the 'Generation' of the Certificate that
	// was last processed by the controller. The observed generation is updated
	// even if the controller failed to process the spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// InitializeConditions initializes the certificate conditions.
func (cs *CertificateStatus) InitializeConditions() {
	certificateCondSet.Manage(cs).InitializeConditions()
}

// MarkReady marks the certificate as ready to use.
func (cs *CertificateStatus) MarkReady() {
	certificateCondSet.Manage(cs).MarkTrue(CertificateCondidtionReady)
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

var certificateCondSet = duckv1alpha1.NewLivingConditionSet(CertificateCondidtionReady)

var _ apis.Validatable = (*Certificate)(nil)

// GetGroupVersionKind returns the GroupVersionKind of Certificate.
func (c *Certificate) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Certificate")
}
