// +build e2e

/*
Copyright 2019 The Knative Authors
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

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pipelinev1alpha1 "github.com/knative/build-pipeline/pkg/apis/pipeline/v1alpha1"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
)

func TestPipeline(t *testing.T) {
	t.Parallel()
	pipelineName := test.ObjectNameForTest(t)

	testCases := []struct {
		name         string
		rawExtension *v1alpha1.RawExtension
		preFn        func(*testing.T, *test.Clients)
		validateFn   func(*testing.T, string, *test.Clients)
	}{{
		name: "task run",
		rawExtension: &v1alpha1.RawExtension{
			Object: &pipelinev1alpha1.TaskRun{
				TypeMeta: metav1.TypeMeta{
					APIVersion: pipelinev1alpha1.SchemeGroupVersion.String(),
					Kind:       "TaskRun",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: test.ServingNamespace,
				},
				Spec: pipelinev1alpha1.TaskRunSpec{
					Trigger: pipelinev1alpha1.TaskTrigger{
						Type: pipelinev1alpha1.TaskTriggerTypeManual,
					},
					TaskSpec: &pipelinev1alpha1.TaskSpec{
						Steps: []corev1.Container{{
							Name:  "foo",
							Image: "busybox",
							Args:  []string{"echo", "hellow"},
						}},
					},
				},
			},
		},
		validateFn: func(t *testing.T, buildName string, clients *test.Clients) {
			t.Logf("Revision's Build is taskrun %q", buildName)
			b, err := clients.PipelineClient.TaskRun.Get(buildName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get build for latest revision: %v", err)
			}
			if cond := b.Status.GetCondition(duckv1alpha1.ConditionSucceeded); cond == nil {
				t.Fatalf("Condition for build %q was nil", buildName)
			} else if cond.Status != corev1.ConditionTrue {
				t.Fatalf("Build %q was not successful", buildName)
			}
		},
	}, {
		name: "pipeline run",
		rawExtension: &v1alpha1.RawExtension{
			Object: &pipelinev1alpha1.PipelineRun{
				TypeMeta: metav1.TypeMeta{
					APIVersion: pipelinev1alpha1.SchemeGroupVersion.String(),
					Kind:       "PipelineRun",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: test.ServingNamespace,
				},
				Spec: pipelinev1alpha1.PipelineRunSpec{
					Trigger: pipelinev1alpha1.PipelineTrigger{
						Type: pipelinev1alpha1.PipelineTriggerTypeManual,
					},
					PipelineRef: pipelinev1alpha1.PipelineRef{
						Name: pipelineName,
					},
				},
			},
		},
		preFn: func(t *testing.T, clients *test.Clients) {
			t.Log("Creating Pipeline and Task for the build with PipelineRun")
			taskName := test.ObjectNameForTest(t)

			if _, err := clients.PipelineClient.Task.Create(&pipelinev1alpha1.Task{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: test.ServingNamespace,
					Name:      taskName,
				},
				Spec: pipelinev1alpha1.TaskSpec{
					Steps: []corev1.Container{{
						Name:  "foo",
						Image: "busybox",
						Args:  []string{"echo", "hellow"},
					}},
				},
			}); err != nil {
				t.Fatalf("Failed to create Task: %v", err)
			}
			if _, err := clients.PipelineClient.Pipeline.Create(&pipelinev1alpha1.Pipeline{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: test.ServingNamespace,
					Name:      pipelineName,
				},
				Spec: pipelinev1alpha1.PipelineSpec{
					Tasks: []pipelinev1alpha1.PipelineTask{{
						Name: "test-pipe-test-task",
						TaskRef: pipelinev1alpha1.TaskRef{
							Name: taskName,
						},
					}},
				},
			}); err != nil {
				t.Fatalf("Failed to create Pipeline: %v", err)
			}
		},
		validateFn: func(t *testing.T, buildName string, clients *test.Clients) {
			t.Logf("Revision's Build is pipelinerun %q", buildName)
			b, err := clients.PipelineClient.PipelineRun.Get(buildName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get build for latest revision: %v", err)
			}
			if cond := b.Status.GetCondition(duckv1alpha1.ConditionSucceeded); cond == nil {
				t.Fatalf("Condition for build %q was nil", buildName)
			} else if cond.Status != corev1.ConditionTrue {
				t.Fatalf("Build %q was not successful", buildName)
			}
		},
	}}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clients := Setup(t)

			t.Log("Creating a new Route and Configuration with build")
			svcName := test.ObjectNameForTest(t)
			names := test.ResourceNames{
				Config: svcName,
				Route:  svcName,
				Image:  "helloworld",
			}

			if tc.preFn != nil {
				tc.preFn(t, clients)
			}

			test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
			defer test.TearDown(clients, names)

			if _, err := clients.ServingClient.Configs.Create(test.ConfigurationWithBuild(test.ServingNamespace, names, tc.rawExtension)); err != nil {
				t.Fatalf("Failed to create Configuration: %v", err)
			}
			if _, err := clients.ServingClient.Routes.Create(test.Route(test.ServingNamespace, names)); err != nil {
				t.Fatalf("Failed to create Route: %v", err)
			}

			t.Log("When the Revision can have traffic routed to it, the Route is marked as Ready.")
			if err := test.WaitForRouteState(clients.ServingClient, names.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
				t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", names.Route, err)
			}

			route, err := clients.ServingClient.Routes.Get(names.Route, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Error fetching Route %s: %v", names.Route, err)
			}
			domain := route.Status.Domain

			endState := pkgTest.Retrying(pkgTest.MatchesBody(helloWorldExpectedOutput), http.StatusNotFound)
			if _, err := pkgTest.WaitForEndpointState(clients.KubeClient, t.Logf, domain, endState, "HelloWorldServesText", test.ServingFlags.ResolvableDomain); err != nil {
				t.Fatalf("The endpoint for Route %s at domain %s didn't serve the expected text \"%s\": %v", names.Route, domain, helloWorldExpectedOutput, err)
			}

			// Get Configuration's latest ready Revision's Build, and check that the Build was successful.
			t.Log("Revision is ready and serving, checking Build status.")
			config, err := clients.ServingClient.Configs.Get(names.Config, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get Configuration after it was seen to be live: %v", err)
			}
			rev, err := clients.ServingClient.Revisions.Get(config.Status.LatestReadyRevisionName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get latest Revision: %v", err)
			}
			names.Revision = rev.Name
			if tc.validateFn != nil {
				t.Logf("Latest ready Revision is %q", rev.Name)
				buildName := rev.Spec.BuildRef.Name
				tc.validateFn(t, buildName, clients)
			}

			// Update the Configuration with an environment variable, which should trigger a new revision
			// to be created, but without creating a new build.
			if err := updateConfigWithEnvVars(clients, names, []corev1.EnvVar{{
				Name:  "FOO",
				Value: "bar",
			}}); err != nil {
				t.Fatalf("Failed to update config with environment variables: %v", err)
			}

			nextRevName, err := getNextRevisionName(clients, names)
			if err != nil {
				t.Fatalf("Error waiting for next revision to be created: %v", err)
			}
			names.Revision = nextRevName

			nextRev, err := clients.ServingClient.Revisions.Get(nextRevName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get latest Revision: %v", err)
			}

			if diff := cmp.Diff(rev.Spec.BuildRef, nextRev.Spec.BuildRef); diff != "" {
				t.Fatalf("Unexpected differences in BuildRef: %v", diff)
			}

			// Update the Configuration's Build with an annotation, which should trigger the creation
			// of BOTH a Build and a Revision.
			if err := updateConfigWithBuildAnnotation(clients, names, map[string]string{
				"testing.knative.dev/foo": "bar",
			}); err != nil {
				t.Fatalf("Failed to update config with environment variables: %v", err)
			}

			nextRevName, err = getNextRevisionName(clients, names)
			if err != nil {
				t.Fatalf("Error waiting for next revision to be created: %v", err)
			}
			names.Revision = nextRevName

			nextRev, err = clients.ServingClient.Revisions.Get(nextRevName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get latest Revision: %v", err)
			}

			if diff := cmp.Diff(rev.Spec.BuildRef, nextRev.Spec.BuildRef); diff == "" {
				t.Fatalf("Got matching BuildRef, wanted different: %#v", rev.Spec.BuildRef)
			}
		})
	}
}
