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
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	fakebuildclientset "github.com/knative/build/pkg/client/clientset/versioned/fake"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	fakeclientset "github.com/knative/serving/pkg/client/clientset/versioned/fake"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	ctrl "github.com/knative/serving/pkg/controller"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	. "github.com/knative/serving/pkg/controller/testing"
)

const (
	testNamespace string = "test"
)

var revName string = getTestRevision().Name

func getTestConfiguration() *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/configurations/test-config",
			Name:      "test-config",
			Namespace: testNamespace,
		},
		Spec: v1alpha1.ConfigurationSpec{
			//TODO(grantr): This is a workaround for generation initialization
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

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/serving/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      generateRevisionName(getTestConfiguration()),
			Namespace: testNamespace,
		},
		Spec: v1alpha1.RevisionSpec{
			Container: corev1.Container{
				Image: "test-image",
			},
		},
	}
}

func newTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	buildClient *fakebuildclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	buildClient = fakebuildclientset.NewSimpleClientset()
	// The ability to insert objects here is intended to work around the problem
	// with watches not firing in client-go 1.9. When we update to client-go 1.10
	// this can probably be removed.
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

	// Create informer factories with fake clients. The second parameter sets the
	// resync period to zero, disabling it.
	kubeInformer = kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	elaInformer = informers.NewSharedInformerFactory(elaClient, 0)

	controller = NewController(
		ctrl.Options{
			KubeClientSet:    kubeClient,
			ServingClientSet: elaClient,
			BuildClientSet:   buildClient,
			Logger:           zap.NewNop().Sugar(),
		},
		elaInformer.Serving().V1alpha1().Configurations(),
		elaInformer.Serving().V1alpha1().Revisions(),
		&rest.Config{},
	).(*Controller)

	return
}

func newRunningTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory,
	stopCh chan struct{}) {

	kubeClient, _, elaClient, controller, kubeInformer, elaInformer = newTestController(t, elaObjects...)

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
	kubeClient, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)
	config := getTestConfiguration()
	h := NewHooks()

	// Look for the event. Events are delivered asynchronously so we need to use
	// hooks here.
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "Created Revision .+"))

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	list, err := elaClient.ServingV1alpha1().Revisions(testNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing revisions: %v", err)
	}

	if got, want := len(list.Items), 1; got != want {
		t.Fatalf("expected %v revisions, got %v", want, got)
	}

	rev := list.Items[0].DeepCopy()
	if diff := cmp.Diff(config.Spec.RevisionTemplate.Spec, rev.Spec); diff != "" {
		t.Errorf("rev spec != config RevisionTemplate spec (-want +got): %v", diff)
	}

	if rev.Labels[serving.ConfigurationLabelKey] != config.Name {
		t.Errorf("rev does not have configuration label <%s:%s>", serving.ConfigurationLabelKey, config.Name)
	}

	if rev.Annotations[serving.ConfigurationGenerationAnnotationKey] != fmt.Sprintf("%v", config.Spec.Generation) {
		t.Errorf("rev does not have generation annotation <%s:%s>", serving.ConfigurationGenerationAnnotationKey, config.Name)
	}

	for k, v := range config.Spec.RevisionTemplate.ObjectMeta.Labels {
		if rev.Labels[k] != v {
			t.Errorf("revisionTemplate label %s=%s not passed to revision", k, v)
		}
	}

	for k, v := range config.Spec.RevisionTemplate.ObjectMeta.Annotations {
		if rev.Annotations[k] != v {
			t.Errorf("revisionTemplate annotation %s=%s not passed to revision", k, v)
		}
	}

	if len(rev.OwnerReferences) != 1 || config.Name != rev.OwnerReferences[0].Name {
		t.Errorf("expected owner references to have 1 ref with name %s", config.Name)
	}

	// Check that rerunning reconciliation does nothing.
	reconciledConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(reconciledConfig)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	if err := controller.Reconcile(KeyOrDie(reconciledConfig)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	list, err = elaClient.ServingV1alpha1().Revisions(testNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing revisions: %v", err)
	}
	// Still have one revision.
	if got, want := len(list.Items), 1; got != want {
		t.Fatalf("expected %v revisions, got %v", want, got)
	}

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateConfigurationCreatesBuildAndRevision(t *testing.T) {
	_, buildClient, elaClient, controller, _, elaInformer := newTestController(t)
	config := getTestConfiguration()
	config.Spec.Build = &buildv1alpha1.BuildSpec{
		Steps: []corev1.Container{{
			Name:    "nop",
			Image:   "busybox:latest",
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", "echo Hello"},
		}},
	}

	elaClient.ServingV1alpha1().Configurations(testNamespace).Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	revList, err := elaClient.ServingV1alpha1().Revisions(testNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing revisions: %v", err)
	}
	if got, want := len(revList.Items), 1; got != want {
		t.Fatalf("expected %v revisions, got %v", want, got)
	}
	if got, want := revList.Items[0].Spec.ServiceAccountName, "test-account"; got != want {
		t.Fatalf("expected service account name %v, got %v", want, got)
	}

	buildList, err := buildClient.BuildV1alpha1().Builds(testNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing builds: %v", err)
	}
	if got, want := len(buildList.Items), 1; got != want {
		t.Fatalf("expected %v builds, got %v", want, got)
	}

	b := buildList.Items[0]
	if diff := cmp.Diff(config.Spec.Build.Steps, b.Spec.Steps); diff != "" {
		t.Errorf("Unexpected build steps diff (-want +got): %v", diff)
	}
}

func TestMarkConfigurationsReadyWhenLatestRevisionReady(t *testing.T) {
	kubeClient, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName

	// Events are delivered asynchronously so we need to use hooks here. Each hook
	// tests for a specific event.
	h := NewHooks()
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "Configuration becomes ready"))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "LatestReadyRevisionName updated to .+"))

	configClient.Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	reconciledConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	// Config should be initialized with its conditions as Unknown.
	if got, want := len(reconciledConfig.Status.Conditions), 1; got != want {
		t.Errorf("Conditions length diff; got %v, want %v", got, want)
	}
	// Config should not have a latest ready revision
	if got, want := reconciledConfig.Status.LatestReadyRevisionName, ""; got != want {
		t.Errorf("Latest in Status diff; got %v, want %v", got, want)
	}

	// Get the revision created
	revList, err := elaClient.ServingV1alpha1().Revisions(config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing revisions: %v", err)
	}
	if got, want := len(revList.Items), 1; got != want {
		t.Fatalf("expected %d revisions, got %d", want, got)
	}
	revision := revList.Items[0].DeepCopy()

	// mark the revision as Ready
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(reconciledConfig)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(revision)
	if err := controller.Reconcile(KeyOrDie(reconciledConfig)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	readyConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	for _, ct := range []v1alpha1.ConfigurationConditionType{"Ready"} {
		got := readyConfig.Status.GetCondition(ct)
		want := &v1alpha1.ConfigurationCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}

	if got, want := readyConfig.Status.LatestReadyRevisionName, revision.Name; got != want {
		t.Errorf("Latest in Status diff; got %v, want %v", got, want)
	}

	// wait for events to be created
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDoNotUpdateConfigurationWhenRevisionIsNotReady(t *testing.T) {
	_, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName

	configClient.Create(config)

	// Create a revision owned by this Configuration. Calling IsReady() on this
	// revision will return false.
	controllerRef := ctrl.NewConfigurationControllerRef(config)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(revision)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	// Configuration should not have changed.
	actualConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	if diff := cmp.Diff(config, actualConfig); diff != "" {
		t.Errorf("Unexpected configuration diff (-want +got): %v", diff)
	}
}

func TestDoNotUpdateConfigurationWhenReadyRevisionIsNotLatestCreated(t *testing.T) {
	_, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	// Don't set LatestCreatedRevisionName.

	configClient.Create(config)

	// Create a revision owned by this Configuration. This revision is Ready, but
	// doesn't match the LatestCreatedRevisionName.
	controllerRef := ctrl.NewConfigurationControllerRef(config)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}

	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(revision)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	// Configuration should not have changed.
	actualConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	if diff := cmp.Diff(config, actualConfig); diff != "" {
		t.Errorf("Unexpected configuration diff (-want +got): %v", diff)
	}
}

func TestDoNotUpdateConfigurationWhenLatestReadyRevisionNameIsUpToDate(t *testing.T) {
	_, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status = v1alpha1.ConfigurationStatus{
		Conditions: []v1alpha1.ConfigurationCondition{{
			Type:   v1alpha1.ConfigurationConditionReady,
			Status: corev1.ConditionTrue,
			Reason: "Ready",
		}},
		LatestCreatedRevisionName: revName,
		LatestReadyRevisionName:   revName,
	}
	configClient.Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)

	// Create a revision owned by this Configuration. This revision is Ready and
	// matches the Configuration's LatestReadyRevisionName.
	controllerRef := ctrl.NewConfigurationControllerRef(config)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}

	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}
}

func TestMarkConfigurationStatusWhenLatestRevisionIsNotReady(t *testing.T) {
	kubeClient, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName

	// Events are delivered asynchronously so we need to use hooks here. Each hook
	// tests for a specific event.
	h := NewHooks()
	h.OnCreate(&kubeClient.Fake, "events", ExpectWarningEventDelivery(t, `Latest created revision "test-config-00001" has failed`))

	configClient.Create(config)
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	reconciledConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	// Get the revision created
	revList, err := elaClient.ServingV1alpha1().Revisions(config.Namespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatalf("error listing revisions: %v", err)
	}

	revision := revList.Items[0].DeepCopy()

	// mark the revision not ready with the status
	revision.Status.PropagateBuildStatus(buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{{
			Type:    buildv1alpha1.BuildSucceeded,
			Status:  corev1.ConditionFalse,
			Reason:  "StepFailed",
			Message: "Build step failed with error",
		}},
	})

	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(reconciledConfig)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(revision)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	readyConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	for _, ct := range []v1alpha1.ConfigurationConditionType{"Ready"} {
		got := readyConfig.Status.GetCondition(ct)
		want := &v1alpha1.ConfigurationCondition{
			Type:               ct,
			Status:             corev1.ConditionFalse,
			Reason:             "RevisionFailed",
			Message:            `revision "test-config-00001" failed with message: Build step failed with error`,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}

	if got, want := readyConfig.Status.LatestCreatedRevisionName, revision.Name; got != want {
		t.Errorf("LatestCreatedRevision do not match; got %v, want %v", got, want)
	}

	if got, want := readyConfig.Status.LatestReadyRevisionName, ""; got != want {
		t.Errorf("LatestReadyRevision should be empty; got %v, want %v", got, want)
	}

	// wait for events to be created
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestMarkConfigurationsReadyWhenLatestRevisionRecovers(t *testing.T) {
	kubeClient, _, elaClient, controller, _, elaInformer := newTestController(t)
	configClient := elaClient.ServingV1alpha1().Configurations(testNamespace)

	config := getTestConfiguration()
	config.Status.LatestCreatedRevisionName = revName
	config.Status.Conditions = []v1alpha1.ConfigurationCondition{{
		Type:    v1alpha1.ConfigurationConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  "BuildFailed",
		Message: "Build step failed with error",
	}}
	// Events are delivered asynchronously so we need to use hooks here. Each hook
	// tests for a specific event.
	h := NewHooks()
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "Configuration becomes ready"))
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, "LatestReadyRevisionName updated to .+"))

	configClient.Create(config)

	controllerRef := ctrl.NewConfigurationControllerRef(config)
	revision := getTestRevision()
	revision.OwnerReferences = append(revision.OwnerReferences, *controllerRef)
	// mark the revision as Ready
	revision.Status = v1alpha1.RevisionStatus{
		Conditions: []v1alpha1.RevisionCondition{{
			Type:   v1alpha1.RevisionConditionReady,
			Status: corev1.ConditionTrue,
		}},
	}
	// Since Reconcile looks in the lister, we need to add it to the informer
	elaInformer.Serving().V1alpha1().Configurations().Informer().GetIndexer().Add(config)
	elaInformer.Serving().V1alpha1().Revisions().Informer().GetIndexer().Add(revision)
	if err := controller.Reconcile(KeyOrDie(config)); err != nil {
		t.Fatalf("controller.Reconcile() = %v", err)
	}

	readyConfig, err := configClient.Get(config.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get config: %v", err)
	}

	for _, ct := range []v1alpha1.ConfigurationConditionType{"Ready"} {
		got := readyConfig.Status.GetCondition(ct)
		want := &v1alpha1.ConfigurationCondition{
			Type:               ct,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: got.LastTransitionTime,
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Unexpected config conditions diff (-want +got): %v", diff)
		}
	}

	if got, want := readyConfig.Status.LatestReadyRevisionName, revision.Name; got != want {
		t.Errorf("LatestReadyRevision do not match; got %v, want %v", got, want)
	}

	// wait for events to be created
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}
