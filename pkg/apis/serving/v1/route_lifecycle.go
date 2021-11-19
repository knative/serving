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

package v1

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
)

var routeCondSet = apis.NewLivingConditionSet(
	RouteConditionAllTrafficAssigned,
	RouteConditionIngressReady,
	RouteConditionCertificateProvisioned,
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*Route) GetConditionSet() apis.ConditionSet {
	return routeCondSet
}

// GetGroupVersionKind returns the GroupVersionKind.
func (r *Route) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Route")
}

// IsReady returns true if the Status condition RouteConditionReady
// is true and the latest spec has been observed.
func (r *Route) IsReady() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RouteConditionReady).IsTrue()
}

// IsFailed returns true if the resource has observed
// the latest generation and ready is false.
func (r *Route) IsFailed() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RouteConditionReady).IsFalse()
}

// RolloutDuration returns the rollout duration specified as an
// annotation.
// 0 is returned if missing or cannot be parsed.
func (r *Route) RolloutDuration() time.Duration {
	if _, v, ok := serving.RolloutDurationAnnotation.Get(r.Annotations); ok && v != "" {
		// WH should've declined all the invalid values for this annotation.
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 0
}

// InitializeConditions sets the initial values to the conditions.
func (rs *RouteStatus) InitializeConditions() {
	routeCondSet.Manage(rs).InitializeConditions()
}

// MarkServiceNotOwned changes the IngressReady status to be false with the reason being that
// there is a pre-existing placeholder service with the name we wanted to use.
func (rs *RouteStatus) MarkServiceNotOwned(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionIngressReady, "NotOwned",
		fmt.Sprintf("There is an existing placeholder Service %q that we do not own.", name))
}

// MarkEndpointNotOwned changes the IngressReady status to be false with the reason being that
// there is a pre-existing placeholder endpoint with the name we wanted to use.
func (rs *RouteStatus) MarkEndpointNotOwned(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionIngressReady, "NotOwned",
		fmt.Sprintf("There is an existing placeholder Endpoint %q that we do not own.", name))
}

// MarkIngressRolloutInProgress changes the IngressReady condition to be unknown to reflect
// that a gradual rollout of the latest new revision (or stacked revisions) is in progress.
func (rs *RouteStatus) MarkIngressRolloutInProgress() {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionIngressReady,
		"RolloutInProgress", "A gradual rollout of the latest revision(s) is in progress.")
}

// MarkIngressNotConfigured changes the IngressReady condition to be unknown to reflect
// that the Ingress does not yet have a Status
func (rs *RouteStatus) MarkIngressNotConfigured() {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionIngressReady,
		"IngressNotConfigured", "Ingress has not yet been reconciled.")
}

// MarkTrafficAssigned marks the RouteConditionAllTrafficAssigned condition true.
func (rs *RouteStatus) MarkTrafficAssigned() {
	routeCondSet.Manage(rs).MarkTrue(RouteConditionAllTrafficAssigned)
}

// MarkUnknownTrafficError marks the RouteConditionAllTrafficAssigned condition
// to indicate an error has occurred.
func (rs *RouteStatus) MarkUnknownTrafficError(msg string) {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionAllTrafficAssigned, "Unknown", msg)
}

// MarkConfigurationNotReady marks the RouteConditionAllTrafficAssigned
// condition to indiciate the Revision is not yet ready.
func (rs *RouteStatus) MarkConfigurationNotReady(name string) {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionAllTrafficAssigned,
		"RevisionMissing",
		"Configuration %q is waiting for a Revision to become ready.", name)
}

// MarkConfigurationFailed marks the RouteConditionAllTrafficAssigned condition
// to indicate the Revision has failed.
func (rs *RouteStatus) MarkConfigurationFailed(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionAllTrafficAssigned,
		"RevisionMissing",
		"Configuration %q does not have any ready Revision.", name)
}

// MarkRevisionNotReady marks the RouteConditionAllTrafficAssigned condition to
// indiciate the Revision is not yet ready.
func (rs *RouteStatus) MarkRevisionNotReady(name string) {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionAllTrafficAssigned,
		"RevisionMissing",
		"Revision %q is not yet ready.", name)
}

// MarkRevisionFailed marks the RouteConditionAllTrafficAssigned condition to
// indiciate the Revision has failed.
func (rs *RouteStatus) MarkRevisionFailed(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionAllTrafficAssigned,
		"RevisionMissing",
		"Revision %q failed to become ready.", name)
}

// MarkMissingTrafficTarget marks the RouteConditionAllTrafficAssigned
// condition to indicate a reference traffic target was not found.
func (rs *RouteStatus) MarkMissingTrafficTarget(kind, name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionAllTrafficAssigned,
		kind+"Missing",
		"%s %q referenced in traffic not found.", kind, name)
}

// MarkCertificateProvisionFailed marks the
// RouteConditionCertificateProvisioned condition to indicate that the
// Certificate provisioning failed.
func (rs *RouteStatus) MarkCertificateProvisionFailed(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionCertificateProvisioned,
		"CertificateProvisionFailed",
		"Certificate %s fails to be provisioned.", name)
}

// MarkCertificateReady marks the RouteConditionCertificateProvisioned
// condition to indicate that the Certificate is ready.
func (rs *RouteStatus) MarkCertificateReady(name string) {
	routeCondSet.Manage(rs).MarkTrue(RouteConditionCertificateProvisioned)
}

// MarkCertificateNotReady marks the RouteConditionCertificateProvisioned
// condition to indicate that the Certificate is not ready.
func (rs *RouteStatus) MarkCertificateNotReady(name string) {
	routeCondSet.Manage(rs).MarkUnknown(RouteConditionCertificateProvisioned,
		"CertificateNotReady",
		"Certificate %s is not ready.", name)
}

// MarkCertificateNotOwned changes the RouteConditionCertificateProvisioned
// status to be false with the reason being that there is an existing
// certificate with the name we wanted to use.
func (rs *RouteStatus) MarkCertificateNotOwned(name string) {
	routeCondSet.Manage(rs).MarkFalse(RouteConditionCertificateProvisioned,
		"CertificateNotOwned",
		"There is an existing certificate %s that we don't own.", name)
}

const (
	// AutoTLSNotEnabledMessage is the message which is set on the
	// RouteConditionCertificateProvisioned condition when it is set to True
	// because AutoTLS was not enabled.
	AutoTLSNotEnabledMessage = "autoTLS is not enabled"

	// TLSNotEnabledForClusterLocalMessage is the message which is set on the
	// RouteConditionCertificateProvisioned condition when it is set to True
	// because the domain is cluster-local.
	TLSNotEnabledForClusterLocalMessage = "TLS is not enabled for cluster-local"
)

// MarkTLSNotEnabled sets RouteConditionCertificateProvisioned to true when
// certificate config such as autoTLS is not enabled or private cluster-local service.
func (rs *RouteStatus) MarkTLSNotEnabled(msg string) {
	routeCondSet.Manage(rs).MarkTrueWithReason(RouteConditionCertificateProvisioned,
		"TLSNotEnabled", msg)
}

// MarkHTTPDowngrade sets RouteConditionCertificateProvisioned to true when plain
// HTTP is enabled even when Certificated is not ready.
func (rs *RouteStatus) MarkHTTPDowngrade(name string) {
	routeCondSet.Manage(rs).MarkTrueWithReason(RouteConditionCertificateProvisioned,
		"HTTPDowngrade",
		"Certificate %s is not ready downgrade HTTP.", name)
}

// PropagateIngressStatus update RouteConditionIngressReady condition
// in RouteStatus according to IngressStatus.
func (rs *RouteStatus) PropagateIngressStatus(cs v1alpha1.IngressStatus) {
	cc := cs.GetCondition(v1alpha1.IngressConditionReady)
	if cc == nil {
		rs.MarkIngressNotConfigured()
		return
	}

	m := routeCondSet.Manage(rs)
	switch cc.Status {
	case corev1.ConditionTrue:
		m.MarkTrue(RouteConditionIngressReady)
	case corev1.ConditionFalse:
		m.MarkFalse(RouteConditionIngressReady, cc.Reason, cc.Message)
	case corev1.ConditionUnknown:
		m.MarkUnknown(RouteConditionIngressReady, cc.Reason, cc.Message)
	}
}
