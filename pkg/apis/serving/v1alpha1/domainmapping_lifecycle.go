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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
)

var domainMappingCondSet = apis.NewLivingConditionSet(
	DomainMappingConditionDomainClaimed,
	DomainMappingConditionIngressReady,
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*DomainMapping) GetConditionSet() apis.ConditionSet {
	return domainMappingCondSet
}

// GetGroupVersionKind returns the GroupVersionKind.
func (dm *DomainMapping) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("DomainMapping")
}

// IsReady returns true if the DomainMapping is ready.
func (dms *DomainMappingStatus) IsReady() bool {
	return domainMappingCondSet.Manage(dms).IsHappy()
}

// IsReady returns true if the Status condition DomainMappingConditionReady
// is true and the latest spec has been observed.
func (dm *DomainMapping) IsReady() bool {
	dms := dm.Status
	return dms.ObservedGeneration == dm.Generation &&
		dms.GetCondition(DomainMappingConditionReady).IsTrue()
}

// InitializeConditions sets the initial values to the conditions.
func (dms *DomainMappingStatus) InitializeConditions() {
	domainMappingCondSet.Manage(dms).InitializeConditions()
}

// MarkIngressNotConfigured changes the IngressReady condition to be unknown to reflect
// that the Ingress does not yet have a Status.
func (dms *DomainMappingStatus) MarkIngressNotConfigured() {
	domainMappingCondSet.Manage(dms).MarkUnknown(DomainMappingConditionIngressReady,
		"IngressNotConfigured", "Ingress has not yet been reconciled.")
}

// MarkDomainClaimed updates the DomainMappingConditionDomainClaimed condition
// to indicate that the domain was successfully claimed.
func (dms *DomainMappingStatus) MarkDomainClaimed() {
	domainMappingCondSet.Manage(dms).MarkTrue(DomainMappingConditionDomainClaimed)
}

// MarkDomainClaimNotOwned updates the DomainMappingConditionDomainClaimed
// condition to indicate that the domain is already in use by another
// DomainMapping.
func (dms *DomainMappingStatus) MarkDomainClaimNotOwned() {
	domainMappingCondSet.Manage(dms).MarkFalse(DomainMappingConditionDomainClaimed, "DomainAlreadyClaimed",
		"The domain name is already in use by another DomainMapping")
}

// MarkDomainClaimFailed updates the DomainMappingConditionDomainClaimed
// condition to indicate that creating the ClusterDomainClaim failed.
func (dms *DomainMappingStatus) MarkDomainClaimFailed(reason string) {
	domainMappingCondSet.Manage(dms).MarkFalse(DomainMappingConditionDomainClaimed, "DomainClaimFailed", reason)
}

// PropagateIngressStatus updates the DomainMappingConditionIngressReady
// condition according to the underlying Ingress's status.
func (dms *DomainMappingStatus) PropagateIngressStatus(cs netv1alpha1.IngressStatus) {
	cc := cs.GetCondition(netv1alpha1.IngressConditionReady)
	if cc == nil {
		dms.MarkIngressNotConfigured()
		return
	}

	m := domainMappingCondSet.Manage(dms)
	switch cc.Status {
	case corev1.ConditionTrue:
		m.MarkTrue(DomainMappingConditionIngressReady)
	case corev1.ConditionFalse:
		m.MarkFalse(DomainMappingConditionIngressReady, cc.Reason, cc.Message)
	case corev1.ConditionUnknown:
		m.MarkUnknown(DomainMappingConditionIngressReady, cc.Reason, cc.Message)
	}
}
