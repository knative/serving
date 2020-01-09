/*
Copyright 2020 The Knative Authors

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

package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/serving/pkg/apis/serving"
)

// NotRunningPodCounter provides a count of pods currently not in the
// RUNNING state. The interface exempts users from needing to
// know how counts are performed.
type notRunningPodCounter struct {
	podsLister   corev1listers.PodLister
	namespace    string
	revisionName string
}

// NewNotRunningPodsCounter creates a NotRunningPodCounter that counts
// pods for a namespace/serviceNam. The values returned by
// TerminatingCount() and PendingCount() will vary over time.
func NewNotRunningPodsCounter(lister corev1listers.PodLister, namespace, revisionName string) notRunningPodCounter {
	return notRunningPodCounter{
		podsLister:   lister,
		namespace:    namespace,
		revisionName: revisionName,
	}
}

// PendingTerminatingCount returns the number of pods in a Pending or
// Terminating state
func (pc *notRunningPodCounter) PendingTerminatingCount() (int, int, error) {
	pods, err := pc.podsLister.Pods(pc.namespace).List(labels.SelectorFromSet(labels.Set{
		serving.RevisionLabelKey: pc.revisionName,
	}))
	if err != nil {
		return 0, 0, err
	}

	pending, terminating := pendingTerminatingCount(pods)
	return pending, terminating, nil
}

func pendingTerminatingCount(pods []*corev1.Pod) (int, int) {
	pending, terminating := 0, 0
	for _, pod := range pods {
		switch pod.Status.Phase {
		case corev1.PodRunning:
			if pod.ObjectMeta.DeletionTimestamp != nil {
				terminating++
			}
		case corev1.PodPending:
			pending++
		}
	}
	return pending, terminating
}
