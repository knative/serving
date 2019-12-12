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
	"text/template"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	kubelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	rbase "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/nscert/config"
	"knative.dev/serving/pkg/reconciler/nscert/resources"
)

// Reconciler implements controller.Reconciler for Certificate resources.
type reconciler struct {
	*rbase.Base

	// listers index properties about resources
	nsLister            kubelisters.NamespaceLister
	knCertificateLister listers.CertificateLister

	configStore configStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*reconciler)(nil)
var domainTemplateRegex *regexp.Regexp = regexp.MustCompile(`^\*\..+$`)

// Reconciler implements controller.Reconciler for Namespace resources.
func (c *reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	ctx = c.configStore.ToContext(ctx)

	if !config.FromContext(ctx).Network.AutoTLS {
		logger.Debug("AutoTLS is disabled. Skipping wildcard certificate creation")
		return nil
	}

	_, ns, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	namespace, err := c.nsLister.Get(ns)
	if apierrs.IsNotFound(err) {
		logger.Info("Namespace in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}

	if _, ok := namespace.Labels[networking.DisableWildcardCertLabelKey]; ok {
		logger.Infof("Skipping wildcard certificate creation for excluded namespace %s", namespace.Name)
		return nil
	}

	err = c.reconcile(ctx, namespace)
	if err != nil {
		c.Recorder.Event(namespace, corev1.EventTypeWarning, "InternalError", err.Error())
	}
	return err
}

func (c *reconciler) reconcile(ctx context.Context, ns *corev1.Namespace) error {
	cfg := config.FromContext(ctx)

	labelSelector := kubelabels.NewSelector()
	req, err := kubelabels.NewRequirement(networking.WildcardCertDomainLabelKey, selection.Exists, nil)
	if err != nil {
		return fmt.Errorf("failed to create requirement: %v", err)
	}
	labelSelector = labelSelector.Add(*req)

	existingCerts, err := c.knCertificateLister.Certificates(ns.Name).List(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list certificates: %w", err)
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

	desiredCert := resources.MakeWildcardCertificate(ns, dnsName, defaultDomain)

	// If there is no matching cert find one previously created by this reconciler which may
	// need to be updated.
	existingCert, err := findNamespaceCert(ns, existingCerts)

	if apierrs.IsNotFound(err) {
		cert, err := c.ServingClientSet.NetworkingV1alpha1().Certificates(ns.Name).Create(desiredCert)
		if err != nil {
			c.Recorder.Eventf(ns, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Knative certificate %s/%s: %v", ns.Name, desiredCert.ObjectMeta.Name, err)
			return fmt.Errorf("failed to create namespace certificate: %w", err)
		}

		c.Recorder.Eventf(cert, corev1.EventTypeNormal, "Created",
			"Created Knative Certificate %s/%s", ns.Name, cert.ObjectMeta.Name)
	} else if err != nil {
		return fmt.Errorf("failed to get namespace certificate: %w", err)
	} else if !metav1.IsControlledBy(existingCert, ns) {
		return fmt.Errorf("namespace %s does not own Knative Certificate: %s", ns.Name, existingCert.Name)
	} else if !equality.Semantic.DeepEqual(existingCert.Spec, desiredCert.Spec) {
		copy := existingCert.DeepCopy()
		copy.Spec = desiredCert.Spec
		copy.ObjectMeta.Labels[networking.WildcardCertDomainLabelKey] = desiredCert.ObjectMeta.Labels[networking.WildcardCertDomainLabelKey]

		_, err := c.ServingClientSet.NetworkingV1alpha1().Certificates(copy.Namespace).Update(copy)
		if err != nil {
			c.Recorder.Eventf(existingCert, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update Knative Certificate %s/%s: %v", existingCert.Namespace, existingCert.Name, err)
			return fmt.Errorf("failed to update namespace certificate: %w", err)
		}
		c.Recorder.Eventf(existingCert, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Knative Certificate %s/%s", desiredCert.Namespace, desiredCert.Name)
		return nil
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
