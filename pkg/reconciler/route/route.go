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
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	kubelabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"

	network "knative.dev/networking/pkg"
	"knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclientset "knative.dev/networking/pkg/client/clientset/versioned"
	networkinglisters "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/system"
	"knative.dev/pkg/tracker"
	cfgmap "knative.dev/serving/pkg/apis/config"
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
	ingressLister       networkinglisters.IngressLister
	certificateLister   networkinglisters.CertificateLister
	tracker             tracker.Interface

	clock system.Clock
}

// Check that our Reconciler implements routereconciler.Interface
var _ routereconciler.Interface = (*Reconciler)(nil)

func ingressClassForRoute(ctx context.Context, r *v1.Route) string {
	if ingressClass := r.Annotations[networking.IngressClassAnnotationKey]; ingressClass != "" {
		return ingressClass
	}
	return config.FromContext(ctx).Network.DefaultIngressClass
}

func certClass(ctx context.Context, r *v1.Route) string {
	if class := r.Annotations[networking.CertificateClassAnnotationKey]; class != "" {
		return class
	}
	return config.FromContext(ctx).Network.DefaultCertificateClass
}

func (c *Reconciler) getServices(route *v1.Route) ([]*corev1.Service, error) {
	currentServices, err := c.serviceLister.Services(route.Namespace).List(resources.SelectorFromRoute(route))
	if err != nil {
		return nil, err
	}

	serviceCopy := make([]*corev1.Service, len(currentServices))
	for i, svc := range currentServices {
		serviceCopy[i] = svc.DeepCopy()
	}

	return serviceCopy, err
}

func (c *Reconciler) ReconcileKind(ctx context.Context, r *v1.Route) pkgreconciler.Event {
	logger := logging.FromContext(ctx)
	logger.Debugf("Reconciling route: %#v", r.Spec)

	// Configure traffic based on the RouteSpec.
	traffic, err := c.configureTraffic(ctx, r)
	if traffic == nil || err != nil {
		// Traffic targets aren't ready, no need to configure child resources.
		return err
	}

	if cfgmap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC != cfgmap.Enabled {
		// In all cases we will add annotations to the referred targets.  This is so that when they become
		// routable we can know (through a listener) and attempt traffic configuration again.
		if err := c.reconcileTargetRevisions(ctx, traffic, r); err != nil {
			return err
		}
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

	tls, acmeChallenges, err := c.tls(ctx, r.Status.URL.Host, r, traffic)
	if err != nil {
		return err
	}

	// Reconcile ingress and its children resources.
	ingress, err := c.reconcileIngressResources(ctx, r, traffic, tls, ingressClassForRoute(ctx, r), acmeChallenges...)

	if err != nil {
		return err
	}

	if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration {
		r.Status.MarkIngressNotConfigured()
	} else {
		r.Status.PropagateIngressStatus(ingress.Status)
	}

	logger.Info("Updating placeholder k8s services with ingress information")
	if err := c.updatePlaceholderServices(ctx, r, services, ingress); err != nil {
		return err
	}

	logger.Info("Route successfully synced")
	return nil
}

func (c *Reconciler) reconcileIngressResources(ctx context.Context, r *v1.Route, tc *traffic.Config, tls []netv1alpha1.IngressTLS,
	ingressClass string, acmeChallenges ...netv1alpha1.HTTP01Challenge) (*netv1alpha1.Ingress, error) {

	desired, err := resources.MakeIngress(ctx, r, tc, tls, ingressClass, acmeChallenges...)
	if err != nil {
		return nil, err
	}

	ingress, err := c.reconcileIngress(ctx, r, desired)
	if err != nil {
		return nil, err
	}

	return ingress, nil
}

func (c *Reconciler) tls(ctx context.Context, host string, r *v1.Route, traffic *traffic.Config) ([]netv1alpha1.IngressTLS, []netv1alpha1.HTTP01Challenge, error) {
	tls := []netv1alpha1.IngressTLS{}
	if !autoTLSEnabled(ctx, r) {
		r.Status.MarkTLSNotEnabled(v1.AutoTLSNotEnabledMessage)
		return tls, nil, nil
	}

	domainToTagMap, err := domains.GetAllDomainsAndTags(ctx, r, getTrafficNames(traffic.Targets), traffic.Visibility)
	if err != nil {
		return nil, nil, err
	}

	for domain := range domainToTagMap {
		if domains.IsClusterLocal(domain) {
			r.Status.MarkTLSNotEnabled(v1.TLSNotEnabledForClusterLocalMessage)
			delete(domainToTagMap, domain)
		}
	}

	routeDomain := config.FromContext(ctx).Domain.LookupDomainForLabels(r.Labels)
	labelSelector := kubelabels.SelectorFromSet(kubelabels.Set{
		networking.WildcardCertDomainLabelKey: routeDomain,
	})

	allWildcardCerts, err := c.certificateLister.Certificates(r.Namespace).List(labelSelector)
	if err != nil {
		return nil, nil, err
	}

	acmeChallenges := []netv1alpha1.HTTP01Challenge{}
	desiredCerts := resources.MakeCertificates(r, domainToTagMap, certClass(ctx, r))
	for _, desiredCert := range desiredCerts {
		dnsNames := sets.NewString(desiredCert.Spec.DNSNames...)
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
					r.Status.MarkCertificateProvisionFailed(desiredCert.Name)
				}
				return nil, nil, err
			}
			dnsNames = sets.NewString(cert.Spec.DNSNames...)
		}

		// r.Status.URL is for the major domain, so only change if the cert is for
		// the major domain
		if dnsNames.Has(host) {
			r.Status.URL.Scheme = "https"
		}
		// TODO: we should only mark https for the public visible targets when
		// we are able to configure visibility per target.
		setTargetsScheme(&r.Status, dnsNames.List(), "https")
		if cert.IsReady() {
			r.Status.MarkCertificateReady(cert.Name)
			tls = append(tls, resources.MakeIngressTLS(cert, dnsNames.List()))
		} else {
			acmeChallenges = append(acmeChallenges, cert.Status.HTTP01Challenges...)
			r.Status.MarkCertificateNotReady(cert.Name)
			// When httpProtocol is enabled, downgrade http scheme.
			if config.FromContext(ctx).Network.HTTPProtocol == network.HTTPEnabled {
				if dnsNames.Has(host) {
					r.Status.URL = &apis.URL{
						Scheme: "http",
						Host:   host,
					}
				}
				setTargetsScheme(&r.Status, dnsNames.List(), "http")
				r.Status.MarkHTTPDowngrade(cert.Name)
			}
		}
	}
	sort.Slice(acmeChallenges, func(i, j int) bool {
		return acmeChallenges[i].URL.String() < acmeChallenges[j].URL.String()
	})
	return tls, acmeChallenges, nil
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

	badTarget, isTargetError := trafficErr.(traffic.TargetError)
	if trafficErr != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		r.Status.MarkUnknownTrafficError(trafficErr.Error())
		return nil, trafficErr
	}
	if badTarget != nil && isTargetError {
		logger.Info("Marking bad traffic target: ", badTarget)
		badTarget.MarkBadTrafficTarget(&r.Status)

		// Traffic targets aren't ready, no need to configure Route.
		return nil, nil
	}

	logger.Info("All referred targets are routable, marking AllTrafficAssigned with traffic information.")

	// Domain should already be present
	r.Status.Traffic, err = t.GetRevisionTrafficTargets(ctx, r)
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

	route.Status.URL = &apis.URL{
		Scheme: "http",
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
	names := []string{}
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

func autoTLSEnabled(ctx context.Context, r *v1.Route) bool {
	if !config.FromContext(ctx).Network.AutoTLS {
		return false
	}

	logger := logging.FromContext(ctx)
	annotationValue := r.Annotations[networking.DisableAutoTLSAnnotationKey]

	disabledByAnnotation, err := strconv.ParseBool(annotationValue)
	if err != nil {
		// validation should've caught an invalid value here.
		// if we have one anyways, assume not disabled and log a warning.
		logger.Warnf("Invalid annotation value for %q. Value: %q",
			networking.DisableAutoTLSAnnotationKey, annotationValue)
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
	dnsNames := make(sets.String, len(cert.Spec.DNSNames))
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
