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
	Congfiguration's LatestReadyRevisionName points to it. Also the previous Congfiguration
	still exists.
- When a Congfiguration controller is created and a Congfiguration is already
	out of sync, the controller creates or updates a Revision for the out of sync
	Congfiguration.
*/
import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	buildv1alpha1 "github.com/elafros/elafros/pkg/apis/cloudbuild/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"

	hooks "github.com/elafros/elafros/pkg/controller/testing"

	"k8s.io/client-go/rest"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

const (
	testNamespace string = "test"
	revName       string = "test-rev"
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
					// corev1.Container has a lot of setting.  We try to pass many
					// of them here to verify that we pass through the settings to
					// the derived Revisions.
					ContainerSpec: &corev1.Container{
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

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      revName,
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			ContainerSpec: &corev1.Container{
				Image: "test-image",
			},
		},
	}
}

func newTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset()

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	controller = NewController(
		kubeClient,
		elaClient,
		kubeInformer,
		elaInformer,
		&rest.Config{},
	).(*Controller)

	return
}

func newRunningTestController(t *testing.T) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, elaClient, controller, kubeInformer, elaInformer = newTestController(t)

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

func keyOrDie(obj interface{}) string {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		panic(err)
	}
	return key
}

func TestCreateConfigurationsCreatesRevision(t *testing.T) {
	kubeClient, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	h := hooks.NewHooks()

	h.OnCreate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		rev := obj.(*v1alpha1.Revision)
		glog.Infof("checking revision %s", rev.Name)
		if diff := cmp.Diff(config.Spec.Template.Spec, rev.Spec); diff != "" {
			t.Errorf("rev spec != config template spec (-want +got): %v", diff)
		}

		if rev.Labels[ela.ConfigurationLabelKey] != config.Name {
			t.Errorf("rev does not have label <%s:%s>", ela.ConfigurationLabelKey, config.Name)
		}

		if len(rev.OwnerReferences) != 1 || config.Name != rev.OwnerReferences[0].Name {
			t.Errorf("expected owner references to have 1 ref with name %s", config.Name)
		}

		return hooks.HookComplete
	})

	// Look for the events.
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) hooks.HookResult {
		event := obj.(*corev1.Event)
		expectedMessages := "Created Revision:"
		if !strings.HasPrefix(event.Message, expectedMessages) {
			t.Errorf("unexpected Message: %q expected beginning with: %q", event.Message, expectedMessages)
		}
		if wanted, got := corev1.EventTypeNormal, event.Type; wanted != got {
			t.Errorf("unexpected event Type: %q expected: %q", got, wanted)
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
	kubeClient, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName
	h := hooks.NewHooks()

	update := 0
	h.OnUpdate(&elaClient.Fake, "configurations", func(obj runtime.Object) hooks.HookResult {
		config := obj.(*v1alpha1.Configuration)
		update = update + 1
		switch update {
		case 1:
			// This update is triggered by the configuration creation.
			if got, want := len(config.Status.Conditions), 0; !reflect.DeepEqual(got, want) {
				t.Errorf("Conditions length diff; got %v, want %v", got, want)
			}
			if got, want := config.Status.LatestReadyRevisionName, ""; got != want {
				t.Errorf("Latest in Status diff; got %v, want %v", got, want)
			}
			controllerRef := metav1.NewControllerRef(config, controllerKind)
			revision := getTestRevision()
			revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
			revision.Status = v1alpha1.RevisionStatus{
				Conditions: []v1alpha1.RevisionCondition{{
					Type:   v1alpha1.RevisionConditionReady,
					Status: corev1.ConditionTrue,
				}},
			}
			// This call should update configration.
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
			if got, want := config.Status.LatestReadyRevisionName, revName; got != want {
				t.Errorf("Latest in Status diff; got %v, want %v", got, want)
			}
		}
		return hooks.HookComplete
	})

	// Look for the event
	expectedMessages := map[string]struct{}{
		"Configuration becomes ready":                      struct{}{},
		"LatestReadyRevisionName is updated to 'test-rev'": struct{}{},
	}
	eventNum := 0
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) hooks.HookResult {
		event := obj.(*corev1.Event)
		eventNum = eventNum + 1
		if strings.HasPrefix(event.Message, "Created Revision:") {
			// The first revision created event.
			return hooks.HookIncomplete
		}
		if _, ok := expectedMessages[event.Message]; !ok {
			t.Errorf("unexpected Message: %q expected one of: %q", event.Message, expectedMessages)
		}
		if wanted, got := corev1.EventTypeNormal, event.Type; wanted != got {
			t.Errorf("unexpected event Type: %q expected: %q", got, wanted)
		}
		// Expect 3 events.
		if eventNum < 3 {
			return hooks.HookIncomplete
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Configurations(testNamespace).Create(config)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDoNotUpdateConfigurationWhenRevisionIsNotReady(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ElafrosV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName

	configClient.Create(config)
	// Since addRevisionEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)

	// Get the configuration after reconciling
	reconciledConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	// Create a revision owned by this Configuration. Calling IsReady() on this
	// revision will return false.
	controllerRef := metav1.NewControllerRef(config, controllerKind)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	controller.addRevisionEvent(revision)

	// Configuration should not have changed.
	actualConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	if diff := cmp.Diff(reconciledConfig, actualConfig); diff != "" {
		t.Errorf("Unexpected configuration diff (-want +got): %v", diff)
	}
}

func TestDoNotUpdateConfigurationWhenReadyRevisionIsNotLatestCreated(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ElafrosV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	// Don't set LatestCreatedRevisionName.

	configClient.Create(config)
	// Since addRevisionEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)

	// Get the configuration after reconciling
	reconciledConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	// Create a revision owned by this Configuration. This revision is Ready, but
	// doesn't match the LatestCreatedRevisionName.
	controllerRef := metav1.NewControllerRef(config, controllerKind)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}

	controller.addRevisionEvent(revision)

	// Configuration should not have changed.
	actualConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	if diff := cmp.Diff(reconciledConfig, actualConfig); diff != "" {
		t.Errorf("Unexpected configuration diff (-want +got): %v", diff)
	}
}

func TestDoNotUpdateConfigurationWhenLatestReadyRevisionNameIsUpToDate(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ElafrosV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status = v1alpha1.ConfigurationStatus{
		Conditions: []v1alpha1.ConfigurationCondition{
			v1alpha1.ConfigurationCondition{
				Type:   v1alpha1.ConfigurationConditionReady,
				Status: corev1.ConditionTrue,
				Reason: "LatestRevisionReady",
			},
		},
		LatestCreatedRevisionName: revName,
		LatestReadyRevisionName:   revName,
	}
	configClient.Create(config)
	// Since addRevisionEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Configurations().Informer().GetIndexer().Add(config)

	// Create a revision owned by this Configuration. This revision is Ready and
	// matches the Configuration's LatestReadyRevisionName.
	controllerRef := metav1.NewControllerRef(config, controllerKind)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}

	// In this case, we can't tell if addRevisionEvent has updated the
	// Configuration, because it's already in the expected state. Use a reactor
	// instead to test whether Update() is called.
	elaClient.Fake.PrependReactor("update", "configurations", func(a kubetesting.Action) (bool, runtime.Object, error) {
		t.Error("Configuration was updated unexpectedly")
		return true, nil, nil
	})

	controller.addRevisionEvent(revision)
}
