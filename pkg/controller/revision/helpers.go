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
	"time"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// Checks whether the Revision knows whether the build is done.
func isBuildDone(rev *v1alpha1.Revision) (done, failed bool) {
	if rev.Spec.BuildName == "" {
		done, failed = true, false
		return
	}
	cond := rev.Status.GetCondition(v1alpha1.RevisionConditionBuildSucceeded)
	if cond == nil {
		done, failed = false, false
		return
	}
	switch cond.Status {
	case corev1.ConditionTrue:
		done, failed = true, false
	case corev1.ConditionFalse:
		done, failed = true, true
	case corev1.ConditionUnknown:
		done, failed = false, false
	}
	return
}

// TODO(mattmoor): This should be a helper on Build (upstream)
func getBuildDoneCondition(build *buildv1alpha1.Build) *buildv1alpha1.BuildCondition {
	for _, cond := range build.Status.Conditions {
		if cond.Status == corev1.ConditionUnknown {
			continue
		}
		return &cond
	}
	return nil
}

func getIsServiceReady(e *corev1.Endpoints) bool {
	for _, es := range e.Subsets {
		if len(es.Addresses) > 0 {
			return true
		}
	}
	return false
}

func getRevisionLastTransitionTime(r *v1alpha1.Revision) time.Time {
	condCount := len(r.Status.Conditions)
	if condCount == 0 {
		return r.CreationTimestamp.Time
	}
	return r.Status.Conditions[condCount-1].LastTransitionTime.Time
}

func getDeploymentProgressCondition(deployment *appsv1.Deployment) *appsv1.DeploymentCondition {
	// as per https://kubernetes.io/docs/concepts/workloads/controllers/deployment
	for _, cond := range deployment.Status.Conditions {
		// Look for Deployment with status False
		if cond.Status != corev1.ConditionFalse {
			continue
		}
		// with Type Progressing and Reason Timeout
		// TODO(arvtiwar): hard coding "ProgressDeadlineExceeded" to avoid import kubernetes/kubernetes
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return &cond
		}
	}
	return nil
}
