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
	"reflect"
	"strconv"

	cmv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	kubelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/pkg/apis"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	certmanagerclientset "knative.dev/serving/pkg/client/certmanager/clientset/versioned"
	certmanagerlisters "knative.dev/serving/pkg/client/certmanager/listers/certmanager/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/certificate/config"
	"knative.dev/serving/pkg/reconciler/certificate/resources"
)

const (
	noCMConditionReason  = "NoCertManagerCertCondition"
	noCMConditionMessage = "The ready condition of Cert Manager Certifiate does not exist."
	notReconciledReason  = "ReconcileFailed"
	notReconciledMessage = "Cert-Manager certificate has not yet been reconciled."
	httpDomainLabel      = "certmanager.k8s.io/acme-http-domain"
	httpChallengePath    = "/.well-known/acme-challenge"
)

// Reconciler implements controller.Reconciler for Certificate resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	knCertificateLister listers.CertificateLister
	cmCertificateLister certmanagerlisters.CertificateLister
	cmChallengeLister   certmanagerlisters.ChallengeLister
	svcLister           kubelisters.ServiceLister
	certManagerClient   certmanagerclientset.Interface
	tracker             tracker.Interface

	configStore reconciler.ConfigStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Certificate resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	ctx = c.configStore.ToContext(ctx)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	original, err := c.knCertificateLister.Certificates(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		logger.Errorf("Knative Certificate %s in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy
	knCert := original.DeepCopy()

	// Reconcile this copy of the Certificate and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, knCert)
	if err != nil {
		logger.Warnw("Failed to reconcile certificate", zap.Error(err))
		c.Recorder.Event(knCert, corev1.EventTypeWarning, "InternalError", err.Error())
		knCert.Status.MarkNotReady(notReconciledReason, notReconciledMessage)
	}
	if equality.Semantic.DeepEqual(original.Status, knCert.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(knCert); err != nil {
		logger.Warnw("Failed to update certificate status", zap.Error(err))
		c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Certificate %s: %v", key, err)
		return err
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, knCert *v1alpha1.Certificate) error {
	logger := logging.FromContext(ctx)

	knCert.SetDefaults(ctx)
	knCert.Status.InitializeConditions()

	logger.Infof("Reconciling Cert-Manager certificate for Knative cert %s/%s.", knCert.Namespace, knCert.Name)
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
	switch {
	case cmCertReadyCondition == nil:
		knCert.Status.MarkNotReady(noCMConditionReason, noCMConditionMessage)
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionUnknown:
		knCert.Status.MarkNotReady(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionTrue:
		knCert.Status.MarkReady()
		knCert.Status.HTTP01Challenges = []v1alpha1.HTTP01Challenge{}
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionFalse:
		knCert.Status.MarkFailed(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
	}

	http01Domains := getHTTP01Domains(cmCert)
	if !knCert.Status.IsReady() && len(http01Domains) > 0 {
		err := c.setHTTP01Challenges(knCert, http01Domains)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Reconciler) reconcileCMCertificate(ctx context.Context, knCert *v1alpha1.Certificate, desired *cmv1alpha1.Certificate) (*cmv1alpha1.Certificate, error) {
	cmCert, err := c.cmCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		cmCert, err = c.certManagerClient.CertmanagerV1alpha1().Certificates(desired.Namespace).Create(desired)
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
		updated, err := c.certManagerClient.CertmanagerV1alpha1().Certificates(copy.Namespace).Update(copy)
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

func (c *Reconciler) updateStatus(desired *v1alpha1.Certificate) (*v1alpha1.Certificate, error) {
	cert, err := c.knCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(cert.Status, desired.Status) {
		return cert, nil
	}
	// Don't modify the informers copy
	existing := cert.DeepCopy()
	existing.Status = desired.Status

	return c.ServingClientSet.NetworkingV1alpha1().Certificates(existing.Namespace).UpdateStatus(existing)
}

func (c *Reconciler) setHTTP01Challenges(knCert *v1alpha1.Certificate, http01Domains []string) error {
	challenges := make([]v1alpha1.HTTP01Challenge, 0, len(http01Domains))
	for _, dnsName := range http01Domains {
		// This selector comes from https://github.com/jetstack/cert-manager/blob/1b9b83a4b80068207b0a8070dadb0e760f5095f6/pkg/issuer/acme/http/pod.go#L34
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
			return fmt.Errorf("no challenge solver service for domain %s.", dnsName)
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

func getHTTP01Domains(cmCert *cmv1alpha1.Certificate) []string {
	domains := make([]string, 0, len(cmCert.Spec.DNSNames))
	if cmCert.Spec.ACME == nil {
		return domains
	}

	for _, domainConfig := range cmCert.Spec.ACME.Config {
		if domainConfig.SolverConfig.HTTP01 != nil {
			domains = append(domains, domainConfig.Domains...)
		}
	}
	return domains
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
