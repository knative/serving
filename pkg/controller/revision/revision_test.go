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

/* TODO tests:
- When a Revision is updated TODO
- When a Revision is deleted TODO
*/
import (
	"fmt"
	"testing"
	"time"

	buildv1alpha1 "github.com/elafros/elafros/pkg/apis/build/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"
	ctrl "github.com/elafros/elafros/pkg/controller"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	. "github.com/elafros/elafros/pkg/controller/testing"
)

const testNamespace string = "test"

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: testNamespace,
			Labels: map[string]string{
				"route": "test-route",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			// corev1.Container has a lot of setting.  We try to pass many
			// of them here to verify that we pass through the settings to
			// derived objects.
			Container: &corev1.Container{
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
	}
}

func getTestReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", revName),
			Namespace: testNamespace,
			Labels: map[string]string{
				"revision": revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "123.456.78.90",
					},
				},
			},
		},
	}
}

func getTestAuxiliaryReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-auxiliary", revName),
			Namespace: "test",
			Labels: map[string]string{
				"revision": revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{
					corev1.EndpointAddress{
						IP: "123.456.78.90",
					},
				},
			},
		},
	}
}

func getTestNotReadyEndpoints(revName string) *corev1.Endpoints {
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-service", revName),
			Namespace: "test",
			Labels: map[string]string{
				"revision": revName,
			},
		},
		Subsets: []corev1.EndpointSubset{
			corev1.EndpointSubset{
				Addresses: []corev1.EndpointAddress{},
			},
		},
	}
}

func newTestController(t *testing.T, elaObjects ...runtime.Object) (
	kubeClient *fakekubeclientset.Clientset,
	elaClient *fakeclientset.Clientset,
	controller *Controller,
	kubeInformer kubeinformers.SharedInformerFactory,
	elaInformer informers.SharedInformerFactory) {

	// Create fake clients
	kubeClient = fakekubeclientset.NewSimpleClientset()
	elaClient = fakeclientset.NewSimpleClientset(elaObjects...)

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
		ctrl.Config{},
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

	kubeClient, elaClient, controller, kubeInformer, elaInformer = newTestController(t, elaObjects...)

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

func TestCreateRevCreatesStuff(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// This function is used to verify pass through of container environment
	// variables.
	checkEnv := func(env []corev1.EnvVar, name, value, fieldPath string) {
		nameFound := false
		for _, e := range env {
			if e.Name == name {
				nameFound = true
				if value != "" && e.Value != value {
					t.Errorf("Incorrect environment variable %s. Expected value %s. Got %s.", name, value, e.Value)
				}
				if fieldPath != "" {
					if vf := e.ValueFrom; vf == nil {
						t.Errorf("Incorrect environment variable %s. Missing value source.", name)
					} else if fr := vf.FieldRef; fr == nil {
						t.Errorf("Incorrect environment variable %s. Missing field ref.", name)
					} else if fr.FieldPath != fieldPath {
						t.Errorf("Incorrect environment variable %s. Expected field path %s. Got %s.",
							name, fr.FieldPath, fieldPath)
					}
				}
			}
		}
		if !nameFound {
			t.Errorf("Missing environment variable %s", name)
		}
	}

	// Look for the revision deployment.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	deployment, err := kubeClient.ExtensionsV1beta1().Deployments(testNamespace).Get(expectedDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get ela deployment: %v", err)
	}

	if len(deployment.OwnerReferences) != 1 && rev.Name != deployment.OwnerReferences[0].Name {
		t.Errorf("expected owner references to have 1 ref with name %s", rev.Name)
	}

	// Check the revision deployment queue proxy environment variables
	foundQueueProxy := false
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "queue-proxy" {
			foundQueueProxy = true
			checkEnv(container.Env, "ELA_NAMESPACE", testNamespace, "")
			checkEnv(container.Env, "ELA_REVISION", "test-rev", "")
			checkEnv(container.Env, "ELA_POD", "", "metadata.name")
			break
		}
	}
	if !foundQueueProxy {
		t.Error("Missing queue-proxy container")
	}

	expectedRouteLabel := rev.Labels["route"]
	if routeLabel := deployment.ObjectMeta.Labels["route"]; routeLabel != expectedRouteLabel {
		t.Errorf("Route label not set correctly on deployment: expected %s got %s.",
			expectedRouteLabel, routeLabel)
	}
	if routeLabel := deployment.Spec.Template.ObjectMeta.Labels["route"]; routeLabel != expectedRouteLabel {
		t.Errorf("Route label not set correctly in pod template: expected %s got %s.",
			expectedRouteLabel, routeLabel)
	}

	// Look for the revision service.
	expectedServiceName := fmt.Sprintf("%s-service", rev.Name)
	service, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedServiceName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler service: %v", err)
	}
	// The revision service should be owned by rev.
	expectedRefs := []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: "elafros.dev/v1alpha1",
			Kind:       "Revision",
			Name:       rev.Name,
		},
	}
	if diff := cmp.Diff(expectedRefs, service.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}

	// Look for the autoscaler deployment.
	expectedAutoscalerName := fmt.Sprintf("%s-autoscaler", rev.Name)
	asDeployment, err := kubeClient.ExtensionsV1beta1().Deployments(testNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler deployment: %v", err)
	}
	// Check the autoscaler deployment environment variables
	foundAutoscaler := false
	for _, container := range asDeployment.Spec.Template.Spec.Containers {
		if container.Name == "autoscaler" {
			foundAutoscaler = true
			checkEnv(container.Env, "ELA_NAMESPACE", testNamespace, "")
			checkEnv(container.Env, "ELA_DEPLOYMENT", expectedDeploymentName, "")
			break
		}
	}
	if !foundAutoscaler {
		t.Error("Missing autoscaler")
	}

	// Look for the autoscaler service.
	asService, err := kubeClient.CoreV1().Services(testNamespace).Get(expectedAutoscalerName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get autoscaler service: %v", err)
	}
	// The autoscaler service should also be owned by rev.
	if diff := cmp.Diff(expectedRefs, asService.OwnerReferences, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}

	rev, err = elaClient.ElafrosV1alpha1().Revisions(testNamespace).Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionFalse,
			Reason: "Deploying",
		},
	}
	if diff := cmp.Diff(want, rev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

}

func TestCreateRevWithBuildNameWaits(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "foo",
		},
		Spec: buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Name:    "nop",
				Image:   "busybox:latest",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "echo Hello"},
			}},
		},
	}
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	waitRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// Ensure that the Revision status is updated.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "BuildComplete",
			Status: corev1.ConditionFalse,
			Reason: "Building",
		},
	}
	if diff := cmp.Diff(want, waitRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}
}

func TestCreateRevWithFailedBuildNameFails(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	reason := "Foo"
	errMessage := "a long human-readable error message."

	h := NewHooks()
	// Look for the build failure event. Events are delivered asynchronously so
	// we need to use hooks here.
	h.OnCreate(&kubeClient.Fake, "events", ExpectWarningEventDelivery(t, errMessage))

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "foo",
		},
		Spec: buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Name:    "nop",
				Image:   "busybox:latest",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "echo Hello"},
			}},
		},
	}
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete, but
	// with a failure.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildFailed,
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	failedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The next update we receive should tell us that the build failed,
	// and surface the reason and message from that failure in our own
	// status.
	want := []v1alpha1.RevisionCondition{
		{
			Type:    "BuildFailed",
			Status:  corev1.ConditionTrue,
			Reason:  reason,
			Message: errMessage,
		},
	}
	if diff := cmp.Diff(want, failedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithCompletedBuildNameCompletes(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	h := NewHooks()
	// Look for the build complete event. Events are delivered asynchronously so
	// we need to use hooks here.
	h.OnCreate(&kubeClient.Fake, "events", func(obj runtime.Object) HookResult {
		event := obj.(*corev1.Event)
		if wanted, got := "BuildComplete", event.Reason; wanted != got {
			t.Errorf("unexpected event reason: %q expected: %q", got, wanted)
		}
		if wanted, got := corev1.EventTypeNormal, event.Type; wanted != got {
			t.Errorf("unexpected event Type: %q expected: %q", got, wanted)
		}
		return HookComplete
	})

	completeMessage := "a long human-readable complete message."

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "foo",
		},
		Spec: buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Name:    "nop",
				Image:   "busybox:latest",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "echo Hello"},
			}},
		},
	}
	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildComplete,
				Status:  corev1.ConditionTrue,
				Message: completeMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	completedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The next update we receive should tell us that the build completed.
	want := []v1alpha1.RevisionCondition{
		{
			Type:   "BuildComplete",
			Status: corev1.ConditionTrue,
		},
	}
	if diff := cmp.Diff(want, completedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithInvalidBuildNameFails(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)

	reason := "Foo"
	errMessage := "a long human-readable error message."

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "foo",
		},
		Spec: buildv1alpha1.BuildSpec{
			Steps: []corev1.Container{{
				Name:    "nop",
				Image:   "busybox:latest",
				Command: []string{"/bin/sh"},
				Args:    []string{"-c", "echo Hello"},
			}},
		},
	}

	elaClient.BuildV1alpha1().Builds(testNamespace).Create(bld)

	rev := getTestRevision()
	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))

	// After the initial update to the revision, we should be
	// watching for this build to complete, so make it complete, but
	// with a validation failure.
	bld.Status = buildv1alpha1.BuildStatus{
		Conditions: []buildv1alpha1.BuildCondition{
			{
				Type:    buildv1alpha1.BuildInvalid,
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			},
		},
	}

	controller.addBuildEvent(bld)

	failedRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	want := []v1alpha1.RevisionCondition{
		{
			Type:    "BuildFailed",
			Status:  corev1.ConditionTrue,
			Reason:  reason,
			Message: errMessage,
		},
	}
	if diff := cmp.Diff(want, failedRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

}

func TestMarkRevReadyUponEndpointBecomesReady(t *testing.T) {
	kubeClient, elaClient, controller, _, elaInformer := newTestController(t)
	revClient := elaClient.ElafrosV1alpha1().Revisions(testNamespace)
	rev := getTestRevision()

	h := NewHooks()
	// Look for the revision ready event. Events are delivered asynchronously so
	// we need to use hooks here.
	expectedMessage := "Revision becomes ready upon endpoint \"test-rev-service\" becoming ready"
	h.OnCreate(&kubeClient.Fake, "events", ExpectNormalEventDelivery(t, expectedMessage))

	revClient.Create(rev)
	// Since syncHandler looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)
	controller.syncHandler(KeyOrDie(rev))

	deployingRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// The revision is not marked ready until an endpoint is created.
	deployingConditions := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionFalse,
			Reason: "Deploying",
		},
	}
	if diff := cmp.Diff(deployingConditions, deployingRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	endpoints := getTestReadyEndpoints(rev.Name)
	controller.addEndpointsEvent(endpoints)

	readyRev, err := revClient.Get(rev.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Couldn't get revision: %v", err)
	}

	// After reconciling the endpoint, the revision should be ready.
	readyConditions := []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		},
	}
	if diff := cmp.Diff(readyConditions, readyRev.Status.Conditions); diff != "" {
		t.Errorf("Unexpected revision conditions diff (-want +got): %v", diff)
	}

	// Wait for events to be delivered.
	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestDoNotUpdateRevIfRevIsAlreadyReady(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	// Mark the revision already ready.
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		{
			Type:   "Ready",
			Status: corev1.ConditionTrue,
			Reason: "ServiceReady",
		},
	}

	elaClient.ElafrosV1alpha1().Revisions(testNamespace).Create(rev)
	// Since addEndpointsEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}

func TestDoNotUpdateRevIfRevIsMarkedAsFailed(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()
	// Mark the revision already ready.
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		v1alpha1.RevisionCondition{
			Type:   "Failed",
			Status: corev1.ConditionTrue,
			Reason: "ExceededReadinessChecks",
		},
	}

	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	// Since addEndpointsEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}

func TestMarkRevAsFailedIfEndpointHasNoAddressesAfterSomeDuration(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	creationTime := time.Now().Add(-10 * time.Minute)
	rev.ObjectMeta.CreationTimestamp = metav1.NewTime(creationTime)
	rev.Status.Conditions = []v1alpha1.RevisionCondition{
		v1alpha1.RevisionCondition{
			Type:   "Ready",
			Status: corev1.ConditionFalse,
			Reason: "Deploying",
		},
	}

	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	// Since addEndpointsEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestNotReadyEndpoints(rev.Name)

	controller.addEndpointsEvent(endpoints)

	currentRev, _ := elaClient.ElafrosV1alpha1().Revisions("test").Get(rev.Name, metav1.GetOptions{})

	want := v1alpha1.RevisionCondition{
		Type:    "Failed",
		Status:  corev1.ConditionTrue,
		Reason:  "ServiceTimeout",
		Message: "Timed out waiting for a service endpoint to become ready",
	}

	if len(currentRev.Status.Conditions) != 1 || want != currentRev.Status.Conditions[0] {
		t.Errorf("expected conditions to have 1 condition equal to %v", want)
	}
}

func TestAuxiliaryEndpointDoesNotUpdateRev(t *testing.T) {
	_, elaClient, controller, _, elaInformer := newTestController(t)
	rev := getTestRevision()

	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)
	// Since addEndpointsEvent looks in the lister, we need to add it to the informer
	elaInformer.Elafros().V1alpha1().Revisions().Informer().GetIndexer().Add(rev)

	// Create endpoints owned by this Revision.
	endpoints := getTestAuxiliaryReadyEndpoints(rev.Name)

	// No revision updates.
	elaClient.Fake.PrependReactor("update", "revisions",
		func(a kubetesting.Action) (bool, runtime.Object, error) {
			t.Error("Revision was updated unexpectedly")
			return true, nil, nil
		},
	)

	controller.addEndpointsEvent(endpoints)
}
