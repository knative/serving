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
	"strings"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubelabels "k8s.io/apimachinery/pkg/labels"
	"knative.dev/pkg/apis"
	duckv1alpha1 "knative.dev/pkg/apis/duck/v1alpha1"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/pkg/tracker"
	"knative.dev/serving/pkg/apis/networking"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
	clientset "knative.dev/serving/pkg/client/clientset/versioned"
	networkinglisters "knative.dev/serving/pkg/client/listers/networking/v1alpha1"
	listers "knative.dev/serving/pkg/client/listers/serving/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
	networkaccessor "knative.dev/serving/pkg/reconciler/accessor/networking"
	"knative.dev/serving/pkg/reconciler/route/config"
	"knative.dev/serving/pkg/reconciler/route/domains"
	"knative.dev/serving/pkg/reconciler/route/resources"
	"knative.dev/serving/pkg/reconciler/route/resources/labels"
	resourcenames "knative.dev/serving/pkg/reconciler/route/resources/names"
	"knative.dev/serving/pkg/reconciler/route/traffic"
)

// routeFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - routes.serving.knative.dev
var (
	routeResource  = v1alpha1.Resource("routes")
	routeFinalizer = routeResource.String()
)

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	*reconciler.Base

	// Listers index properties about resources
	routeLister         listers.RouteLister
	configurationLister listers.ConfigurationLister
	revisionLister      listers.RevisionLister
	serviceLister       corev1listers.ServiceLister
	ingressLister       networkinglisters.IngressLister
	certificateLister   networkinglisters.CertificateLister
	configStore         reconciler.ConfigStore
	tracker             tracker.Interface

	clock system.Clock
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Route resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	ctx = c.configStore.ToContext(ctx)
	ctx = controller.WithEventRecorder(ctx, c.Recorder)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorw("Invalid resource key", zap.Error(err))
		return nil
	}

	// Get the Route resource with this namespace/name.
	original, err := c.routeLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Info("Route in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy.
	route := original.DeepCopy()

	// Reconcile this copy of the route and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := c.reconcile(ctx, route)
	if equality.Semantic.DeepEqual(original.Status, route.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if err = c.updateStatus(original, route); err != nil {
		logger.Warnw("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Route %q: %v", route.Name, err)
		return err
	}
	if reconcileErr != nil {
		c.Recorder.Event(route, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
		return reconcileErr
	}
	// TODO(mattmoor): Remove this after 0.7 cuts.
	// If the spec has changed, then assume we need an upgrade and issue a patch to trigger
	// the webhook to upgrade via defaulting.  Status updates do not trigger this due to the
	// use of the /status resource.
	if !equality.Semantic.DeepEqual(original.Spec, route.Spec) {
		routes := v1alpha1.SchemeGroupVersion.WithResource("routes")
		if err := c.MarkNeedsUpgrade(routes, route.Namespace, route.Name); err != nil {
			return err
		}
	}
	return nil
}

func ingressClassForRoute(ctx context.Context, r *v1alpha1.Route) string {
	if ingressClass := r.Annotations[networking.IngressClassAnnotationKey]; ingressClass != "" {
		return ingressClass
	}
	return config.FromContext(ctx).Network.DefaultIngressClass
}

func certClass(ctx context.Context, r *v1alpha1.Route) string {
	if class := r.Annotations[networking.CertificateClassAnnotationKey]; class != "" {
		return class
	}
	return config.FromContext(ctx).Network.DefaultCertificateClass
}

func (c *Reconciler) getServices(route *v1alpha1.Route) ([]*corev1.Service, error) {
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

func (c *Reconciler) reconcile(ctx context.Context, r *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)
	if r.GetDeletionTimestamp() != nil {
		// Check for a DeletionTimestamp.  If present, elide the normal reconcile logic.
		return c.reconcileDeletion(ctx, r)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	r.SetDefaults(v1.WithUpgradeViaDefaulting(ctx))
	r.Status.InitializeConditions()

	if err := r.ConvertUp(ctx, &v1beta1.Route{}); err != nil {
		return err
	}

	logger.Infof("Reconciling route: %#v", r)

	serviceNames, err := c.getServiceNames(ctx, r)
	if err != nil {
		return err
	}

	if err := c.updateRouteStatusURL(ctx, r, serviceNames.clusterLocal()); err != nil {
		return err
	}

	// Configure traffic based on the RouteSpec.
	traffic, err := c.configureTraffic(ctx, r, serviceNames.desiredClusterLocalServiceNames)
	if traffic == nil || err != nil {
		// Traffic targets aren't ready, no need to configure child resources.
		// Need to update ObservedGeneration, otherwise Route's Ready state won't
		// be propagated to Service and the Service's RoutesReady will stay in
		// 'Unknown'.
		r.Status.ObservedGeneration = r.Generation
		return err
	}

	logger.Info("Updating targeted revisions.")
	// In all cases we will add annotations to the referred targets.  This is so that when they become
	// routable we can know (through a listener) and attempt traffic configuration again.
	if err := c.reconcileTargetRevisions(ctx, traffic, r); err != nil {
		return err
	}

	r.Status.Address = &duckv1alpha1.Addressable{
		Addressable: duckv1beta1.Addressable{
			URL: &apis.URL{
				Scheme: "http",
				Host:   resourcenames.K8sServiceFullname(r),
			},
		},
	}

	logger.Info("Creating placeholder k8s services")
	services, err := c.reconcilePlaceholderServices(ctx, r, traffic.Targets, serviceNames.existing())
	if err != nil {
		return err
	}

	clusterLocalServiceNames := serviceNames.clusterLocal()
	tls, acmeChallenges, err := c.tls(ctx, r.Status.URL.Host, r, traffic, clusterLocalServiceNames)
	if err != nil {
		return err
	}

	// Reconcile ingress and its children resources.
	ingress, err := c.reconcileIngressResources(ctx, r, traffic, tls, clusterLocalServiceNames, ingressClassForRoute(ctx, r), acmeChallenges...)

	if err != nil {
		return err
	}

	if ingress.GetObjectMeta().GetGeneration() != ingress.Status.ObservedGeneration || !ingress.Status.IsReady() {
		r.Status.MarkIngressNotConfigured()
	} else {
		r.Status.PropagateIngressStatus(ingress.Status)
	}

	logger.Info("Updating placeholder k8s services with ingress information")
	if err := c.updatePlaceholderServices(ctx, r, services, ingress); err != nil {
		return err
	}

	r.Status.ObservedGeneration = r.Generation
	logger.Info("Route successfully synced")
	return nil
}

func (c *Reconciler) reconcileIngressResources(ctx context.Context, r *v1alpha1.Route, tc *traffic.Config, tls []netv1alpha1.IngressTLS,
	clusterLocalServices sets.String, ingressClass string, acmeChallenges ...netv1alpha1.HTTP01Challenge) (*netv1alpha1.Ingress, error) {

	desired, err := resources.MakeIngress(ctx, r, tc, tls, clusterLocalServices, ingressClass, acmeChallenges...)
	if err != nil {
		return nil, err
	}

	ingress, err := c.reconcileIngress(ctx, r, desired)
	if err != nil {
		return nil, err
	}

	return ingress, nil
}

func (c *Reconciler) tls(ctx context.Context, host string, r *v1alpha1.Route, traffic *traffic.Config, clusterLocalServiceNames sets.String) ([]netv1alpha1.IngressTLS, []netv1alpha1.HTTP01Challenge, error) {
	tls := []netv1alpha1.IngressTLS{}
	if !config.FromContext(ctx).Network.AutoTLS {
		return tls, nil, nil
	}
	domainToTagMap, err := domains.GetAllDomainsAndTags(ctx, r, getTrafficNames(traffic.Targets), clusterLocalServiceNames)
	if err != nil {
		return nil, nil, err
	}

	for domain := range domainToTagMap {
		if domains.IsClusterLocal(domain) {
			delete(domainToTagMap, domain)
		}
	}

	routeDomain := config.FromContext(ctx).Domain.LookupDomainForLabels(r.Labels)
	labelSelector := kubelabels.SelectorFromSet(
		kubelabels.Set{
			networking.WildcardCertDomainLabelKey: routeDomain,
		},
	)

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
		if cert.Status.IsReady() {
			r.Status.MarkCertificateReady(cert.Name)
		} else {
			acmeChallenges = append(acmeChallenges, cert.Status.HTTP01Challenges...)
			r.Status.MarkCertificateNotReady(cert.Name)
			// When httpProtocol is enabled, downward http scheme.
			if config.FromContext(ctx).Network.HTTPProtocol == network.HTTPEnabled {
				if dnsNames.Has(host) {
					r.Status.URL = &apis.URL{
						Scheme: "http",
						Host:   host,
					}
				}
				setTargetsScheme(&r.Status, dnsNames.List(), "http")
			}
		}
		tls = append(tls, resources.MakeIngressTLS(cert, dnsNames.List()))
	}
	sort.Slice(acmeChallenges, func(i, j int) bool {
		return acmeChallenges[i].URL.String() < acmeChallenges[j].URL.String()
	})
	return tls, acmeChallenges, nil
}

func (c *Reconciler) reconcileDeletion(ctx context.Context, r *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)

	// If our Finalizer is first, delete the Ingress for this Route
	// and remove the finalizer.
	if len(r.Finalizers) == 0 || r.Finalizers[0] != routeFinalizer {
		return nil
	}

	// Delete the Ingress resources for this Route.
	logger.Info("Cleaning up Ingress")
	if err := c.deleteIngressForRoute(r); err != nil {
		return err
	}

	// Update the Route to remove the Finalizer.
	logger.Info("Removing Finalizer")
	r.Finalizers = r.Finalizers[1:]
	_, err := c.ServingClientSet.ServingV1alpha1().Routes(r.Namespace).Update(r)
	return err
}

// configureTraffic attempts to configure traffic based on the RouteSpec.  If there are missing
// targets (e.g. Configurations without a Ready Revision, or Revision that isn't Ready or Inactive),
// no traffic will be configured.
//
// If traffic is configured we update the RouteStatus with AllTrafficAssigned = True.  Otherwise we
// mark AllTrafficAssigned = False, with a message referring to one of the missing target.
func (c *Reconciler) configureTraffic(ctx context.Context, r *v1alpha1.Route, clusterLocalServices sets.String) (*traffic.Config, error) {
	logger := logging.FromContext(ctx)
	t, err := traffic.BuildTrafficConfiguration(c.configurationLister, c.revisionLister, r)

	if t == nil {
		return nil, err
	}

	// Tell our trackers to reconcile Route whenever the things referred to by our
	// traffic stanza change. We also track missing targets since there may be
	// race conditions were routes are reconciled before their targets appear
	// in the informer cache
	for _, obj := range t.MissingTargets {
		if err := c.tracker.Track(obj, r); err != nil {
			return nil, err
		}
	}
	for _, configuration := range t.Configurations {
		if err := c.tracker.Track(objectRef(configuration), r); err != nil {
			return nil, err
		}
	}
	for _, revision := range t.Revisions {
		if revision.Status.IsActivationRequired() {
			logger.Infof("Revision %s/%s is inactive", revision.Namespace, revision.Name)
		}
		if err := c.tracker.Track(objectRef(revision), r); err != nil {
			return nil, err
		}
	}

	badTarget, isTargetError := err.(traffic.TargetError)
	if err != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		r.Status.MarkUnknownTrafficError(err.Error())
		return nil, err
	}
	if badTarget != nil && isTargetError {
		logger.Infof("Marking bad traffic target: %v", badTarget)
		badTarget.MarkBadTrafficTarget(&r.Status)

		// Traffic targets aren't ready, no need to configure Route.
		return nil, nil
	}

	logger.Info("All referred targets are routable, marking AllTrafficAssigned with traffic information.")
	// Domain should already be present
	r.Status.Traffic, err = t.GetRevisionTrafficTargets(ctx, r, clusterLocalServices)
	if err != nil {
		return nil, err
	}

	r.Status.MarkTrafficAssigned()

	return t, nil
}

func (c *Reconciler) updateRouteStatusURL(ctx context.Context, route *v1alpha1.Route, clusterLocalServices sets.String) error {
	mainRouteServiceName, err := domains.HostnameFromTemplate(ctx, route.Name, "")
	if err != nil {
		return err
	}

	mainRouteMeta := route.ObjectMeta.DeepCopy()
	isClusterLocal := clusterLocalServices.Has(mainRouteServiceName) || labels.IsObjectLocalVisibility(route.ObjectMeta)
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

func (c *Reconciler) getServiceNames(ctx context.Context, route *v1alpha1.Route) (*serviceNames, error) {
	// Populate existing service name sets
	existingServices, err := c.getServices(route)
	if err != nil {
		return nil, err
	}
	existingServiceNames := resources.GetNames(existingServices)
	existingClusterLocalServices := resources.FilterService(existingServices, resources.IsClusterLocalService)
	existingClusterLocalServiceNames := resources.GetNames(existingClusterLocalServices)
	existingPublicServiceNames := existingServiceNames.Difference(existingClusterLocalServiceNames)

	// Populate desired service name sets
	desiredServiceNames, err := resources.GetDesiredServiceNames(ctx, route)
	if err != nil {
		return nil, err
	}
	if labels.IsObjectLocalVisibility(route.ObjectMeta) {
		return &serviceNames{
			existingPublicServiceNames:       existingPublicServiceNames,
			existingClusterLocalServiceNames: existingClusterLocalServiceNames,
			desiredPublicServiceNames:        sets.NewString(),
			desiredClusterLocalServiceNames:  desiredServiceNames,
		}, nil
	}
	desiredPublicServiceNames := desiredServiceNames.Intersection(existingPublicServiceNames)
	desiredClusterLocalServiceNames := desiredServiceNames.Intersection(existingClusterLocalServiceNames)

	// Any new desired services will follow the default route visibility, which is public.
	serviceWithDefaultVisibility := desiredServiceNames.Difference(existingServiceNames)
	desiredPublicServiceNames = desiredPublicServiceNames.Union(serviceWithDefaultVisibility)

	return &serviceNames{
		existingPublicServiceNames:       existingPublicServiceNames,
		existingClusterLocalServiceNames: existingClusterLocalServiceNames,
		desiredPublicServiceNames:        desiredPublicServiceNames,
		desiredClusterLocalServiceNames:  desiredClusterLocalServiceNames,
	}, nil
}

// GetServingClient returns the client to access Knative serving resources.
func (c *Reconciler) GetServingClient() clientset.Interface {
	return c.ServingClientSet
}

// GetCertificateLister returns the lister for Knative Certificate.
func (c *Reconciler) GetCertificateLister() networkinglisters.CertificateLister {
	return c.certificateLister
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////

type serviceNames struct {
	existingPublicServiceNames       sets.String
	existingClusterLocalServiceNames sets.String
	desiredPublicServiceNames        sets.String
	desiredClusterLocalServiceNames  sets.String
}

func (sn serviceNames) existing() sets.String {
	return sn.existingPublicServiceNames.Union(sn.existingClusterLocalServiceNames)
}

func (sn serviceNames) clusterLocal() sets.String {
	return sn.existingClusterLocalServiceNames.Union(sn.desiredClusterLocalServiceNames)
}

type accessor interface {
	GetGroupVersionKind() schema.GroupVersionKind
	GetNamespace() string
	GetName() string
}

func objectRef(a accessor) corev1.ObjectReference {
	gvk := a.GetGroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return corev1.ObjectReference{
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
func setTargetsScheme(rs *v1alpha1.RouteStatus, dnsNames []string, scheme string) {
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

func findMatchingWildcardCert(ctx context.Context, domains []string, certs []*netv1alpha1.Certificate) *netv1alpha1.Certificate {
	for _, cert := range certs {
		if wildcardCertMatches(ctx, domains, cert) {
			return cert
		}
	}
	return nil
}

func wildcardCertMatches(ctx context.Context, domains []string, cert *netv1alpha1.Certificate) bool {
	dnsNames := sets.NewString()
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
