/*
Copyright 2018 Google LLC.

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

package revision

import (
	"testing"
	"time"

	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/controller"
	. "github.com/knative/serving/pkg/controller/testing"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

const testNamespace string = "test"

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
			Labels: map[string]string{
				"testLabel1":          "foo",
				"testLabel2":          "bar",
				serving.RouteLabelKey: "test-route",
			},
			Annotations: map[string]string{
				"testAnnotation": "test",
			},
			UID: "test-rev-uid",
		},
		Spec: v1alpha1.RevisionSpec{
			// corev1.Container has a lot of setting.  We try to pass many
			// of them here to verify that we pass through the settings to
			// derived objects.
			Container: corev1.Container{
				Image:      "gcr.io/repo/image",
				Command:    []string{"echo"},
				Args:       []string{"hello", "world"},
				WorkingDir: "/tmp",
				Env: []corev1.EnvVar{{
					Name:  "EDITOR",
					Value: "emacs",
				}},
				LivenessProbe: &corev1.Probe{
					TimeoutSeconds: 42,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path: "health",
						},
					},
					TimeoutSeconds: 43,
				},
				TerminationMessagePath: "/dev/null",
			},
			ServingState: v1alpha1.RevisionServingStateActive,
		},
	}
}

type receiver struct {
	kubeClient *fakekubeclientset.Clientset
}

func (r *receiver) SyncRevision(rev *v1alpha1.Revision) error {
	_, err := r.kubeClient.Core().Services(rev.Namespace).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: rev.Namespace,
			Name:      rev.Name,
		},
	})
	return err
}

var _ Receiver = (*receiver)(nil)

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	kontroller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	var err error
	ctrl, err := NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		// buildInformer,
		&rest.Config{},
		controller.Config{},
		zap.NewNop().Sugar(),
		&receiver{kubeClient},
	)
	if err != nil {
		t.Fatalf("Error creating controller: %v", err)
	}
	kontroller = ctrl.(*Controller)

	// Start the informers. This must happen after the call to NewController,
	// otherwise there are no informers to be started.
	stopCh = make(chan struct{})
	kubeInformer.Start(stopCh)
	elaInformer.Start(stopCh)

	// Run the controller.
	go func() {
		if err := kontroller.Run(2, stopCh); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()

	return
}

func TestNewRevisionCallsSyncHandler(t *testing.T) {
	rev := getTestRevision()
	// TODO(grantr): inserting the route at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	kubeClient, _, _, _, _, stopCh := newRunningTestController(t, rev)
	defer close(stopCh)

	h := NewHooks()

	// Check for a service created as a signal that syncHandler ran
	h.OnCreate(&kubeClient.Fake, "services", func(obj runtime.Object) HookResult {
		service := obj.(*corev1.Service)
		t.Logf("service created: %q", service.Name)

		return HookComplete
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
