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
	"time"

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
			if podReady(p) {
				ready++
			} else {
				notReady++
			}
		}
	}

	return ready, notReady, pending, terminating, nil
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

// EffectiveCapacityCount implements EndpointsCounter.
// Returns ready pods + starting pods (running but not ready due to startup reasons),
// excluding pods that became unready (running but not ready due to failure reasons).
func (pa PodAccessor) EffectiveCapacityCount() (int, error) {
	pods, err := pa.podsLister.List(pa.selector)
	if err != nil {
		return 0, err
	}

	effectiveCapacity := 0
	for _, p := range pods {
		switch p.Status.Phase {
		case corev1.PodRunning:
			if p.DeletionTimestamp != nil {
				continue // Ignore terminating pods
			}
			if podReady(p) {
				effectiveCapacity++ // Ready pods count as capacity
			} else if podIsStarting(p) {
				effectiveCapacity++ // Starting pods count as capacity
			}
			// Pods that became unready (not starting) are excluded
		}
		// Pending pods are not included in effective capacity
		// as they're not yet running and consuming resources
	}

	return effectiveCapacity, nil
}

// podIsStarting determines if a running pod is in a starting state vs became unready
func podIsStarting(p *corev1.Pod) bool {
	// Look at the Ready condition to determine the reason for not being ready
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionFalse {
			// Reasons that indicate the pod is starting up (not failed)
			switch cond.Reason {
			case "ContainersNotReady":
				// Check if containers are still starting vs failed
				return containersAreStarting(p)
			case "PodReadinessGatesNotReady":
				// Pod readiness gates not satisfied, typically during startup
				return true
			case "":
				// No specific reason, likely still initializing
				return true
			default:
				// Other reasons typically indicate failure states
				return false
			}
		}
	}
	// If no Ready condition found or status is not False, default to not starting
	return false
}

// containersAreStarting checks if containers are starting vs failed
func containersAreStarting(p *corev1.Pod) bool {
	for _, containerStatus := range p.Status.ContainerStatuses {
		if !containerStatus.Ready {
			// If container is in waiting state, check the reason
			if containerStatus.State.Waiting != nil {
				reason := containerStatus.State.Waiting.Reason
				switch reason {
				case "ContainerCreating", "PodInitializing":
					// Genuine startup states
					return true
				case "ImagePullBackOff", "ErrImagePull":
					// Treat persistent image pull issues as failures (no capacity)
					return false
				case "CrashLoopBackOff", "Error", "CreateContainerConfigError", "InvalidImageName":
					// These indicate failure, not startup
					return false
				default:
					// Unknown waiting reason: do not count as starting
					return false
				}
			}
			// If container is running but not ready, it's likely a readiness probe failure
			if containerStatus.State.Running != nil {
				return false // Running but not ready suggests it became unready
			}
			// If container is terminated, it's definitely not starting
			if containerStatus.State.Terminated != nil {
				return false
			}
		}
	}
	// If all containers are ready, this shouldn't be called, but default to not starting
	return false
}

// PodFilter provides a way to filter pods for a revision.
// Returning true, means that pod should be kept.
type PodFilter func(p *corev1.Pod) bool

// PodTransformer provides a way to do something with the pod
// that has been selected by all the filters.
// For example pod transformer may extract a field and store it in
// internal state.
type PodTransformer func(p *corev1.Pod)

// ProcessPods filters all the pods using provided pod filters and then if the pod
// is selected, applies the transformer to it.
func (pa PodAccessor) ProcessPods(pt PodTransformer, pfs ...PodFilter) error {
	pods, err := pa.podsLister.List(pa.selector)
	if err != nil {
		return err
	}
	for _, p := range pods {
		if applyFilters(p, pfs...) {
			pt(p)
		}
	}
	return nil
}

func applyFilters(p *corev1.Pod, pfs ...PodFilter) bool {
	for _, pf := range pfs {
		if !pf(p) {
			return false
		}
	}
	return true
}

func podRunning(p *corev1.Pod) bool {
	return p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil
}

// podReady checks whether pod's Ready status is True.
func podReady(p *corev1.Pod) bool {
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	// No ready status, probably not even running.
	return false
}

type podIPByAgeSorter struct {
	pods []*corev1.Pod
}

func (s *podIPByAgeSorter) process(p *corev1.Pod) {
	s.pods = append(s.pods, p)
}

func (s *podIPByAgeSorter) get() []string {
	if len(s.pods) > 1 {
		// This results in a few reflection calls, which we can easily avoid.
		sort.SliceStable(s.pods, func(i, j int) bool {
			return s.pods[i].Status.StartTime.Before(s.pods[j].Status.StartTime)
		})
	}
	ret := make([]string, 0, len(s.pods))
	for _, p := range s.pods {
		ret = append(ret, p.Status.PodIP)
	}
	return ret
}

// PodIPsByAge returns the list of running pods (terminating
// and non-running are excluded) IP addresses, sorted descending by pod age.
func (pa PodAccessor) PodIPsByAge() ([]string, error) {
	ps := podIPByAgeSorter{}
	if err := pa.ProcessPods(ps.process, podRunning, podReady); err != nil {
		return nil, err
	}
	return ps.get(), nil
}

type podIPWithCutoffProcessor struct {
	cutOff  time.Duration
	now     time.Time
	older   []string
	younger []string
}

func (pp *podIPWithCutoffProcessor) process(p *corev1.Pod) {
	// If pod is at least as old as cutoff.
	if pp.now.Sub(p.Status.StartTime.Time) >= pp.cutOff {
		pp.older = append(pp.older, p.Status.PodIP)
	} else {
		pp.younger = append(pp.younger, p.Status.PodIP)
	}
}

// PodIPsSplitByAge returns all the ready Pod IPs in two lists: older than cutoff and younger
// than cutoff.
func (pa PodAccessor) PodIPsSplitByAge(cutOff time.Duration, now time.Time) (older, younger []string, err error) {
	pp := podIPWithCutoffProcessor{
		now:    now,
		cutOff: cutOff,
	}
	if err := pa.ProcessPods(pp.process, podRunning, podReady); err != nil {
		return nil, nil, err
	}
	return pp.older, pp.younger, nil
}
