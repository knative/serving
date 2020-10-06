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

package nscert

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/networking/pkg/apis/networking/v1alpha1"
	clientset "knative.dev/networking/pkg/client/clientset/versioned"
	listers "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	namespacereconciler "knative.dev/pkg/client/injection/kube/reconciler/core/v1/namespace"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/nscert/config"
	"knative.dev/serving/pkg/reconciler/nscert/resources"
)

// Reconciler implements controller.Reconciler for Certificate resources.
type reconciler struct {
	client clientset.Interface

	// listers index properties about resources
	knCertificateLister listers.CertificateLister
}

// Check that our Reconciler implements namespacereconciler.Interface
var _ namespacereconciler.Interface = (*reconciler)(nil)
var domainTemplateRegex *regexp.Regexp = regexp.MustCompile(`^\*\..+$`)

func certClass(ctx context.Context, r *corev1.Namespace) string {
	if class := r.Annotations[networking.CertificateClassAnnotationKey]; class != "" {
		return class
	}
	return config.FromContext(ctx).Network.DefaultCertificateClass
}

func (c *reconciler) ReconcileKind(ctx context.Context, ns *corev1.Namespace) pkgreconciler.Event {
	cfg := config.FromContext(ctx)

	labelSelector := kubelabels.NewSelector()
	req, err := kubelabels.NewRequirement(networking.WildcardCertDomainLabelKey, selection.Exists, nil)
	if err != nil {
		return fmt.Errorf("failed to create requirement: %w", err)
	}
	labelSelector = labelSelector.Add(*req)

	existingCerts, err := c.knCertificateLister.Certificates(ns.Name).List(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
	}

	disabledWildcardCertValue, hasDisabledWildcardCertValue := ns.Labels[networking.DisableWildcardCertLabelKey]
	deprecatedDisabledWildcardCertValue, hasDeprecatedDisabledWildcardCertValue := ns.Labels[networking.DeprecatedDisableWildcardCertLabelKey]

	if hasDisabledWildcardCertValue && hasDeprecatedDisabledWildcardCertValue && !strings.EqualFold(disabledWildcardCertValue, deprecatedDisabledWildcardCertValue) {
		return fmt.Errorf("both %s and %s are specified but values do not match", networking.DisableWildcardCertLabelKey, networking.DeprecatedDisableWildcardCertLabelKey)
	}

	if strings.EqualFold(disabledWildcardCertValue, "true") ||
		strings.EqualFold(deprecatedDisabledWildcardCertValue, "true") {
		return c.deleteNamespaceCerts(ctx, ns, existingCerts)
	}

	// Only create wildcard certs for the default domain
	defaultDomain := cfg.Domain.LookupDomainForLabels(nil /* labels */)

	dnsName, err := wildcardDomain(cfg.Network.DomainTemplate, defaultDomain, ns.Name)
	if err != nil {
		return fmt.Errorf("failed to apply domain template %s to domain %s and namespace %s: %w",
			cfg.Network.DomainTemplate, defaultDomain, ns.Name, err)
	}

	// If any labeled cert has been issued for our DNSName then there's nothing to do
	matchingCert := findMatchingCert(dnsName, existingCerts)
	if matchingCert != nil {
		return nil
	}
	recorder := controller.GetEventRecorder(ctx)

	desiredCert := resources.MakeWildcardCertificate(ns, dnsName, defaultDomain, certClass(ctx, ns))

	// If there is no matching cert find one previously created by this reconciler which may
	// need to be updated.
	existingCert, err := findNamespaceCert(ns, existingCerts)
	if apierrs.IsNotFound(err) {
		cert, err := c.client.NetworkingV1alpha1().Certificates(ns.Name).Create(ctx, desiredCert, metav1.CreateOptions{})
		if err != nil {
			recorder.Eventf(ns, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Knative certificate %s/%s: %v", ns.Name, desiredCert.ObjectMeta.Name, err)
			return fmt.Errorf("failed to create namespace certificate: %w", err)
		}

		recorder.Eventf(cert, corev1.EventTypeNormal, "Created",
			"Created Knative Certificate %s/%s", ns.Name, cert.ObjectMeta.Name)
	} else if err != nil {
		return fmt.Errorf("failed to get namespace certificate: %w", err)
	} else if !metav1.IsControlledBy(existingCert, ns) {
		return fmt.Errorf("namespace %s does not own Knative Certificate: %s", ns.Name, existingCert.Name)
	} else if !equality.Semantic.DeepEqual(existingCert.Spec, desiredCert.Spec) {
		copy := existingCert.DeepCopy()
		copy.Spec = desiredCert.Spec
		copy.Labels[networking.WildcardCertDomainLabelKey] = desiredCert.Labels[networking.WildcardCertDomainLabelKey]

		if _, err := c.client.NetworkingV1alpha1().Certificates(copy.Namespace).Update(ctx, copy, metav1.UpdateOptions{}); err != nil {
			recorder.Eventf(existingCert, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update Knative Certificate %s/%s: %v", existingCert.Namespace, existingCert.Name, err)
			return fmt.Errorf("failed to update namespace certificate: %w", err)
		}
		recorder.Eventf(existingCert, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Knative Certificate %s/%s", desiredCert.Namespace, desiredCert.Name)
		return nil
	}

	return nil
}

func (c *reconciler) deleteNamespaceCerts(ctx context.Context, ns *corev1.Namespace, certs []*v1alpha1.Certificate) error {
	recorder := controller.GetEventRecorder(ctx)
	for _, cert := range certs {
		if metav1.IsControlledBy(cert, ns) {
			if err := c.client.NetworkingV1alpha1().Certificates(cert.Namespace).Delete(ctx, cert.Name, metav1.DeleteOptions{}); err != nil {
				recorder.Eventf(cert, corev1.EventTypeNormal, "DeleteFailed",
					"Failed to delete Knative Certificate %s/%s: %v", cert.Namespace, cert.Name, err)
				return err
			}
			recorder.Eventf(cert, corev1.EventTypeNormal, "Deleted",
				"Deleted Knative Certificate %s/%s", cert.Namespace, cert.Name)
		}
	}
	return nil
}

func wildcardDomain(tmpl, domain, namespace string) (string, error) {
	data := network.DomainTemplateValues{
		Name:      "*",
		Domain:    domain,
		Namespace: namespace,
	}

	t, err := template.New("domain-template").Parse(tmpl)
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	dom := buf.String()
	if !domainTemplateRegex.MatchString(dom) {
		return "", fmt.Errorf("invalid DomainTemplate: %s", dom)
	}
	return dom, nil
}

func findMatchingCert(domain string, certs []*v1alpha1.Certificate) *v1alpha1.Certificate {
	for _, cert := range certs {
		if dnsNames := sets.NewString(cert.Spec.DNSNames...); dnsNames.Has(domain) {
			return cert
		}
	}
	return nil
}

func findNamespaceCert(ns *corev1.Namespace, certs []*v1alpha1.Certificate) (*v1alpha1.Certificate, error) {
	for _, cert := range certs {
		if metav1.IsControlledBy(cert, ns) {
			return cert, nil
		}
	}
	return nil, apierrs.NewNotFound(v1alpha1.Resource("certificate"), ns.Name)
}
