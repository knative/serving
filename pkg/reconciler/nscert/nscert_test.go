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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	fakeinformerfactory "knative.dev/pkg/client/injection/kube/informers/factory"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	. "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	fakeservingclient "knative.dev/serving/pkg/client/injection/client/fake"
	fakecertinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
	"knative.dev/serving/pkg/network"
	pkgreconciler "knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/nscert/config"
	"knative.dev/serving/pkg/reconciler/nscert/resources/names"
	routecfg "knative.dev/serving/pkg/reconciler/route/config"
	. "knative.dev/serving/pkg/reconciler/testing/v1alpha1"

	_ "knative.dev/pkg/client/injection/kube/informers/core/v1/namespace/fake"
	_ "knative.dev/pkg/system/testing"
	_ "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/certificate/fake"
)

var (
	wildcardDNSNames      = []string{"*.foo.example.com"}
	defaultCertName       = names.WildcardCertificate(wildcardDNSNames[0])
	defaultDomainTemplate = "{{.Name}}.{{.Namespace}}.{{.Domain}}"
	defaultDomain         = "example.com"
)

func newTestSetup(t *testing.T, configs ...*corev1.ConfigMap) (
	ctx context.Context,
	cancel context.CancelFunc,
	rclr *reconciler,
	configMapWatcher *configmap.ManualWatcher) {

	ctx, cancel, _ = SetupFakeContextWithCancel(t)
	configMapWatcher = &configmap.ManualWatcher{Namespace: system.Namespace()}

	controller := NewController(ctx, configMapWatcher)
	rclr = controller.Reconciler.(*reconciler)

	cms := []*corev1.ConfigMap{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config-network",
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domainTemplate": defaultDomainTemplate,
			"autoTLS":        "true",
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:      routecfg.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"example.com": "",
		},
	}}
	cms = append(cms, configs...)

	for _, cfg := range cms {
		configMapWatcher.OnChange(cfg)
	}
	return
}

func TestNewController(t *testing.T) {
	defer ClearAll()
	ctx, _ := SetupFakeContext(t)

	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
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
				"example.com": "",
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
		Name:                    "certificate not created for excluded namespace",
		Key:                     "foo",
		SkipNamespaceValidation: true,
		Objects: []runtime.Object{
			kubeExcludedNamespace("foo"),
		},
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
			Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for create certificates"),
		},
	}}

	defer ClearAll()

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		return &reconciler{
			Base:                pkgreconciler.NewBase(ctx, controllerAgentName, cmw),
			knCertificateLister: listers.GetKnCertificateLister(),
			nsLister:            listers.GetNamespaceLister(),
			configStore: &testConfigStore{
				config: &config.Config{
					Network: networkConfig(),
					Domain:  domainConfig(),
				},
			},
		}
	}))
}

func TestUpdateDomainTemplate(t *testing.T) {
	ctx, cancel, reconciler, watcher := newTestSetup(t)
	defer func() {
		cancel()
		ClearAll()
	}()

	sorter := cmpopts.SortSlices(func(a, b string) bool {
		return a < b
	})
	// Create a namespace and wildcard cert using the default domain template
	ns := kubeNamespace("testns")
	nsInformer := fakeinformerfactory.Get(ctx).Core().V1().Namespaces()
	knCertificateInformer := fakecertinformer.Get(ctx)

	expected := []string{fmt.Sprintf("*.%s.%s", ns.Name, routecfg.DefaultDomain)}

	nsInformer.Informer().GetIndexer().Add(ns)
	reconciler.Reconcile(context.Background(), ns.Name)
	selector := fmt.Sprintf("%s=%s", networking.WildcardCertDomainLabelKey, routecfg.DefaultDomain)
	certs, _ := fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	cert := certs.Items[0]
	knCertificateInformer.Informer().GetIndexer().Add(&cert)

	actual := cert.Spec.DNSNames

	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("DNSNames (-want, +got) = %s", diff)
	}

	// Update the domain template to something matched by the existing DNSName
	netCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domainTemplate": "{{.Name}}-suffix.{{.Namespace}}.{{.Domain}}",
		},
	}
	watcher.OnChange(netCfg)
	reconciler.Reconcile(context.Background(), ns.Name)
	certs, _ = fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	actual = certs.Items[0].Spec.DNSNames

	// Since no new names should be added our expected value hasn't changed
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Errorf("DNSNames (-want, +got) = %s", diff)
	}

	// Update the domain template to something not matched by the existing DNSName
	netCfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domainTemplate": "{{.Name}}.subdomain.{{.Namespace}}.{{.Domain}}",
		},
	}
	watcher.OnChange(netCfg)
	reconciler.Reconcile(context.Background(), ns.Name)

	expected = []string{fmt.Sprintf("*.subdomain.%s.%s", ns.Name, routecfg.DefaultDomain)}
	certs, _ = fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	actual = certs.Items[0].Spec.DNSNames

	certs, _ = fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).List(metav1.ListOptions{
		LabelSelector: selector,
	})

	// A new domain format not matched by the existing certificate should update the DNSName
	if diff := cmp.Diff(expected, actual, sorter); diff != "" {
		t.Errorf("DNSNames (-want, +got) = %s", diff)
	}
	// Invalid domain template for wildcard certs
	netCfg = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.ConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"domainTemplate": "{{.Namespace}}.{{.Name}}.{{.Domain}}",
		},
	}
	watcher.OnChange(netCfg)
	reconciler.Reconcile(context.Background(), ns.Name)

	certs, _ = fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).List(metav1.ListOptions{
		LabelSelector: selector,
	})
	actual = certs.Items[0].Spec.DNSNames

	// With an invalid domain template nothing should have changed
	if diff := cmp.Diff(expected, actual, sorter); diff != "" {
		t.Errorf("DNSNames (-want, +got) = %s", diff)
	}
}

func TestDomainConfigDefaultDomain(t *testing.T) {
	defer ClearAll()
	domCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routecfg.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"other.com": "selector:\n app: dev",
		},
	}
	ctx, cancel, reconciler, _ := newTestSetup(t, domCfg)
	defer func() {
		cancel()
		ClearAll()
	}()

	ns := kubeNamespace("testns")
	nsInformer := fakeinformerfactory.Get(ctx).Core().V1().Namespaces()
	knCertificateInformer := fakecertinformer.Get(ctx)

	nsInformer.Informer().GetIndexer().Add(ns)
	reconciler.Reconcile(context.Background(), ns.Name)
	certName := "testns.example.com"
	expectedDomain := "*.testns.example.com"

	cert, _ := fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates(ns.Name).Get(certName, metav1.GetOptions{})
	knCertificateInformer.Informer().GetIndexer().Add(cert)

	actualDomain := cert.Spec.DNSNames[0]

	if actualDomain != expectedDomain {
		t.Errorf("Expected certificate to be issued for %s but got it was issued for %s", expectedDomain, actualDomain)
	}
}

func TestDomainConfigExplicitDefaultDomain(t *testing.T) {
	defer ClearAll()

	domCfg := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routecfg.DomainConfigName,
			Namespace: system.Namespace(),
		},
		Data: map[string]string{
			"default.com": "",
		},
	}
	ctx, cancel, reconciler, _ := newTestSetup(t, domCfg)
	defer func() {
		cancel()
		ClearAll()
	}()

	namespace := kubeNamespace("testns")
	nsInformer := fakeinformerfactory.Get(ctx).Core().V1().Namespaces()
	nsInformer.Informer().GetIndexer().Add(namespace)
	reconciler.Reconcile(context.Background(), "testns")
	certs, _ := fakeservingclient.Get(ctx).NetworkingV1alpha1().Certificates("testns").List(metav1.ListOptions{})

	expectedDomain := "*.testns.default.com"
	actualDomain := certs.Items[0].Spec.DNSNames[0]

	if actualDomain != expectedDomain {
		t.Errorf("Expected certificate to be issued for %s but got it was issued for %s", expectedDomain, actualDomain)
	}
}

type testConfigStore struct {
	config *config.Config
}

func (t *testConfigStore) ToContext(ctx context.Context) context.Context {
	return config.ToContext(ctx, t.config)
}

func (t *testConfigStore) WatchConfigs(w configmap.Watcher) {}

var _ configStore = (*testConfigStore)(nil)

func knCert(namespace *corev1.Namespace) *v1alpha1.Certificate {
	return knCertWithStatus(namespace, &v1alpha1.CertificateStatus{})
}

func knCertWithStatus(namespace *corev1.Namespace, status *v1alpha1.CertificateStatus) *v1alpha1.Certificate {
	return &v1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            defaultCertName,
			Namespace:       namespace.Name,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(namespace, corev1.SchemeGroupVersion.WithKind("Namespace"))},
			Labels: map[string]string{
				networking.WildcardCertDomainLabelKey: defaultDomain,
			},
		},
		Spec: v1alpha1.CertificateSpec{
			DNSNames:   wildcardDNSNames,
			SecretName: namespace.Name,
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

func kubeExcludedNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				networking.DisableWildcardCertLabelKey: "true",
			},
		},
	}
}

func networkConfig() *network.Config {
	return &network.Config{
		DomainTemplate: defaultDomainTemplate,
	}
}

func domainConfig() *routecfg.Domain {
	domainConfig := &routecfg.Domain{
		Domains: map[string]*routecfg.LabelSelector{
			"example.com": {},
		},
	}
	return domainConfig
}
