/*
Copyright 2017 The Kubernetes Authors.

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

package revisiontemplate

/* TODO tests:
- When a RT is created or updated, a Revision is created with same namespace and spec
  and RT's latest points to it
- If a RT is created and deleted before the queue fires, no Revision is created
- When a RT is updated, a new Revision is created and RT's latest points to it.
  Also the previous RT still exists.
- When a RT controller is created and a RT is already out of sync, the
  controller creates or updates a Revision for the out of sync RT
-
*/
import (
	"testing"
	"time"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/google/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/google/elafros/pkg/client/informers/externalversions"

	hooks "github.com/google/elafros/pkg/controller/testing"

	"k8s.io/client-go/rest"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func getTestRevisionTemplate() *v1alpha1.RevisionTemplate {
	return &v1alpha1.RevisionTemplate{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisiontemplates/test-rt",
			Name:      "test-rt",
			Namespace: "test",
		},
		Spec: v1alpha1.RevisionTemplateSpec{
			//TODO(grantr): This is a workaround for generation initialization
			Generation: 1,
			Template: v1alpha1.Revision{
				Spec: v1alpha1.RevisionSpec{
					Service: "test-service",
				},
			},
		},
	}
}

func newRunningTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	// Create a controller and safe cast it to the proper type. This is necessary
	// because NewController returns controller.Interface.
	controller, ok := NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
	).(*Controller)
	if !ok {
		t.Fatal("cast to *Controller failed")
	}

	// Start the informers. This must happen after the call to NewController,
	// otherwise there are no informers to be started.
	stopCh = make(chan struct{})
	kubeInformer.Start(stopCh)
	elaInformer.Start(stopCh)

	// Run the controller.
	go func() {
		if err := controller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	return
}

func TestCreateRTCreatesPR(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rt := getTestRevisionTemplate()
	h := hooks.NewHooks()

	h.OnCreate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		rev := obj.(*v1alpha1.Revision)
		glog.Infof("checking revision %s", rev.Name)
		if rt.Spec.Template.Spec.Service != rev.Spec.Service {
			t.Errorf("rev service was not %s", rt.Spec.Template.Spec.Service)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().RevisionTemplates("test").Create(rt)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
