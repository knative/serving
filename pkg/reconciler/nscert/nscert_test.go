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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	// Inject the fakes for informers this reconciler depends on.
	netapi "knative.dev/networking/pkg/apis/networking"
	netv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	netclient "knative.dev/networking/pkg/client/injection/client"
	netcfg "knative.dev/networking/pkg/config"
	namespacereconciler "knative.dev/pkg/client/injection/kube/reconciler/core/v1/namespace"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	. "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/reconciler/nscert/config"
	"knative.dev/serving/pkg/reconciler/nscert/resources/names"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"

	fakeclient "knative.dev/networking/pkg/client/injection/client/fake"
	fakecertinformer "knative.dev/networking/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakensinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/namespace/fake"

	_ "knative.dev/pkg/metrics/testing"
	_ "knative.dev/pkg/system/testing"

	. "knative.dev/serving/pkg/reconciler/testing/v1"
)

const (
	disableWildcardCertLabelKey = "networking.knative.dev/disableWildcardCert"
	testCertClass               = "dns-01.rocks"
)

type key int

var (
	wildcardDNSNames      = []string{"*.foo.example.com"}
	defaultCertName       = names.WildcardCertificate(wildcardDNSNames[0])
	defaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"
	defaultDomain         = "example.com"
	// Used to pass configuration to tests in TestReconcile
	netConfigContextKey key
)

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	context.Context, context.CancelFunc, chan *netv1alpha1.Certificate, *configmap.ManualWatcher) {
	t.Helper()

	ctx, ccl, ifs := SetupFakeContextWithCancel(t)
	wf, err := RunAndSyncInformers(ctx, ifs...)
	if err != nil {
		t.Fatal("Error starting informers:", err)
	}
	cancel := func() {
		ccl()
		wf()
	}

	configMapWatcher := &configmap.ManualWatcher{Namespace: system.Namespace()}

	ctl := NewController(ctx, configMapWatcher)

	cms := []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domain-template":     defaultDomainTemplate,
			"external-domain-tls": "true",
			// Apply to all namespaces
			"namespace-wildcard-cert-selector": "{}",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      routecfg.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"svc.cluster.local": "",
		},
	}}
	cms = append(cms, configs...)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}
	if err := configMapWatcher.Start(ctx.Done()); err != nil {
		t.Fatal("failed to start config manager:", err)
	}

	certEvents := make(chan *netv1alpha1.Certificate)
	fakecertinformer.Get(ctx).Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterControllerGVK(corev1.SchemeGroupVersion.WithKind("Namespace")),
		Handler: controller.HandleAll(func(obj interface{}) {
			select {
			case <-ctx.Done():
				// context go cancelled, no more reads necessary.
			case certEvents <- obj.(*netv1alpha1.Certificate):
				// written successfully.
			}
		}),
	})

	var eg errgroup.Group
	eg.Go(func() error { return ctl.RunContext(ctx, 1) })

	return ctx, func() {
		cancel()
		eg.Wait()
	}, certEvents, configMapWatcher
}

func TestNewController(t *testing.T) {
	ctx, _ := SetupFakeContext(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"DomainTemplate": defaultDomainTemplate,
		}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      routecfg.DomainConfigName,
				Namespace: system.Namespace(),
			},
			Data: map[string]string{
				"svc.cluster.local": "",
			}},
	)

	c := NewController(ctx, configMapWatcher)

	if c == nil {
		t.Fatal("Expected NewController to return a non-nil value")
	}
}

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		Key:  "too/many/parts",
	}, {
		Name: "key not found",
		Key:  "foo/not-found",
	}, {
		Name:                    "create Knative certificate for namespace",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			kubeNamespace("foo"),
		},
		WantCreates: []runtime.Object{
			knCert(kubeNamespace("foo")),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Knative Certificate %s/%s", "foo", defaultCertName),
		},
		Key: "foo",
	}, {
		Name:                    "create Knative certificate for namespace with explicitly enabled",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			kubeNamespaceWithLabelValue("foo", map[string]string{disableWildcardCertLabelKey: "false"}),
		},
		WantCreates: []runtime.Object{
			knCert(kubeNamespace("foo")),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Knative Certificate %s/%s", "foo", defaultCertName),
		},
		Key: "foo",
	}, {
		Name:                    "create Knative certificate for namespace with explicitly enabled for internal label",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			kubeNamespaceWithLabelValue("foo", map[string]string{disableWildcardCertLabelKey: "false"}),
		},
		WantCreates: []runtime.Object{
			knCert(kubeNamespace("foo")),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Knative Certificate %s/%s", "foo", defaultCertName),
		},
		Key: "foo",
	}, {
		Name: "skip certificate when excluded by config-network",
		Key:  "excluded",
		Objects: []runtime.Object{
			kubeNamespaceWithLabelValue("excluded", map[string]string{"excludeWildcard": "anything"}),
		},
		Ctx: context.WithValue(context.Background(), netConfigContextKey,
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      "excludeWildcard",
					Operator: "NotIn",
					Values:   []string{"yes", "true", "anything"},
				}}}),
	}, {
		Name: "certificate not created for excluded namespace when both internal and external labels are present",
		Key:  "foo",
		Objects: []runtime.Object{
			kubeNamespaceWithLabelValue("foo", map[string]string{
				disableWildcardCertLabelKey: "true",
			}),
		},
		Ctx: context.WithValue(context.Background(), netConfigContextKey,
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      disableWildcardCertLabelKey,
					Operator: "NotIn",
					Values:   []string{"true"},
				}}}),
	}, {
		Name:                    "certificate creation failed",
		Key:                     "foo",
		WantErr:                 true,
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			kubeNamespace("foo"),
		},
		WantCreates: []runtime.Object{
			knCert(kubeNamespace("foo")),
		},
		WithReactors: []clientgotesting.ReactionFunc{
			InduceFailure("create", "certificates"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeWarning, "CreationFailed", "Failed to create Knative certificate %s/%s: inducing failure for create certificates", "foo", defaultCertName),
			Eventf(corev1.EventTypeWarning, "InternalError", "failed to create namespace certificate: inducing failure for create certificates"),
		},
	}, {
		Name: "disabling namespace cert feature deletes the cert",
		Key:  "foo",
		Objects: []runtime.Object{
			kubeNamespaceWithLabelValue("foo", map[string]string{disableWildcardCertLabelKey: "true"}),
			knCert(kubeNamespace("foo")),
		},
		SkipNamespaceValidation: true,
		WantDeletes: []clientgotesting.DeleteActionImpl{{
			ActionImpl: clientgotesting.ActionImpl{
				Namespace: "foo",
				Verb:      "delete",
				Resource:  netv1alpha1.SchemeGroupVersion.WithResource("certificates"),
			},
			Name: "foo.example.com",
		}},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Deleted", "Deleted Knative Certificate %s/%s", "foo", defaultCertName),
		},
		Ctx: context.WithValue(context.Background(), netConfigContextKey,
			&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{
					Key:      disableWildcardCertLabelKey,
					Operator: "NotIn",
					Values:   []string{"true"},
				}}}),
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &reconciler{
			client:              netclient.Get(ctx),
			knCertificateLister: listers.GetKnCertificateLister(),
		}

		netCfg := networkConfig()
		if ls, ok := ctx.Value(netConfigContextKey).(*metav1.LabelSelector); ok && ls != nil {
			netCfg.NamespaceWildcardCertSelector = ls
		}

		return namespacereconciler.NewReconciler(ctx, logging.FromContext(ctx), fakekubeclient.Get(ctx),
			listers.GetNamespaceLister(), controller.GetEventRecorder(ctx), r,
			controller.Options{ConfigStore: &testConfigStore{
				config: &config.Config{
					Network: netCfg,
					Domain:  domainConfig(),
				},
			}})
	}))
}

func TestUpdateDomainTemplate(t *testing.T) {
	netCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"namespace-wildcard-cert-selector": "{}",
			"external-domain-tls":              "Enabled",
		},
	}
	ctx, cancel, certEvents, watcher := newTestSetup(t, netCfg)
	defer cancel()

	namespace := kubeNamespace("testns")
	fakekubeclient.Get(ctx).CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	fakensinformer.Get(ctx).Informer().GetIndexer().Add(namespace)

	want := []string{fmt.Sprintf("*.%s.%s", namespace.Name, routecfg.DefaultDomain)}
	cert := <-certEvents
	if diff := cmp.Diff(want, cert.Spec.DNSNames); diff != "" {
		t.Error("DNSNames (-want, +got) =", diff)
	}

	// Update the domain template to something matched by the existing DNSName
	netCfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domain-template":                  "{{.Name}}-suffix.{{.Namespace}}.{{.Domain}}",
			"namespace-wildcard-cert-selector": "{}",
			"external-domain-tls":              "Enabled",
		},
	}
	watcher.OnChange(netCfg)

	// Since no new names should be added nothing should change
	select {
	case <-certEvents:
		t.Error("Unexpected event")
	case <-time.After(100 * time.Millisecond):
	}

	// Update the domain template to something not matched by the existing DNSName
	netCfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domain-template":                  "{{.Name}}.subdomain.{{.Namespace}}.{{.Domain}}",
			"namespace-wildcard-cert-selector": `{}`,
			"external-domain-tls":              "Enabled",
		},
	}
	watcher.OnChange(netCfg)

	// A new domain format not matched by the existing certificate should update the DNSName
	want = []string{fmt.Sprintf("*.subdomain.%s.%s", namespace.Name, routecfg.DefaultDomain)}
	cert = <-certEvents
	if diff := cmp.Diff(want, cert.Spec.DNSNames); diff != "" {
		t.Error("DNSNames (-want, +got) =", diff)
	}

	// Invalid domain template for wildcard certs
	oldDomain := want
	netCfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domain-template":     "{{.Namespace}}.{{.Name}}.{{.Domain}}",
			"external-domain-tls": "Enabled",
		},
	}
	watcher.OnChange(netCfg)
	// With an invalid domain template nothing change
	done := time.After(100 * time.Millisecond)
	for {
		select {
		case cert := <-certEvents:
			// We don't expect the domain of cert to be changed.
			if diff := cmp.Diff(oldDomain, cert.Spec.DNSNames); diff != "" {
				t.Fatal("DNSNames should not be changed: (-want, +got) =", diff)
			}
		case <-done:
			return
		}
	}
}

func TestChangeDefaultDomain(t *testing.T) {
	netCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      netcfg.ConfigMapName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"external-domain-tls":              "Enabled",
			"namespace-wildcard-cert-selector": "{}",
		},
	}

	ctx, cancel, certEvents, watcher := newTestSetup(t, netCfg)
	defer cancel()

	namespace := kubeNamespace("testns")
	fakekubeclient.Get(ctx).CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{})
	fakensinformer.Get(ctx).Informer().GetIndexer().Add(namespace)

	// The certificate should be created with the default domain.
	cert := <-certEvents
	if got, want := cert.Spec.DNSNames[0], "*.testns.svc.cluster.local"; got != want {
		t.Errorf("DNSName[0] = %s, want %s", got, want)
	}

	// Change the domain settings.
	domCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routecfg.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"example.net": "",
		},
	}
	watcher.OnChange(domCfg)

	// The certificate should be updated with the new domain.
	cert2 := <-certEvents
	if got, want := cert2.Spec.DNSNames[0], "*.testns.example.net"; got != want {
		t.Errorf("DNSName[0] = %s, want %s", got, want)
	}

	// Assert we have exactly one certificate.
	certs, _ := fakeclient.Get(ctx).NetworkingV1alpha1().Certificates(namespace.Name).List(ctx, metav1.ListOptions{})
	if len(certs.Items) > 1 {
		t.Errorf("Expected 1 certificate, got %d.", len(certs.Items))
	}
}

func TestDomainConfigDomain(t *testing.T) {
	const ns = "testns"

	tests := []struct {
		name         string
		domainCfg    map[string]string
		netCfg       map[string]string
		wantCertName string
		wantDNSName  string
	}{{
		name:      "no domainmapping without config",
		domainCfg: map[string]string{},
		netCfg: map[string]string{
			"external-domain-tls": "Enabled",
		},
	}, {
		name: "default domain",
		domainCfg: map[string]string{
			"other.com": "selector:\n app: dev",
		},
		netCfg: map[string]string{
			"external-domain-tls":              "Enabled",
			"namespace-wildcard-cert-selector": "{}",
		},
		wantCertName: "testns.svc.cluster.local",
		wantDNSName:  "*.testns.svc.cluster.local",
	}, {
		name: "default domain",
		domainCfg: map[string]string{
			"default.com": "",
		},
		netCfg: map[string]string{
			"external-domain-tls":              "Enabled",
			"namespace-wildcard-cert-selector": "{}",
		},
		wantCertName: "testns.default.com",
		wantDNSName:  "*.testns.default.com",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			domCfg := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      routecfg.DomainConfigName,
					Namespace: system.Namespace(),
				},
				Data: test.domainCfg,
			}
			netCfg := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      netcfg.ConfigMapName,
					Namespace: system.Namespace(),
				},
				Data: test.netCfg,
			}

			ctx, ccl, ifs := SetupFakeContextWithCancel(t)
			wf, err := RunAndSyncInformers(ctx, ifs...)
			if err != nil {
				t.Fatal("Error starting informers:", err)
			}
			defer func() {
				ccl()
				wf()
			}()

			cmw := configmap.NewStaticWatcher(domCfg, netCfg)
			configStore := config.NewStore(logging.FromContext(ctx).Named("config-store"))
			configStore.WatchConfigs(cmw)

			r := &reconciler{
				client:              netclient.Get(ctx),
				knCertificateLister: fakecertinformer.Get(ctx).Lister(),
			}

			namespace := kubeNamespace(ns)

			ctx = configStore.ToContext(ctx)
			r.ReconcileKind(ctx, namespace)

			cert, err := fakeclient.Get(ctx).NetworkingV1alpha1().Certificates(ns).Get(ctx, test.wantCertName, metav1.GetOptions{})
			if test.wantCertName == "" || test.wantDNSName == "" {
				// Expect no cert created
				if err == nil {
					t.Error("Expected no cert to be created, got:", cert)
				} else if !apierrs.IsNotFound(err) {
					t.Error("Expected cert to be missing, got unexpected error:", err)
				}
			} else {
				if err != nil {
					t.Fatal("Could not get certificate:", err)
				}
				if got, want := cert.Spec.DNSNames[0], test.wantDNSName; got != want {
					t.Errorf("DNSName[0] = %s, want %s", got, want)
				}
			}
		})
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

var _ pkgreconciler.ConfigStore = (*testConfigStore)(nil)

func knCert(namespace *corev1.Namespace) *netv1alpha1.Certificate {
	return knCertWithStatus(namespace, &netv1alpha1.CertificateStatus{})
}

func knCertWithStatus(namespace *corev1.Namespace, status *netv1alpha1.CertificateStatus) *netv1alpha1.Certificate {
	return &netv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            defaultCertName,
			Namespace:       namespace.Name,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(namespace, corev1.SchemeGroupVersion.WithKind("Namespace"))},
			Annotations: map[string]string{
				netapi.CertificateClassAnnotationKey: testCertClass,
			},
			Labels: map[string]string{
				netapi.WildcardCertDomainLabelKey: defaultDomain,
			},
		},
		Spec: netv1alpha1.CertificateSpec{
			DNSNames:   wildcardDNSNames,
			SecretName: defaultCertName,
		},
		Status: *status,
	}
}

func kubeNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func kubeNamespaceWithLabelValue(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func networkConfig() *netcfg.Config {
	return &netcfg.Config{
		DomainTemplate:                defaultDomainTemplate,
		ExternalDomainTLS:             true,
		DefaultCertificateClass:       testCertClass,
		NamespaceWildcardCertSelector: &metav1.LabelSelector{},
	}
}

func domainConfig() *routecfg.Domain {
	domainConfig := &routecfg.Domain{
		Domains: map[string]routecfg.DomainConfig{
			"example.com": {Type: routecfg.DomainTypeWildcard},
		},
	}
	return domainConfig
}
