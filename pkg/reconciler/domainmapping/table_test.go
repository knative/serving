/*
Copyright 2020 The Knative Authors

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

package domainmapping

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgotesting "k8s.io/client-go/testing"

	networkingclient "knative.dev/networking/pkg/client/injection/client/fake"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	servingclient "knative.dev/serving/pkg/client/injection/client/fake"
	domainmappingreconciler "knative.dev/serving/pkg/client/injection/reconciler/serving/v1alpha1/domainmapping"
	"knative.dev/serving/pkg/reconciler/domainmapping/resources"

	. "knative.dev/pkg/reconciler/testing"
	. "knative.dev/serving/pkg/reconciler/testing/v1"
)

func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name: "bad workqueue key",
		// Make sure Reconcile handles bad keys.
		Key: "too/many/parts",
	}, {
		Name: "key not found",
		// Make sure Reconcile handles good keys that don't exist.
		Key: "foo/not-found",
	}, {
		Name: "first reconcile",
		Key:  "default/first-reconcile.com",
		Objects: []runtime.Object{
			DomainMapping("default", "first-reconcile.com", withRef("default", "target")),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DomainMapping("default", "first-reconcile.com",
				withRef("default", "target"),
				withURL("http", "first-reconcile.com"),
				withAddress("http", "first-reconcile.com"),
				withInitDomainMappingConditions,
			),
		}},
		WantCreates: []runtime.Object{
			resources.MakeIngress(DomainMapping("default", "first-reconcile.com", withRef("default", "target")), "istio.ingress.networking.knative.dev"),
		},
		WantEvents: []string{
			Eventf(corev1.EventTypeNormal, "Created", "Created Ingress %q", "first-reconcile.com"),
		},
	}, {
		Name: "reconcile changed ref",
		Key:  "default/ingress-exists.org",
		Objects: []runtime.Object{
			DomainMapping("default", "ingress-exists.org", withRef("default", "changed")),
			resources.MakeIngress(DomainMapping("default", "ingress-exists.org", withRef("default", "target")), "istio.ingress.networking.knative.dev"),
		},
		WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
			Object: DomainMapping("default", "ingress-exists.org",
				withRef("default", "changed"),
				withURL("http", "ingress-exists.org"),
				withAddress("http", "ingress-exists.org"),
				withInitDomainMappingConditions,
			),
		}},
		WantUpdates: []clientgotesting.UpdateActionImpl{{
			Object: resources.MakeIngress(DomainMapping("default", "ingress-exists.org", withRef("default", "changed")), "istio.ingress.networking.knative.dev"),
		}},
	}}

	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			netclient:     networkingclient.Get(ctx),
			ingressLister: listers.GetIngressLister(),
		}

		return domainmappingreconciler.NewReconciler(ctx, logging.FromContext(ctx), servingclient.Get(ctx),
			listers.GetDomainMappingLister(), controller.GetEventRecorder(ctx), r,
		)
	}))
}

type domainMappingOption func(dm *v1alpha1.DomainMapping)

func DomainMapping(namespace, name string, opt ...domainMappingOption) *v1alpha1.DomainMapping {
	dm := &v1alpha1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	for _, o := range opt {
		o(dm)
	}
	return dm
}

func withRef(namespace, name string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Spec.Ref.Namespace = namespace
		dm.Spec.Ref.Name = name
	}
}

func withURL(scheme, host string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Status.URL = &apis.URL{Scheme: scheme, Host: host}
	}
}

func withAddress(scheme, host string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Status.Address = &duckv1.Addressable{URL: &apis.URL{
			Scheme: scheme,
			Host:   host,
		}}
	}
}

func withInitDomainMappingConditions(dm *v1alpha1.DomainMapping) {
	dm.Status.InitializeConditions()
}
