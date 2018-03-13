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

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	buildv1alpha1 "github.com/elafros/elafros/pkg/apis/cloudbuild/v1alpha1"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	fakeclientset "github.com/elafros/elafros/pkg/client/clientset/versioned/fake"
	informers "github.com/elafros/elafros/pkg/client/informers/externalversions"

	hooks "github.com/elafros/elafros/pkg/controller/testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"k8s.io/api/extensions/v1beta1"
	kubeinformers "k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"
)

func getTestRevision() *v1alpha1.Revision {
	return &v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			SelfLink:  "/apis/ela/v1alpha1/namespaces/test/revisions/test-rev",
			Name:      "test-rev",
			Namespace: "test",
			Labels: map[string]string{
				"route": "test-route",
			},
		},
		Spec: v1alpha1.RevisionSpec{
			// corev1.Container has a lot of setting.  We try to pass many
			// of them here to verify that we pass through the settings to
			// derived objects.
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

func TestCreateRevCreatesStuff(t *testing.T) {
	kubeClient, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rev := getTestRevision()
	h := hooks.NewHooks()

	// Look for the namespace.
	expectedNamespace := rev.Namespace
	expectedRouteLabel := rev.Labels["route"]
	h.OnCreate(&kubeClient.Fake, "namespaces", func(obj runtime.Object) hooks.HookResult {
		ns := obj.(*corev1.Namespace)
		glog.Infof("checking namespace %s", ns.Name)
		if expectedNamespace != ns.Name {
			t.Errorf("namespace was not named %s", expectedNamespace)
		}
		return hooks.HookComplete
	})

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

	// Look for the ela and autoscaler deployments.
	expectedDeploymentName := fmt.Sprintf("%s-deployment", rev.Name)
	expectedAutoscalerName := fmt.Sprintf("%s-autoscaler", rev.Name)
	h.OnCreate(&kubeClient.Fake, "deployments", func(obj runtime.Object) hooks.HookResult {
		d := obj.(*v1beta1.Deployment)
		glog.Infof("checking d %s", d.Name)
		if expectedNamespace != d.Namespace {
			t.Errorf("Deployment namespace was not %s. Got %s.", expectedNamespace, d.Namespace)
		}
		if len(d.OwnerReferences) != 1 && rev.Name != d.OwnerReferences[0].Name {
			t.Errorf("expected owner references to have 1 ref with name %s", rev.Name)
		}
		if d.Name == expectedDeploymentName {
			// Check the ela deployment queue proxy environment variables
			foundQueueProxy := false
			for _, container := range d.Spec.Template.Spec.Containers {
				if container.Name == "queue-proxy" {
					foundQueueProxy = true
					checkEnv(container.Env, "ELA_NAMESPACE", "test", "")
					checkEnv(container.Env, "ELA_REVISION", "test-rev", "")
					checkEnv(container.Env, "ELA_POD", "", "metadata.name")
				}
			}
			if !foundQueueProxy {
				t.Error("Missing queue-proxy")
			}
			if routeLabel := d.ObjectMeta.Labels["route"]; routeLabel != expectedRouteLabel {
				t.Errorf("Route label not set correctly on deployment: expected %s got %s.",
					expectedRouteLabel, routeLabel)
			}
			if routeLabel := d.Spec.Template.ObjectMeta.Labels["route"]; routeLabel != expectedRouteLabel {
				t.Errorf("Route label not set correctly in pod template: expected %s got %s.",
					expectedRouteLabel, routeLabel)
			}
		} else if d.Name == expectedAutoscalerName {
			// Check the autoscaler deployment environment variables
			foundAutoscaler := false
			for _, container := range d.Spec.Template.Spec.Containers {
				if container.Name == "autoscaler" {
					foundAutoscaler = true
					checkEnv(container.Env, "ELA_NAMESPACE", "test", "")
					checkEnv(container.Env, "ELA_DEPLOYMENT", expectedDeploymentName, "")
				}
			}
			if !foundAutoscaler {
				t.Error("Missing autoscaler")
			}
		} else {
			t.Errorf("Deployment was not named %s or %s. Got %s.", expectedDeploymentName, expectedAutoscalerName, d.Name)
		}
		return hooks.HookComplete
	})

	// Ensure that the Revision status is updated.
	h.OnUpdate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		updatedPr := obj.(*v1alpha1.Revision)
		glog.Infof("updated rev %v", updatedPr)
		want := v1alpha1.RevisionCondition{
			Type:   "Ready",
			Status: corev1.ConditionFalse,
			Reason: "Deploying",
		}
		if len(updatedPr.Status.Conditions) != 1 || want != updatedPr.Status.Conditions[0] {
			t.Errorf("expected conditions to have 1 condition equal to %v", want)
		}
		return hooks.HookComplete
	})

	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithBuildNameWaits(t *testing.T) {
	_, elaClient, _, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rev := getTestRevision()
	h := hooks.NewHooks()

	// Ensure that the Revision status is updated.
	h.OnUpdate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		updatedPr := obj.(*v1alpha1.Revision)
		glog.Infof("updated rev %v", updatedPr)
		want := v1alpha1.RevisionCondition{
			Type:   "BuildComplete",
			Status: corev1.ConditionFalse,
			Reason: "Building",
		}
		if len(updatedPr.Status.Conditions) != 1 || want != updatedPr.Status.Conditions[0] {
			t.Errorf("expected conditions to have 1 condition equal to %v", want)
		}
		return hooks.HookComplete
	})

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
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

	elaClient.BuildV1alpha1().Builds("test").Create(bld)

	// Direct the Revision to wait for this build to complete.
	rev.Spec.BuildName = bld.Name
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(time.Second * 3); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithFailedBuildNameFails(t *testing.T) {
	_, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rev := getTestRevision()
	h := hooks.NewHooks()

	reason := "Foo"
	errMessage := "a long human-readable error message."

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
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

	// Ensure that the Revision status is updated.
	update := 0
	h.OnUpdate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		updatedPr := obj.(*v1alpha1.Revision)
		update = update + 1
		switch update {
		case 1:
			// After the initial update to the revision, we should be
			// watching for this build to complete, so make it complete, but
			// with a failure.
			bld.Status = buildv1alpha1.BuildStatus{
				Conditions: []buildv1alpha1.BuildCondition{{
					Type:    buildv1alpha1.BuildFailed,
					Status:  corev1.ConditionTrue,
					Reason:  reason,
					Message: errMessage,
				}},
			}

			// This hangs for some reason:
			// elaClient.BuildV1alpha1().Builds("test").Update(bld)
			// so manually trigger the build event.
			// Launch this in a goroutine because the OnUpdate logic works a little too
			// synchronously and this leads to lock re-entrancy.
			go controller.addBuildEvent(bld)
			return hooks.HookIncomplete

		case 2:
			// The next update we receive should tell us that the build failed,
			// and surface the reason and message from that failure in our own
			// status.
			want := v1alpha1.RevisionCondition{
				Type:    "BuildFailed",
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			}
			if len(updatedPr.Status.Conditions) != 1 {
				t.Errorf("want 1 condition, got %d", len(updatedPr.Status.Conditions))
			}
			if want != updatedPr.Status.Conditions[0] {
				t.Errorf("wanted %v, got %v", want, updatedPr.Status.Conditions[0])
			}
		}
		return hooks.HookComplete
	})

	// Direct the Revision to wait for this build to complete.
	elaClient.BuildV1alpha1().Builds("test").Create(bld)
	rev.Spec.BuildName = bld.Name
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithCompletedBuildNameFails(t *testing.T) {
	_, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rev := getTestRevision()
	h := hooks.NewHooks()

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
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

	// Ensure that the Revision status is updated.\
	update := 0
	h.OnUpdate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		updatedPr := obj.(*v1alpha1.Revision)
		update = update + 1
		switch update {
		case 1:
			// After the initial update to the revision, we should be
			// watching for this build to complete, so make it complete.
			bld.Status = buildv1alpha1.BuildStatus{
				Conditions: []buildv1alpha1.BuildCondition{{
					Type:   buildv1alpha1.BuildComplete,
					Status: corev1.ConditionTrue,
				}},
			}

			// This hangs for some reason:
			// elaClient.BuildV1alpha1().Builds("test").Update(bld)
			// so manually trigger the build event.
			// Launch this in a goroutine because the OnUpdate logic works a little too
			// synchronously and this leads to lock re-entrancy.
			go controller.addBuildEvent(bld)
			return hooks.HookIncomplete

		case 2:
			// The next update we receive should tell us that the build completed.
			want := v1alpha1.RevisionCondition{
				Type:   "BuildComplete",
				Status: corev1.ConditionTrue,
			}
			if len(updatedPr.Status.Conditions) != 1 {
				t.Errorf("want 1 condition, got %d", len(updatedPr.Status.Conditions))
			}
			if want != updatedPr.Status.Conditions[0] {
				t.Errorf("wanted %v, got %v", want, updatedPr.Status.Conditions[0])
			}
		}
		return hooks.HookComplete
	})

	// Direct the Revision to wait for this build to complete.
	elaClient.BuildV1alpha1().Builds("test").Create(bld)
	rev.Spec.BuildName = bld.Name
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}

func TestCreateRevWithInvalidBuildNameFails(t *testing.T) {
	_, elaClient, controller, _, _, stopCh := newRunningTestController(t)
	defer close(stopCh)
	rev := getTestRevision()
	h := hooks.NewHooks()

	reason := "Foo"
	errMessage := "a long human-readable error message."

	bld := &buildv1alpha1.Build{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
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

	// Ensure that the Revision status is updated.\
	update := 0
	h.OnUpdate(&elaClient.Fake, "revisions", func(obj runtime.Object) hooks.HookResult {
		updatedPr := obj.(*v1alpha1.Revision)
		update = update + 1
		switch update {
		case 1:
			// After the initial update to the revision, we should be
			// watching for this build to complete, so make it complete, but
			// with a validation failure.
			bld.Status = buildv1alpha1.BuildStatus{
				Conditions: []buildv1alpha1.BuildCondition{{
					Type:    buildv1alpha1.BuildInvalid,
					Status:  corev1.ConditionTrue,
					Reason:  reason,
					Message: errMessage,
				}},
			}

			// This hangs for some reason:
			// elaClient.BuildV1alpha1().Builds("test").Update(bld)
			// so manually trigger the build event.
			// Launch this in a goroutine because the OnUpdate logic works a little too
			// synchronously and this leads to lock re-entrancy.
			go controller.addBuildEvent(bld)
			return hooks.HookIncomplete

		case 2:
			// The next update we receive should tell us that the build failed,
			// and surface the reason and message from that failure in our own
			// status.
			want := v1alpha1.RevisionCondition{
				Type:    "BuildFailed",
				Status:  corev1.ConditionTrue,
				Reason:  reason,
				Message: errMessage,
			}
			if len(updatedPr.Status.Conditions) != 1 {
				t.Errorf("want 1 condition, got %d", len(updatedPr.Status.Conditions))
			}
			if want != updatedPr.Status.Conditions[0] {
				t.Errorf("wanted %v, got %v", want, updatedPr.Status.Conditions[0])
			}
		}
		return hooks.HookComplete
	})

	// Direct the Revision to wait for this build to complete.
	elaClient.BuildV1alpha1().Builds("test").Create(bld)
	rev.Spec.BuildName = bld.Name
	elaClient.ElafrosV1alpha1().Revisions("test").Create(rev)

	if err := h.WaitForHooks(3 * time.Second); err != nil {
		t.Error(err)
	}
}
