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

// PodAccessor provides access to various dimensions of pods listing
// and querying for a given bound revision.
type PodAccessor struct {
	podsLister corev1listers.PodNamespaceLister
	selector   labels.Selector
}

// NewPodAccessor creates a PodAccessor implementation that counts
// pods for a namespace/revision.
func NewPodAccessor(lister corev1listers.PodLister, namespace, revisionName string) PodAccessor {
	return PodAccessor{
		podsLister: lister.Pods(namespace),
		selector: labels.SelectorFromSet(labels.Set{
			serving.RevisionLabelKey: revisionName,
		}),
	}
}

// PendingTerminatingCount returns the number of pods in a Pending or
// Terminating state
func (pa PodAccessor) PendingTerminatingCount() (int, int, error) {
	_, _, p, t, err := pa.PodCountsByState()
	return p, t, err
}

// PodCountsByState returns number of pods for the revision grouped by their state, that is
// of interest to knative (e.g. ignoring failed or terminated pods).
func (pa PodAccessor) PodCountsByState() (ready, notReady, pending, terminating int, err error) {
	pods, err := pa.podsLister.List(pa.selector)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, p := range pods {
		switch p.Status.Phase {
		case corev1.PodPending:
			pending++
			notReady++
		case corev1.PodRunning:
			if p.DeletionTimestamp != nil {
				terminating++
				notReady++
				continue
			}
			isReady := false
			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodReady {
					if cond.Status == corev1.ConditionTrue {
						isReady = true
					}
					break
				}
			}
			if isReady {
				ready++
			} else {
				notReady++
			}
		}
	}
	return
}

// ReadyCount implements EndpointsCounter.
func (pa PodAccessor) ReadyCount() (int, error) {
	r, _, _, _, err := pa.PodCountsByState()
	return r, err
}

// NotReadyCount implements EndpointsCounter.
func (pa PodAccessor) NotReadyCount() (int, error) {
	_, nr, _, _, err := pa.PodCountsByState()
	return nr, err
}

// PodIPsByAge returns the list of running pod (terminating
// and non-running are excluded) IP addresses, sorted descending by pod age.
func (pa PodAccessor) PodIPsByAge() ([]string, error) {
	pods, err := pa.podsLister.List(pa.selector)
	if err != nil {
		return nil, err
	}
	// Keep only running ones.
	write := 0
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
			pods[write] = p
			write++
		}
	}
	pods = pods[:write]

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
