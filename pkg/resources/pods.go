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
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/serving/pkg/apis/serving"
)

// PodAccessor interface provides access to various dimensions of pods listing
// and querying for a given bound revision.
type PodAccessor interface {
	// Returns number of pods in pending and terminating state
	// or an error.
	PendingTerminatingCount() (int, int, error)
	// PodIPsByAge returns list of pod IPs sorted by pod age.
	PodIPsByAge() ([]string, error)
}

// podAccessor provides a count of pods currently not in the
// RUNNING state. The interface exempts users from needing to
// know how counts are performed.
type podAccessor struct {
	podsLister corev1listers.PodNamespaceLister
	selector   labels.Selector
}

// NewPodAccessor creates a PodAccessor implementation that counts
// pods for a namespace/revision.
func NewPodAccessor(lister corev1listers.PodLister, namespace, revisionName string) PodAccessor {
	return podAccessor{
		podsLister: lister.Pods(namespace),
		selector: labels.SelectorFromSet(labels.Set{
			serving.RevisionLabelKey: revisionName,
		}),
	}
}

// PendingTerminatingCount returns the number of pods in a Pending or
// Terminating state
func (pc podAccessor) PendingTerminatingCount() (int, int, error) {
	pods, err := pc.podsLister.List(pc.selector)
	if err != nil {
		return 0, 0, err
	}

	return pendingTerminatingCount(pods)
}

// no error is returned, but is here for code nicety.
func pendingTerminatingCount(pods []*corev1.Pod) (int, int, error) {
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
	return pending, terminating, nil
}

func (pc podAccessor) PodIPsByAge() ([]string, error) {
	pods, err := pc.podsLister.List(pc.selector)
	if err != nil {
		return nil, err
	}
	if len(pods) > 1 {
		// This results in a few reflection calls, which we can easily avoid.
		sort.SliceStable(pods, func(i, j int) bool {
			return pods[i].Status.StartTime.Before(pods[j].Status.StartTime)
		})
	}
	ret := make([]string, 0, len(pods))
	for _, p := range pods {
		ret = append(ret, p.Status.PodIP)
	}

	return ret, nil
}
