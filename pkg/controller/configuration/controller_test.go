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

package configuration

/* TODO tests:
- If a Congfiguration is created and deleted before the queue fires, no Revision
  is created.
- When a Congfiguration is updated, a new Revision is created and
	Congfiguration's LatestReady points to it. Also the previous Congfiguration
	still exists.
- When a Congfiguration controller is created and a Congfiguration is already
	out of sync, the controller creates or updates a Revision for the out of sync
	Congfiguration.
*/
import (
	"reflect"
	"testing"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	buildv1alpha1 "github.com/google/elafros/pkg/apis/cloudbuild/v1alpha1"
	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/google/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/google/elafros/pkg/client/informers/externalversions"

	hooks "github.com/google/elafros/pkg/controller/testing"

	"k8s.io/client-go/rest"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace string = "test"
)

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
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

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Service: "test-service",
			ContainerSpec: &corev1.Container{
				Image: "test-image",
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

func TestCreateConfigurationsCreatesRevision(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	h := hooks.NewHooks()

	h.OnCreate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		rev := obj.(*v1alpha1.Revision)
		glog.Infof("checking revision %s", rev.Name)
		if config.Spec.Template.Spec.Service != rev.Spec.Service {
			t.Errorf("rev service was not %s", config.Spec.Template.Spec.Service)
		}

		if rev.Labels[ConfigurationLabelKey] != config.Name {
			t.Errorf("rev does not have lable <%s:%s>", ConfigurationLabelKey, config.Name)
		}

		if len(rev.OwnerReferences) != 1 || config.Name != rev.OwnerReferences[0].Name {
			t.Errorf("expected owner references to have 1 ref with name %s", config.Name)
		}

		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateConfigurationCreatesBuildAndPR(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	h := hooks.NewHooks()

	config.Spec.Build = &buildv1alpha1.BuildSpec{
		Steps: []corev1.Container{{
			Name:    "nop",
			Image:   "busybox:latest",
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", "echo Hello"},
		}},
	}

	h.OnCreate(&elaClient.Fake, "builds", func(obj runtime.Object) hooks.HookResult {
		b := obj.(*buildv1alpha1.Build)
		glog.Infof("checking build %s", b.Name)
		if config.Spec.Build.Steps[0].Name != b.Spec.Steps[0].Name {
			t.Errorf("BuildSpec mismatch; want %v, got %v", config.Spec.Build.Steps[0], b.Spec.Steps[0])
		}
		return hooks.HookComplete
	})

	h.OnCreate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		rev := obj.(*v1alpha1.Revision)
		glog.Infof("checking revision %s", rev.Name)
		if config.Spec.Template.Spec.Service != rev.Spec.Service {
			t.Errorf("rev service was not %s", config.Spec.Template.Spec.Service)
		}
		// TODO(mattmoor): The fake doesn't properly support GenerateName,
		// so it never looks like the BuildName is populated.
		// if rev.Spec.BuildName == "" {
		// 	t.Error("Missing BuildName; want non-empty, but got empty string")
		// }
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestMarkConfigurationReadyWhenLatestRevisionReady(t *testing.T) {
	_, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	h := hooks.NewHooks()

	controllerRef := metav1.NewControllerRef(config, controllerKind)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	config.Status.LatestCreated = revision.Name

	// Ensure that the Configuration status is updated.
	update := 0
	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		update = update + 1
		switch update {
		case 1:
			// Not update if the revision is not ready.
			if got, want := len(config.Status.Conditions), 0; !reflect.DeepEqual(got, want) {
				t.Errorf("Conditions length diff; got %v, want %v", got, want)
			}
			if got, want := config.Status.LatestReady, ""; got != want {
				t.Errorf("Latest in Stauts diff; got %v, want %v", got, want)
			}
			// After the initial update to the configuration, we should be
			// watching for this revision to become ready.
			revision.Status = v1alpha1.RevisionStatus{
				Conditions: []v1alpha1.RevisionCondition{{
					Type:   v1alpha1.RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			}
			go controller.addRevisionEvent(revision)
			return hooks.HookIncomplete
		case 2:
			// The next update we receive should tell us that the revision is ready.
			expectedConfigConditions := []v1alpha1.ConfigurationCondition{
				v1alpha1.ConfigurationCondition{
					Type:   v1alpha1.ConfigurationConditionReady,
					Status: corev1.ConditionTrue,
					Reason: "LatestRevisionReady",
				},
			}

			if got, want := config.Status.Conditions, expectedConfigConditions; !reflect.DeepEqual(got, want) {
				t.Errorf("Conditions diff; got %v, want %v", got, want)
			}
			if got, want := config.Status.LatestReady, revision.Name; got != want {
				t.Errorf("Latest in Stauts diff; got %v, want %v", got, want)
			}
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(revision)
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDoNotSetLatestWhenReadyRevisionIsNotLastestCreated(t *testing.T) {
	_, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	h := hooks.NewHooks()

	controllerRef := metav1.NewControllerRef(config, controllerKind)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)

	// Mark the revision ready to trigger an update
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}
	go controller.addRevisionEvent(revision)

	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		if got, want := len(config.Status.Conditions), 0; !reflect.DeepEqual(got, want) {
			t.Errorf("Conditions length diff; got %v, want %v", got, want)
		}
		if got, want := config.Status.LatestReady, ""; got != want {
			t.Errorf("Latest in Stauts diff; got %v, want %v", got, want)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(revision)
	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
