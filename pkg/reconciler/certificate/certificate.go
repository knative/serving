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

package certificate

import (
	"context"
	"fmt"
	"hash/adler32"
	"strconv"

	"k8s.io/apimachinery/pkg/util/sets"

	cmv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	kubelisters "k8s.io/client-go/listers/core/v1"
	certreconciler "knative.dev/serving/pkg/client/injection/reconciler/networking/v1alpha1/certificate"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	certmanagerclientset "knative.dev/serving/pkg/client/certmanager/clientset/versioned"
	acmelisters "knative.dev/serving/pkg/client/certmanager/listers/acme/v1alpha2"
	certmanagerlisters "knative.dev/serving/pkg/client/certmanager/listers/certmanager/v1alpha2"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/certificate/config"
	"knative.dev/serving/pkg/reconciler/certificate/resources"
)

const (
	noCMConditionReason  = "NoCertManagerCertCondition"
	noCMConditionMessage = "The ready condition of Cert Manager Certifiate does not exist."
	notReconciledReason  = "ReconcileFailed"
	notReconciledMessage = "Cert-Manager certificate has not yet been reconciled."
	httpDomainLabel      = "acme.cert-manager.io/http-domain"
	httpChallengePath    = "/.well-known/acme-challenge"
)

// It comes from cert-manager status:
// https://github.com/jetstack/cert-manager/blob/b7e83b53820e712e7cf6b8dce3e5a050f249da79/pkg/controller/certificates/sync.go#L130
var notReadyReasons = sets.NewString("InProgress", "Pending", "TemporaryCertificate")

// Reconciler implements controller.Reconciler for Certificate resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	cmCertificateLister certmanagerlisters.CertificateLister
	cmChallengeLister   acmelisters.ChallengeLister
	cmIssuerLister      certmanagerlisters.ClusterIssuerLister
	svcLister           kubelisters.ServiceLister
	certManagerClient   certmanagerclientset.Interface
	tracker             tracker.Interface
}

// Check that our Reconciler implements certreconciler.Interface
var _ certreconciler.Interface = (*Reconciler)(nil)

func (c *Reconciler) ReconcileKind(ctx context.Context, knCert *v1alpha1.Certificate) pkgreconciler.Event {
	// Reconcile this copy of the Certificate and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err := c.reconcile(ctx, knCert)
	if err != nil {
		knCert.Status.MarkNotReady(notReconciledReason, notReconciledMessage)
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, knCert *v1alpha1.Certificate) error {
	logger := logging.FromContext(ctx)

	knCert.SetDefaults(ctx)
	knCert.Status.InitializeConditions()

	logger.Info("Reconciling Cert-Manager certificate for Knative cert.")
	knCert.Status.ObservedGeneration = knCert.Generation

	cmConfig := config.FromContext(ctx).CertManager

	cmCert := resources.MakeCertManagerCertificate(cmConfig, knCert)
	cmCert, err := c.reconcileCMCertificate(ctx, knCert, cmCert)
	if err != nil {
		return err
	}

	knCert.Status.NotAfter = cmCert.Status.NotAfter
	// Propagate cert-manager Certificate status to Knative Certificate.
	cmCertReadyCondition := resources.GetReadyCondition(cmCert)
	logger.Infof("cm cert condition %v.", cmCertReadyCondition)

	switch {
	case cmCertReadyCondition == nil:
		knCert.Status.MarkNotReady(noCMConditionReason, noCMConditionMessage)
		return c.setHTTP01Challenges(knCert, cmCert)
	case cmCertReadyCondition.Status == cmmeta.ConditionUnknown:
		knCert.Status.MarkNotReady(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
		return c.setHTTP01Challenges(knCert, cmCert)
	case cmCertReadyCondition.Status == cmmeta.ConditionTrue:
		knCert.Status.MarkReady()
		knCert.Status.HTTP01Challenges = []v1alpha1.HTTP01Challenge{}
	case cmCertReadyCondition.Status == cmmeta.ConditionFalse:
		if notReadyReasons.Has(cmCertReadyCondition.Reason) {
			knCert.Status.MarkNotReady(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
		} else {
			knCert.Status.MarkFailed(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
		}
		return c.setHTTP01Challenges(knCert, cmCert)
	}
	return nil
}

func (c *Reconciler) reconcileCMCertificate(ctx context.Context, knCert *v1alpha1.Certificate, desired *cmv1alpha2.Certificate) (*cmv1alpha2.Certificate, error) {
	cmCert, err := c.cmCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		cmCert, err = c.certManagerClient.CertmanagerV1alpha2().Certificates(desired.Namespace).Create(desired)
		if err != nil {
			c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Cert-Manager Certificate %s/%s: %v", desired.Name, desired.Namespace, err)
			return nil, fmt.Errorf("failed to create Cert-Manager Certificate: %w", err)
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Created",
			"Created Cert-Manager Certificate %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return nil, fmt.Errorf("failed to get Cert-Manager Certificate: %w", err)
	} else if !metav1.IsControlledBy(desired, knCert) {
		knCert.Status.MarkResourceNotOwned("CertManagerCertificate", desired.Name)
		return nil, fmt.Errorf("knative Certificate %s in namespace %s does not own CertManager Certificate: %s", knCert.Name, knCert.Namespace, desired.Name)
	} else if !equality.Semantic.DeepEqual(cmCert.Spec, desired.Spec) {
		copy := cmCert.DeepCopy()
		copy.Spec = desired.Spec
		updated, err := c.certManagerClient.CertmanagerV1alpha2().Certificates(copy.Namespace).Update(copy)
		if err != nil {
			c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to create Cert-Manager Certificate %s/%s: %v", desired.Namespace, desired.Name, err)
			return nil, fmt.Errorf("failed to update Cert-Manager Certificate: %w", err)
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Cert-Manager Certificate %s/%s", desired.Namespace, desired.Name)
		return updated, nil
	}
	return cmCert, nil
}

func (c *Reconciler) setHTTP01Challenges(knCert *v1alpha1.Certificate, cmCert *cmv1alpha2.Certificate) error {
	if isHTTP, err := c.isHTTPChallenge(cmCert); err != nil {
		return err
	} else if !isHTTP {
		return nil
	}
	challenges := make([]v1alpha1.HTTP01Challenge, 0, len(cmCert.Spec.DNSNames))
	for _, dnsName := range cmCert.Spec.DNSNames {
		// This selector comes from:
		// https://github.com/jetstack/cert-manager/blob/1b9b83a4b80068207b0a8070dadb0e760f5095f6/pkg/issuer/acme/http/pod.go#L34
		selector := labels.NewSelector()
		value := strconv.FormatUint(uint64(adler32.Checksum([]byte(dnsName))), 10)
		req, err := labels.NewRequirement(httpDomainLabel, selection.Equals, []string{value})
		if err != nil {
			return fmt.Errorf("failed to create requirement %s=%s: %w", httpDomainLabel, value, err)
		}
		selector = selector.Add(*req)

		svcs, err := c.svcLister.Services(knCert.Namespace).List(selector)
		if err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}
		if len(svcs) == 0 {
			return fmt.Errorf("no challenge solver service for domain %s; selector=%v", dnsName, selector)
		}

		for _, svc := range svcs {
			if err := c.tracker.Track(svcRef(svc.Namespace, svc.Name), knCert); err != nil {
				return err
			}
			owner := svc.GetOwnerReferences()[0]
			cmChallenge, err := c.cmChallengeLister.Challenges(knCert.Namespace).Get(owner.Name)
			if err != nil {
				return err
			}

			challenge := v1alpha1.HTTP01Challenge{
				ServiceName:      svc.Name,
				ServicePort:      svc.Spec.Ports[0].TargetPort,
				ServiceNamespace: svc.Namespace,
				URL: &apis.URL{
					Scheme: "http",
					Path:   fmt.Sprintf("%s/%s", httpChallengePath, cmChallenge.Spec.Token),
					Host:   cmChallenge.Spec.DNSName,
				},
			}
			challenges = append(challenges, challenge)
		}
	}
	knCert.Status.HTTP01Challenges = challenges
	return nil
}

func (c *Reconciler) isHTTPChallenge(cmCert *cmv1alpha2.Certificate) (bool, error) {
	if issuer, err := c.cmIssuerLister.Get(cmCert.Spec.IssuerRef.Name); err != nil {
		return false, err
	} else {
		return issuer.Spec.ACME != nil &&
			len(issuer.Spec.ACME.Solvers) > 0 &&
			issuer.Spec.ACME.Solvers[0].HTTP01 != nil, nil
	}
}

func svcRef(namespace, name string) corev1.ObjectReference {
	gvk := corev1.SchemeGroupVersion.WithKind("Service")
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
	}
}
