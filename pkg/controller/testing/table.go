/*
Copyright 2018 The Knative Authors.

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

package testing

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/system"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	istiov1alpha3 "github.com/knative/serving/pkg/apis/istio/v1alpha3"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/controller"
	. "github.com/knative/serving/pkg/logging/testing"
)

// Listers holds the universe of objects that are available at the start
// of a reconciliation.
type Listers struct {
	Service       *ServiceLister
	Route         *RouteLister
	Configuration *ConfigurationLister
	Revision      *RevisionLister

	VirtualService *VirtualServiceLister

	Build *BuildLister

	Deployment *DeploymentLister
	K8sService *K8sServiceLister
	Endpoints  *EndpointsLister
	ConfigMap  *ConfigMapLister
}

func (f *Listers) GetServiceLister() *ServiceLister {
	if f.Service == nil {
		return &ServiceLister{}
	}
	return f.Service
}

func (f *Listers) GetVirtualServiceLister() *VirtualServiceLister {
	if f.VirtualService == nil {
		return &VirtualServiceLister{}
	}
	return f.VirtualService
}

func (f *Listers) GetRouteLister() *RouteLister {
	if f.Route == nil {
		return &RouteLister{}
	}
	return f.Route
}

func (f *Listers) GetConfigurationLister() *ConfigurationLister {
	if f.Configuration == nil {
		return &ConfigurationLister{}
	}
	return f.Configuration
}

func (f *Listers) GetRevisionLister() *RevisionLister {
	if f.Revision == nil {
		return &RevisionLister{}
	}
	return f.Revision
}

func (f *Listers) GetBuildLister() *BuildLister {
	if f.Build == nil {
		return &BuildLister{}
	}
	return f.Build
}

func (f *Listers) GetDeploymentLister() *DeploymentLister {
	if f.Deployment == nil {
		return &DeploymentLister{}
	}
	return f.Deployment
}

func (f *Listers) GetK8sServiceLister() *K8sServiceLister {
	if f.K8sService == nil {
		return &K8sServiceLister{}
	}
	return f.K8sService
}

func (f *Listers) GetEndpointsLister() *EndpointsLister {
	if f.Endpoints == nil {
		return &EndpointsLister{}
	}
	return f.Endpoints
}

func (f *Listers) GetConfigMapLister() *ConfigMapLister {
	if f.ConfigMap == nil {
		return &ConfigMapLister{}
	}
	return f.ConfigMap
}

func (f *Listers) GetKubeObjects() []runtime.Object {
	var kubeObjs []runtime.Object
	for _, r := range f.GetDeploymentLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	for _, r := range f.GetK8sServiceLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	for _, r := range f.GetEndpointsLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	for _, r := range f.GetConfigMapLister().Items {
		kubeObjs = append(kubeObjs, r)
	}
	return kubeObjs
}

func (f *Listers) GetBuildObjects() []runtime.Object {
	var buildObjs []runtime.Object
	for _, r := range f.GetBuildLister().Items {
		buildObjs = append(buildObjs, r)
	}
	return buildObjs
}

func (f *Listers) GetServingObjects() []runtime.Object {
	var objs []runtime.Object
	for _, r := range f.GetServiceLister().Items {
		objs = append(objs, r)
	}
	for _, r := range f.GetRouteLister().Items {
		objs = append(objs, r)
	}
	for _, r := range f.GetConfigurationLister().Items {
		objs = append(objs, r)
	}
	for _, r := range f.GetRevisionLister().Items {
		objs = append(objs, r)
	}
	for _, r := range f.GetVirtualServiceLister().Items {
		objs = append(objs, r)
	}
	return objs
}

func NewListers(objs []runtime.Object) Listers {
	ls := Listers{
		Service:       &ServiceLister{},
		Route:         &RouteLister{},
		Configuration: &ConfigurationLister{},
		Revision:      &RevisionLister{},

		VirtualService: &VirtualServiceLister{},

		Build: &BuildLister{},

		Deployment: &DeploymentLister{},
		K8sService: &K8sServiceLister{},
		Endpoints:  &EndpointsLister{},
		ConfigMap:  &ConfigMapLister{},
	}
	for _, obj := range objs {
		switch o := obj.(type) {
		case *v1alpha1.Service:
			ls.Service.Items = append(ls.Service.Items, o)
		case *v1alpha1.Route:
			ls.Route.Items = append(ls.Route.Items, o)
		case *v1alpha1.Configuration:
			ls.Configuration.Items = append(ls.Configuration.Items, o)
		case *v1alpha1.Revision:
			ls.Revision.Items = append(ls.Revision.Items, o)

		case *istiov1alpha3.VirtualService:
			ls.VirtualService.Items = append(ls.VirtualService.Items, o)

		case *buildv1alpha1.Build:
			ls.Build.Items = append(ls.Build.Items, o)

		case *appsv1.Deployment:
			ls.Deployment.Items = append(ls.Deployment.Items, o)
		case *corev1.Service:
			ls.K8sService.Items = append(ls.K8sService.Items, o)
		case *corev1.Endpoints:
			ls.Endpoints.Items = append(ls.Endpoints.Items, o)
		case *corev1.ConfigMap:
			ls.ConfigMap.Items = append(ls.ConfigMap.Items, o)

		default:
			panic(fmt.Sprintf("Unsupported type in TableTest %T", obj))
		}
	}
	return ls
}

// TableRow holds a single row of our table test.
type TableRow struct {
	// Name is a descriptive name for this test suitable as a first argument to t.Run()
	Name string

	// Objects holds the state of the world at the onset of reconciliation.
	Objects []runtime.Object

	// Key is the parameter to reconciliation.
	// This has the form "namespace/name".
	Key string

	// WantErr holds whether we should expect the reconciliation to result in an error.
	WantErr bool

	// WantCreates holds the set of Create calls we expect during reconciliation.
	WantCreates []metav1.Object

	// WantUpdates holds the set of Update calls we expect during reconciliation.
	WantUpdates []clientgotesting.UpdateActionImpl

	// WantDeletes holds the set of Delete calls we expect during reconciliation.
	WantDeletes []clientgotesting.DeleteActionImpl

	// WantQueue is the set of keys we expect to be in the workqueue following reconciliation.
	WantQueue []string

	// WithReactors is a set of functions that are installed as Reactors for the execution
	// of this row of the table-driven-test.
	WithReactors []clientgotesting.ReactionFunc
}

type Ctor func(*Listers, controller.Options) controller.Interface

func (r *TableRow) Test(t *testing.T, ctor Ctor) {
	ls := NewListers(r.Objects)

	kubeClient := fakekubeclientset.NewSimpleClientset(ls.GetKubeObjects()...)
	client := fakeclientset.NewSimpleClientset(ls.GetServingObjects()...)
	buildClient := fakebuildclientset.NewSimpleClientset(ls.GetBuildObjects()...)
	// Set up our Controller from the fakes.
	c := ctor(&ls, controller.Options{
		KubeClientSet:    kubeClient,
		BuildClientSet:   buildClient,
		ServingClientSet: client,
		Logger:           TestLogger(t),
	})

	for _, reactor := range r.WithReactors {
		kubeClient.PrependReactor("*", "*", reactor)
		client.PrependReactor("*", "*", reactor)
		buildClient.PrependReactor("*", "*", reactor)
	}

	// Validate all Create operations through the serving client.
	client.PrependReactor("create", "*", ValidateCreates)
	client.PrependReactor("update", "*", ValidateUpdates)

	// Run the Reconcile we're testing.
	if err := c.Reconcile(r.Key); (err != nil) != r.WantErr {
		t.Errorf("Reconcile() error = %v, WantErr %v", err, r.WantErr)
	}
	// Now check that the Reconcile had the desired effects.
	expectedNamespace, _, _ := cache.SplitMetaNamespaceKey(r.Key)

	c.GetWorkQueue().ShutDown()
	gotQueue := drainWorkQueue(c.GetWorkQueue())
	if diff := cmp.Diff(r.WantQueue, gotQueue); diff != "" {
		t.Errorf("unexpected queue (-Want +got): %s", diff)
	}

	createActions, updateActions, deleteActions := extractActions(t, buildClient, client, kubeClient)

	for i, want := range r.WantCreates {
		if i >= len(createActions) {
			t.Errorf("Missing create: %v", want)
			continue
		}
		got := createActions[i]
		if got.GetNamespace() != expectedNamespace && got.GetNamespace() != system.Namespace {
			t.Errorf("unexpected action[%d]: %#v", i, got)
		}
		obj := got.GetObject()
		if diff := cmp.Diff(want, obj, ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected create (-want +got): %s", diff)
		}
	}
	if got, want := len(createActions), len(r.WantCreates); got > want {
		for _, extra := range createActions[want:] {
			t.Errorf("Extra create: %v", extra)
		}
	}

	for i, want := range r.WantUpdates {
		if i >= len(updateActions) {
			t.Errorf("Missing update: %v", want.GetObject())
			continue
		}
		got := updateActions[i]
		if diff := cmp.Diff(want.GetObject(), got.GetObject(), ignoreLastTransitionTime, safeDeployDiff, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("unexpected update (-want +got): %s", diff)
		}
	}
	if got, want := len(updateActions), len(r.WantUpdates); got > want {
		for _, extra := range updateActions[want:] {
			t.Errorf("Extra update: %v", extra)
		}
	}

	for i, want := range r.WantDeletes {
		if i >= len(deleteActions) {
			t.Errorf("Missing delete: %v", want)
			continue
		}
		got := deleteActions[i]
		if got.GetName() != want.Name {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
		if got.GetNamespace() != expectedNamespace && got.GetNamespace() != system.Namespace {
			t.Errorf("unexpected delete[%d]: %#v", i, got)
		}
	}
	if got, want := len(deleteActions), len(r.WantDeletes); got > want {
		for _, extra := range deleteActions[want:] {
			t.Errorf("Extra delete: %v", extra)
		}
	}
}

type hasActions interface {
	Actions() []clientgotesting.Action
}

func extractActions(t *testing.T, clients ...hasActions) (createActions []clientgotesting.CreateAction,
	updateActions []clientgotesting.UpdateAction,
	deleteActions []clientgotesting.DeleteAction) {

	for _, c := range clients {
		for _, action := range c.Actions() {
			switch action.GetVerb() {
			case "create":
				createActions = append(createActions,
					action.(clientgotesting.CreateAction))
			case "update":
				updateActions = append(updateActions,
					action.(clientgotesting.UpdateAction))
			case "delete":
				deleteActions = append(deleteActions,
					action.(clientgotesting.DeleteAction))
			default:
				t.Errorf("Unexpected verb %v: %+v", action.GetVerb(), action)
			}
		}
	}
	return
}

func drainWorkQueue(wq workqueue.RateLimitingInterface) (hasQueue []string) {
	for {
		key, shutdown := wq.Get()
		if shutdown {
			break
		}
		hasQueue = append(hasQueue, key.(string))
	}
	return
}

type TableTest []TableRow

func (tt TableTest) Test(t *testing.T, ctor Ctor) {
	for _, test := range tt {
		t.Run(test.Name, func(t *testing.T) {
			test.Test(t, ctor)
		})
	}
}

var ignoreLastTransitionTime = cmp.FilterPath(func(p cmp.Path) bool {
	return strings.HasSuffix(p.String(), "LastTransitionTime.Time")
}, cmp.Ignore())

var safeDeployDiff = cmpopts.IgnoreUnexported(resource.Quantity{})
