/*
Copyright 2018 The Knative Authors

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

package route

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/utils/clock"

	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	networkinglisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	routereconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1/route"
	listers "knative.dev/serving/pkg/client/listers/serving/v1"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
	networkaccessor "knative.dev/serving/pkg/reconciler/accessor/networking"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	resourcenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
	"knative.dev/serving/pkg/reconciler/route/visibility"
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	kubeclient kubernetes.Interface
	client     clientset.Interface
	netclient  netclientset.Interface

	// Listers index properties about resources
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
	serviceLister       corev1listers.ServiceLister
	endpointsLister     corev1listers.EndpointsLister
	ingressLister       networkinglisters.IngressLister
	certificateLister   networkinglisters.CertificateLister
	tracker             tracker.Interface

	clock        clock.PassiveClock
	enqueueAfter func(interface{}, time.Duration)
}

const errorConfigMsg = "ErrorConfig"

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)

func ingressClassForRoute(ctx context.Context, r *v1.Route) string {
	if ingressClass := networking.GetIngressClass(r.Annotations); ingressClass != "" {
		return ingressClass
	}
	return config.FromContext(ctx).Network.DefaultIngressClass
}

func certClass(ctx context.Context, r *v1.Route) string {
	if class := networking.GetCertificateClass(r.Annotations); class != "" {
		return class
	}
	return config.FromContext(ctx).Network.DefaultCertificateClass
}

// ReconcileKind implements Interface.ReconcileKind.
func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	ctx, cancel := context.WithTimeout(ctx, pkgreconciler.DefaultTimeout)
	defer cancel()

	logger := logging.FromContext(ctx)
	logger.Debugf("Reconciling route: %#v", r.Spec)

	// When a new generation is observed for the first time, we need to make sure that we
	// do not report ourselves as being ready prematurely due to an error during
	// reconciliation.  For instance, if we were to hit an error creating new placeholder
	// service, we might report "Ready: True" with a bumped ObservedGeneration without
	// having updated the kingress at all!
	// We hit this in: https://github.com/knative-extensions/net-contour/issues/238
	if r.GetObjectMeta().GetGeneration() != r.Status.ObservedGeneration {
		r.Status.MarkIngressNotConfigured()
	}

	// Configure traffic based on the RouteSpec.
	traffic, err := c.configureTraffic(ctx, r)
	if traffic == nil || err != nil {
		if err != nil {
			if errors.Is(err, domains.ErrorDomainName) {
				r.Status.MarkRevisionTargetTrafficError(errorConfigMsg, err.Error())
			} else {
				r.Status.MarkUnknownTrafficError(err.Error())
			}
		}
		// Traffic targets aren't ready, no need to configure child resources.
		return err
	}

	r.Status.Address = &duckv1.Addressable{
		URL: &apis.URL{
			Scheme: "http",
			Host:   resourcenames.K8sServiceFullname(r),
		},
	}

	logger.Info("Creating placeholder k8s services")
	services, err := c.reconcilePlaceholderServices(ctx, r, traffic.Targets)
	if err != nil {
		return err
	}

	tls, acmeChallenges, desiredCerts, err := c.externalDomainTLS(ctx, r.Status.URL.Host, r, traffic)
	if err != nil {
		return err
	}

	if config.FromContext(ctx).Network.ClusterLocalDomainTLS == netcfg.EncryptionEnabled {
		internalTLS, err := c.clusterLocalDomainTLS(ctx, r, traffic)
		if err != nil {
			return err
		}
		tls = append(tls, internalTLS...)
	} else if externalDomainTLSEnabled(ctx, r) && len(desiredCerts) == 0 {
		// If external TLS is enabled but we have no desired certs then the route
		// must have only cluster local hosts
		r.Status.MarkTLSNotEnabled(v1.TLSNotEnabledForClusterLocalMessage)
	}

	// Reconcile ingress and its children resources.
	ingress, effectiveRO, err := c.reconcileIngress(ctx, r, traffic, tls, ingressClassForRoute(ctx, r), acmeChallenges...)
	if err != nil {
		return err
	}

	roInProgress := !effectiveRO.Done()
	if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration {
		r.Status.MarkIngressNotConfigured()
	} else if !roInProgress {
		r.Status.PropagateIngressStatus(ingress.Status)
	}

	logger.Info("Updating placeholder k8s services with ingress information")
	if err := c.updatePlaceholderServices(ctx, r, services, ingress); err != nil {
		return err
	}

	// We do it here, rather than in the similar check above,
	// since we might be inside a rollout and Ingress
	// is not yet ready and that takes priority, so the `roInProgress` branch
	// will not be triggered.
	if roInProgress {
		logger.Info("Rollout is in progress")
		// Rollout in progress, so mark the status as such.
		r.Status.MarkIngressRolloutInProgress()
		// Update the route.Status.Traffic to contain correct traffic
		// distribution based on rollout status.
		r.Status.Traffic, err = traffic.GetRevisionTrafficTargets(ctx, r, effectiveRO)
		if err != nil {
			return err
		}
		return nil
	}

	logger.Info("Route successfully synced")
	return nil
}

func (c *Reconciler) externalDomainTLS(ctx context.Context, host string, r *v1.Route, traffic *traffic.Config) (
	[]netv1alpha1.IngressTLS,
	[]netv1alpha1.HTTP01Challenge,
	[]*netv1alpha1.Certificate,
	error,
) {
	var desiredCerts []*netv1alpha1.Certificate
	logger := logging.FromContext(ctx)

	tls := []netv1alpha1.IngressTLS{}
	if !externalDomainTLSEnabled(ctx, r) {
		r.Status.MarkTLSNotEnabled(v1.ExternalDomainTLSNotEnabledMessage)
		return tls, nil, desiredCerts, nil
	}

	domainToTagMap, err := domains.GetAllDomainsAndTags(ctx, r, getTrafficNames(traffic.Targets), traffic.Visibility)
	if err != nil {
		return nil, nil, desiredCerts, err
	}

	for domain := range domainToTagMap {
		// Ignore cluster local domains here, as their TLS is handled in clusterLocalDomainTLS
		if domains.IsClusterLocal(domain) {
			delete(domainToTagMap, domain)
		}
	}

	routeDomain := config.FromContext(ctx).Domain.LookupDomainForLabels(r.Labels)
	labelSelector := kubelabels.SelectorFromSet(kubelabels.Set{
		networking.WildcardCertDomainLabelKey: routeDomain,
	})

	allWildcardCerts, err := c.certificateLister.Certificates(r.Namespace).List(labelSelector)
	if err != nil {
		return nil, nil, desiredCerts, err
	}

	domainConfig := config.FromContext(ctx).Domain
	rLabels := r.Labels
	domain := domainConfig.LookupDomainForLabels(rLabels)

	acmeChallenges := []netv1alpha1.HTTP01Challenge{}
	desiredCerts = resources.MakeCertificates(r, domainToTagMap, certClass(ctx, r), domain)
	for _, desiredCert := range desiredCerts {
		dnsNames := sets.New(desiredCert.Spec.DNSNames...)
		// Look for a matching wildcard cert before provisioning a new one. This saves the
		// the time required to provision a new cert and reduces the chances of hitting the
		// Let's Encrypt API rate limits.
		cert := findMatchingWildcardCert(ctx, desiredCert.Spec.DNSNames, allWildcardCerts)

		if cert == nil {
			cert, err = networkaccessor.ReconcileCertificate(ctx, r, desiredCert, c)
			if err != nil {
				if kaccessor.IsNotOwned(err) {
					r.Status.MarkCertificateNotOwned(desiredCert.Name)
				} else {
					r.Status.MarkCertificateProvisionFailed(desiredCert)
				}
				return nil, nil, desiredCerts, err
			}
			dnsNames = sets.New(cert.Spec.DNSNames...)
		}

		// r.Status.URL is for the major domain, so only change if the cert is for
		// the major domain
		if dnsNames.Has(host) {
			r.Status.URL.Scheme = "https"
		}
		// TODO: we should only mark https for the public visible targets when
		// we are able to configure visibility per target.
		setTargetsScheme(&r.Status, sets.List(dnsNames), "https")

		if cert.IsReady() {
			if renewingCondition := cert.Status.GetCondition("Renewing"); renewingCondition != nil {
				if renewingCondition.Status == corev1.ConditionTrue {
					logger.Infof("Renewing Condition detected on Cert (%s), will attempt creating new challenges.", cert.Name)
					if len(cert.Status.HTTP01Challenges) == 0 {
						//Not sure log level this should be at.
						//It is possible for certs to be renewed without getting
						//validated again, for example, LetsEncrypt will cache
						//validation results. See
						//[here](https://letsencrypt.org/docs/faq/#i-successfully-renewed-a-certificate-but-validation-didn-t-happen-this-time-how-is-that-possible)
						logger.Infof("No HTTP01Challenges found on Cert (%s).", cert.Name)
					}
					acmeChallenges = append(acmeChallenges, cert.Status.HTTP01Challenges...)
				}
			}
			r.Status.MarkCertificateReady(cert.Name)
			tls = append(tls, resources.MakeIngressTLS(cert, sets.List(dnsNames)))
		} else {
			acmeChallenges = append(acmeChallenges, cert.Status.HTTP01Challenges...)
			if cert.IsFailed() {
				r.Status.MarkCertificateProvisionFailed(cert)
			} else {
				r.Status.MarkCertificateNotReady(cert)
			}
			// When httpProtocol is enabled, downgrade http scheme.
			// Explicitly not using the override settings here as to not to muck with
			// external-domain-tls semantics.
			if config.FromContext(ctx).Network.HTTPProtocol == netcfg.HTTPEnabled {
				if dnsNames.Has(host) {
					r.Status.URL = &apis.URL{
						Scheme: "http",
						Host:   host,
					}
				}
				setTargetsScheme(&r.Status, sets.List(dnsNames), "http")
				r.Status.MarkHTTPDowngrade(cert.Name)
			}
		}
	}
	sort.Slice(acmeChallenges, func(i, j int) bool {
		return acmeChallenges[i].URL.String() < acmeChallenges[j].URL.String()
	})

	orphanCerts, err := c.getOrphanRouteCerts(r, domainToTagMap, netcfg.CertificateExternalDomain)
	if err != nil {
		return nil, nil, desiredCerts, err
	}

	c.deleteOrphanedCerts(ctx, orphanCerts)

	return tls, acmeChallenges, desiredCerts, nil
}

func (c *Reconciler) clusterLocalDomainTLS(ctx context.Context, r *v1.Route, tc *traffic.Config) ([]netv1alpha1.IngressTLS, error) {
	tls := []netv1alpha1.IngressTLS{}
	usedDomains := make(map[string]string)

	for name := range tc.Targets {
		localDomains, err := domains.GetDomainsForVisibility(ctx, name, r, netv1alpha1.IngressVisibilityClusterLocal)
		if err != nil {
			return nil, err
		}

		desiredCert := resources.MakeClusterLocalCertificate(r, name, localDomains, certClass(ctx, r))
		cert, err := networkaccessor.ReconcileCertificate(ctx, r, desiredCert, c)
		if err != nil {
			if kaccessor.IsNotOwned(err) {
				r.Status.MarkCertificateNotOwned(desiredCert.Name)
			} else {
				r.Status.MarkCertificateProvisionFailed(desiredCert)
			}
			return nil, err
		}

		if cert.IsReady() {
			r.Status.Address.URL.Scheme = "https"

			// r.Status.URL contains the major domain,
			// so only change if the cert is for the major domain
			if localDomains.Has(r.Status.URL.Host) {
				r.Status.URL.Scheme = "https"
			}

			r.Status.MarkCertificateReady(cert.Name)
			tls = append(tls, resources.MakeIngressTLS(cert, sets.List(localDomains)))
		} else if cert.IsFailed() {
			r.Status.MarkCertificateProvisionFailed(cert)
		} else {
			r.Status.MarkCertificateNotReady(cert)
		}

		for s := range localDomains {
			usedDomains[s] = s
		}
	}

	orphanCerts, err := c.getOrphanRouteCerts(r, usedDomains, netcfg.CertificateClusterLocalDomain)
	if err != nil {
		return nil, nil
	}

	c.deleteOrphanedCerts(ctx, orphanCerts)

	return tls, nil
}

// Returns a slice of certificates that used to belong route's old domains/tags for a specific visibility that are currently not in use.
func (c *Reconciler) getOrphanRouteCerts(r *v1.Route, domainToTagMap map[string]string, certificateType netcfg.CertificateType) ([]*netv1alpha1.Certificate, error) {
	labelSelector := kubelabels.SelectorFromSet(kubelabels.Set{
		serving.RouteLabelKey: r.Name,
	})

	certs, err := c.certificateLister.Certificates(r.Namespace).List(labelSelector)
	if err != nil {
		return nil, err
	}

	var unusedCerts []*netv1alpha1.Certificate
	for _, cert := range certs {
		if v, ok := cert.ObjectMeta.Labels[networking.CertificateTypeLabelKey]; ok && v == string(certificateType) {
			var shouldKeepCert bool
			for _, dn := range cert.Spec.DNSNames {
				if _, used := domainToTagMap[dn]; used {
					shouldKeepCert = true
				}
			}

			if !shouldKeepCert {
				unusedCerts = append(unusedCerts, cert)
			}
		}
	}

	return unusedCerts, nil
}

func (c *Reconciler) deleteOrphanedCerts(ctx context.Context, orphanCerts []*netv1alpha1.Certificate) {
	recorder := controller.GetEventRecorder(ctx)
	for _, cert := range orphanCerts {
		err := c.GetNetworkingClient().NetworkingV1alpha1().Certificates(cert.Namespace).Delete(ctx, cert.Name, metav1.DeleteOptions{})
		if err != nil {
			recorder.Eventf(cert, corev1.EventTypeNormal, "DeleteFailed",
				"Failed to delete orphaned Knative Certificate %s/%s: %v", cert.Namespace, cert.Name, err)
		} else {
			recorder.Eventf(cert, corev1.EventTypeNormal, "Deleted",
				"Deleted orphaned Knative Certificate %s/%s", cert.Namespace, cert.Name)
		}
	}
}

// configureTraffic attempts to configure traffic based on the RouteSpec.  If there are missing
// targets (e.g. Configurations without a Ready Revision, or Revision that isn't Ready or Inactive),
// no traffic will be configured.
//
// If traffic is configured we update the RouteStatus with AllTrafficAssigned = True.  Otherwise we
// mark AllTrafficAssigned = False, with a message referring to one of the missing target.
func (c *Reconciler) configureTraffic(ctx context.Context, r *v1.Route) (*traffic.Config, error) {
	logger := logging.FromContext(ctx)
	t, trafficErr := traffic.BuildTrafficConfiguration(c.configurationLister, c.revisionLister, r)
	if t == nil {
		return nil, trafficErr
	}
	// Augment traffic configuration with visibility information.  Do not overwrite trafficErr,
	// since we will use it later.
	visibility, err := visibility.NewResolver(c.serviceLister).GetVisibility(ctx, r)
	if err != nil {
		return nil, err
	}
	t.Visibility = visibility
	// Update the Route URL.
	if err := c.updateRouteStatusURL(ctx, r, t.Visibility); err != nil {
		return nil, err
	}
	// Tell our trackers to reconcile Route whenever the things referred to by our
	// traffic stanza change. We also track missing targets since there may be
	// race conditions were routes are reconciled before their targets appear
	// in the informer cache
	for _, obj := range t.MissingTargets {
		if err := c.tracker.TrackReference(tracker.Reference{
			APIVersion: obj.APIVersion,
			Kind:       obj.Kind,
			Namespace:  obj.Namespace,
			Name:       obj.Name,
		}, r); err != nil {
			return nil, err
		}
	}
	for _, configuration := range t.Configurations {
		if err := c.tracker.TrackReference(objectRef(configuration), r); err != nil {
			return nil, err
		}
	}
	for _, revision := range t.Revisions {
		if revision.Status.IsActivationRequired() {
			logger.Infof("Revision %s/%s is inactive", revision.Namespace, revision.Name)
		}
		if err := c.tracker.TrackReference(objectRef(revision), r); err != nil {
			return nil, err
		}
	}

	var badTarget traffic.TargetError
	isTargetError := errors.As(trafficErr, &badTarget)
	if trafficErr != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		return nil, trafficErr
	}
	if badTarget != nil && isTargetError {
		logger.Info("Marking bad traffic target: ", badTarget)
		badTarget.MarkBadTrafficTarget(&r.Status)

		// Traffic targets aren't ready, no need to configure Route.
		return nil, nil
	}

	logger.Info("All referred targets are routable, marking AllTrafficAssigned with traffic information.")

	// Pass empty rollout here. We'll recompute this if there is a rollout in progress.
	r.Status.Traffic, err = t.GetRevisionTrafficTargets(ctx, r, &traffic.Rollout{})
	if err != nil {
		return nil, err
	}

	r.Status.MarkTrafficAssigned()

	return t, nil
}

func (c *Reconciler) updateRouteStatusURL(ctx context.Context, route *v1.Route, visibility map[string]netv1alpha1.IngressVisibility) error {
	isClusterLocal := visibility[traffic.DefaultTarget] == netv1alpha1.IngressVisibilityClusterLocal

	mainRouteMeta := route.ObjectMeta.DeepCopy()
	labels.SetVisibility(mainRouteMeta, isClusterLocal)

	host, err := domains.DomainNameFromTemplate(ctx, *mainRouteMeta, route.Name)
	if err != nil {
		return err
	}

	scheme := "http"
	if !isClusterLocal {
		scheme = config.FromContext(ctx).Network.DefaultExternalScheme
	}
	route.Status.URL = &apis.URL{
		Scheme: scheme,
		Host:   host,
	}

	return nil
}

// GetNetworkingClient returns the client to access networking resources.
func (c *Reconciler) GetNetworkingClient() netclientset.Interface {
	return c.netclient
}

// GetCertificateLister returns the lister for Knative Certificate.
func (c *Reconciler) GetCertificateLister() networkinglisters.CertificateLister {
	return c.certificateLister
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////

type accessor interface {
	GetGroupVersionKind() schema.GroupVersionKind
	GetNamespace() string
	GetName() string
}

func objectRef(a accessor) tracker.Reference {
	gvk := a.GetGroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return tracker.Reference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  a.GetNamespace(),
		Name:       a.GetName(),
	}
}

func getTrafficNames(targets map[string]traffic.RevisionTargets) []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	return names
}

// Sets the traffic URL scheme to scheme if the URL matches the dnsNames.
// dnsNames are DNS names under a certificate for a particular domain, and so only change
// the corresponding traffic under the route, rather than all traffic
func setTargetsScheme(rs *v1.RouteStatus, dnsNames []string, scheme string) {
	for i := range rs.Traffic {
		if rs.Traffic[i].URL == nil {
			continue
		}
		for _, dnsName := range dnsNames {
			if rs.Traffic[i].URL.Host == dnsName {
				rs.Traffic[i].URL.Scheme = scheme
				break
			}
		}
	}
}

func externalDomainTLSEnabled(ctx context.Context, r *v1.Route) bool {
	if !config.FromContext(ctx).Network.ExternalDomainTLS {
		return false
	}

	logger := logging.FromContext(ctx)
	annotationValue := networking.GetDisableExternalDomainTLS(r.Annotations)

	disabledByAnnotation, err := strconv.ParseBool(annotationValue)
	if annotationValue != "" && err != nil {
		// validation should've caught an invalid value here.
		// if we have one anyway, assume not disabled and log a warning.
		logger.Warnf("Invalid annotation value for %q. Value: %q",
			networking.DisableExternalDomainTLSAnnotationKey, annotationValue)
	}

	return !disabledByAnnotation
}

func findMatchingWildcardCert(ctx context.Context, domains []string, certs []*netv1alpha1.Certificate) *netv1alpha1.Certificate {
	for _, cert := range certs {
		if wildcardCertMatches(ctx, domains, cert) {
			return cert
		}
	}
	return nil
}

func wildcardCertMatches(ctx context.Context, domains []string, cert *netv1alpha1.Certificate) bool {
	dnsNames := make(sets.Set[string], len(cert.Spec.DNSNames))
	logger := logging.FromContext(ctx)

	for _, dns := range cert.Spec.DNSNames {
		dnsParts := strings.SplitAfterN(dns, ".", 2)
		if len(dnsParts) < 2 {
			logger.Infof("got non-FQDN DNSName %s in certificate %s", dns, cert.Name)
			continue
		}
		dnsNames.Insert(dnsParts[1])
	}
	for _, domain := range domains {
		domainParts := strings.SplitAfterN(domain, ".", 2)
		if len(domainParts) < 2 || !dnsNames.Has(domainParts[1]) {
			return false
		}
	}

	return true
}
