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

package configuration

import (
	"testing"
	"time"

	fakesharedclientset "github.com/knative/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/configmap"
	ctrl "github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/gc"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/system"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

/* TODO tests:
- syncHandler returns error (in processNextWorkItem)
- invalid key in workqueue (in processNextWorkItem)
- object cannot be converted to key (in enqueueConfiguration)
- invalid key given to syncHandler
- resource doesn't exist in lister (from syncHandler)
*/

const (
	testNamespace = "test"
)

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			// TODO(grantr): This is a workaround for generation initialization
			Generation: 1,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					ServiceAccountName: "test-account",
					// corev1.Container has a lot of setting.  We try to pass many
					// of them here to verify that we pass through the settings to
					// the derived Revisions.
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
							TimeoutSeconds: 43,
						},
						TerminationMessagePath: "/dev/null",
					},
				},
			},
		},
	}
}

func newTestController(t *testing.T, servingObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	sharedClient *fakesharedclientset.Clientset,
	servingClient *fakeclientset.Clientset,
	controller *ctrl.Impl,
	kubeInformer kubeinformers.SharedInformerFactory,
	servingInformer informers.SharedInformerFactory) {
	// Create config
	configMapWatcher := configmap.NewStaticWatcher(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gc.ConfigName,
			Namespace: system.Namespace,
		},
		Data: map[string]string{},
	})

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	sharedClient = fakesharedclientset.NewSimpleClientset()
	// The ability to insert objects here is intended to work around the problem
	// with watches not firing in client-go 1.9. When we update to client-go 1.10
	// this can probably be removed.
	servingClient = fakeclientset.NewSimpleClientset(servingObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	servingInformer = informers.NewSharedInformerFactory(servingClient, 0)

	controller = NewController(
		reconciler.Options{
			KubeClientSet:    kubeClient,
			SharedClientSet:  sharedClient,
			ServingClientSet: servingClient,
			ConfigMapWatcher: configMapWatcher,
			Logger:           TestLogger(t),
		},
		servingInformer.Serving().V1alpha1().Configurations(),
		servingInformer.Serving().V1alpha1().Revisions(),
	)

	return
}

func TestNewConfigurationCallsSyncHandler(t *testing.T) {
	config := getTestConfiguration()
	// TODO(grantr): inserting the configuration at client creation is necessary
	// because ObjectTracker doesn't fire watches in the 1.9 client. When we
	// upgrade to 1.10 we can remove the config argument here and instead use the
	// Create() method.
	_, _, servingClient, controller, kubeInformer, servingInformer := newTestController(t, config)

	h := NewHooks()

	// Check for revision created as a signal that syncHandler ran
	h.OnCreate(&servingClient.Fake, "revisions", func(obj runtime.Object) HookResult {
		rev := obj.(*v1alpha1.Revision)
		t.Logf("revision created: %q", rev.Name)

		return HookComplete
	})

	stopCh := make(chan struct{})
	eg := errgroup.Group{}
	defer func() {
		close(stopCh)
		if err := eg.Wait(); err != nil {
			t.Fatalf("Error running controller: %v", err)
		}
	}()
	kubeInformer.Start(stopCh)
	servingInformer.Start(stopCh)

	eg.Go(func() error {
		return controller.Run(2, stopCh)
	})

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
