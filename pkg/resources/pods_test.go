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
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	"knative.dev/serving/pkg/apis/serving"
)

const testRevision = "test-revision"

func TestPodReadyUnreadyCount(t *testing.T) {
	tests := []struct {
		name string
		pods []*corev1.Pod
		want int
	}{{
		name: "no pods",
	}, {
		name: "one pod",
		pods: []*corev1.Pod{
			pod("ramble-on", makeReady),
		},
		want: 1,
	}, {
		name: "one pod no status",
		pods: []*corev1.Pod{
			pod("whole-lotta-love"),
		},
	}, {
		name: "one pod terminating",
		pods: []*corev1.Pod{
			pod("stairway-to-heaven", func(p *corev1.Pod) {
				n := metav1.Now()
				p.DeletionTimestamp = &n // Pod terminating[
			}),
		},
	}, {
		name: "two pods not ready in various ways",
		pods: []*corev1.Pod{
			pod("when-the-levee-breaks", func(p *corev1.Pod) {
				p.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				}}
			}),
			pod("black-dog", func(p *corev1.Pod) {
				p.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionUnknown,
				}}
			}),
		},
	}, {
		name: "two pods ready",
		pods: []*corev1.Pod{
			pod("dazed-and-confused", makeReady),
			pod("good-times-bad-times", makeReady),
		},
		want: 2,
	}, {
		name: "mix and match",
		pods: []*corev1.Pod{
			pod("immigrant-song", makeReady),
			pod("since-ive-been-lovin-you", func(p *corev1.Pod) {
				p.Status.Conditions = []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				}}
			}),
		},
		want: 1,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			kubeClient := fakek8s.NewSimpleClientset()
			podsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Pods()
			for _, p := range tc.pods {
				kubeClient.CoreV1().Pods(testNamespace).Create(context.Background(), p, metav1.CreateOptions{})
				podsClient.Informer().GetIndexer().Add(p)
			}
			podCounter := NewPodAccessor(podsClient.Lister(), testNamespace, testRevision)

			got, err := podCounter.ReadyCount()
			if err != nil {
				t.Fatal("ReadyCount failed:", err)
			}
			if got != tc.want {
				t.Errorf("ReadyCount = %d, want: %d", got, tc.want)
			}
			got, err = podCounter.NotReadyCount()
			if err != nil {
				t.Fatal("NotReadyCount failed:", err)
			}
			// All that are not ready must be not-ready.
			if want := len(tc.pods) - tc.want; got != want {
				t.Errorf("NotReadyCount = %d, want: %d", got, want)
			}
		})
	}
}

func TestPodIPsSortedByAge(t *testing.T) {
	aTime := time.Now()

	tests := []struct {
		name string
		pods []*corev1.Pod
		want []string
	}{{
		name: "no pods",
	}, {
		name: "one pod",
		pods: []*corev1.Pod{
			pod("foo", makeReady, withStartTime(aTime), withIP("1.1.1.1")),
		},
		want: []string{"1.1.1.1"},
	}, {
		name: "one pod, not ready",
		pods: []*corev1.Pod{
			pod("orion", withStartTime(aTime), withIP("1.1.1.1")),
		},
	}, {
		name: "more than 1 pod, sorted",
		pods: []*corev1.Pod{
			pod("ride-the-lightning", makeReady, withStartTime(aTime), withIP("1.9.8.2")),
			pod("fade-to-black", makeReady, withStartTime(aTime.Add(time.Second)), withIP("1.9.8.4")),
			pod("battery", makeReady, withStartTime(aTime.Add(time.Minute)), withIP("1.9.8.8")),
		},
		want: []string{"1.9.8.2", "1.9.8.4", "1.9.8.8"},
	}, {
		name: "more than 1 pod, unsorted",
		pods: []*corev1.Pod{
			pod("one", makeReady, withStartTime(aTime), withIP("2.0.0.6")),
			pod("seek-and-destroy", makeReady, withStartTime(aTime.Add(-time.Second)), withIP("2.0.0.3")),
			pod("metal-militia", makeReady, withStartTime(aTime.Add(time.Minute)), withIP("2.0.0.9")),
		},
		want: []string{"2.0.0.3", "2.0.0.6", "2.0.0.9"},
	}, {
		name: "more than 1 pod, unsorted, preserve order",
		pods: []*corev1.Pod{
			pod("nothing-else-matters", makeReady, withStartTime(aTime), withIP("1.2.3.4")),
			pod("wherever-i-may-roam", makeReady, withStartTime(aTime.Add(time.Second)), withIP("2.3.4.5")),
			pod("sad-but-true", makeReady, withStartTime(aTime.Add(time.Minute)), withIP("3.4.5.6")),
			pod("enter-sandman", makeReady, withStartTime(aTime.Add(time.Hour)), withIP("1.2.3.5")),
		},
		want: []string{"1.2.3.4", "2.3.4.5", "3.4.5.6", "1.2.3.5"},
	}, {
		name: "one pod, but can't use",
		pods: []*corev1.Pod{
			pod("unforgiven", withStartTime(aTime), withIP("1.1.1.1"), withPhase(corev1.PodPending)),
		},
		want: []string{},
	}, {
		name: "one pod, but can't use II",
		pods: []*corev1.Pod{
			pod("unforgiven-ii", withStartTime(aTime), withIP("1.1.1.1"), withPhase(corev1.PodRunning),
				func(p *corev1.Pod) {
					n := metav1.Now()
					p.DeletionTimestamp = &n // Pod deleted.
				},
			),
		},
		want: []string{},
	}, {
		name: "lock step",
		pods: []*corev1.Pod{
			pod("hit-the-lights", withStartTime(aTime), withIP("1.1.1.1"), withPhase(corev1.PodRunning),
				func(p *corev1.Pod) {
					n := metav1.Now()
					p.DeletionTimestamp = &n
				},
			),
			pod("whiplash", makeReady, withStartTime(aTime), withIP("1.2.3.4")),
			pod("unforgiven", withStartTime(aTime), withIP("1.3.4.5"), withPhase(corev1.PodFailed)),
			pod("motorbreath", makeReady, withStartTime(aTime.Add(-time.Second)), withIP("1.2.3.9")),
		},
		want: []string{"1.2.3.9", "1.2.3.4"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			kubeClient := fakek8s.NewSimpleClientset()
			podsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Pods()
			for _, p := range tc.pods {
				kubeClient.CoreV1().Pods(testNamespace).Create(context.Background(), p, metav1.CreateOptions{})
				podsClient.Informer().GetIndexer().Add(p)
			}
			podCounter := NewPodAccessor(podsClient.Lister(), testNamespace, testRevision)

			got, err := podCounter.PodIPsByAge()
			if err != nil {
				t.Fatal("PodIPsByAge failed:", err)
			}
			if want := tc.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Error("PodIPsByAge wrong answer (-want, +got):\n", cmp.Diff(want, got, cmpopts.EquateEmpty()))
			}
		})
	}
}

func TestPendingTerminatingCounts(t *testing.T) {
	kubeClient := fakek8s.NewSimpleClientset()
	podsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Pods()
	createPods := func(pods []*corev1.Pod) {
		for _, p := range pods {
			kubeClient.CoreV1().Pods(testNamespace).Create(context.Background(), p, metav1.CreateOptions{})
			podsClient.Informer().GetIndexer().Add(p)
		}
	}

	podCounter := NewPodAccessor(podsClient.Lister(), testNamespace, testRevision)

	tests := []struct {
		name            string
		pods            []*corev1.Pod
		wantRunning     int
		wantPending     int
		wantTerminating int
		wantErr         bool
	}{{
		name:            "no pods",
		pods:            podsInPhases(0, 0, 0),
		wantRunning:     0,
		wantPending:     0,
		wantTerminating: 0,
	}, {
		name:            "one running/two pending/three terminating pod",
		pods:            podsInPhases(1, 2, 3),
		wantRunning:     1,
		wantPending:     2,
		wantTerminating: 3,
	}, {
		name:            "ten running/eleven pending/twelve terminating pods",
		pods:            podsInPhases(10, 11, 12),
		wantRunning:     10,
		wantPending:     11,
		wantTerminating: 12,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			createPods(test.pods)

			_, _, pending, terminating, err := podCounter.PodCountsByState()
			if got, want := (err != nil), test.wantErr; got != want {
				t.Errorf("WantErr = %v, want: %v, err: %v", got, want, err)
			}

			if pending != test.wantPending {
				t.Errorf("PendingCount() = %d, want: %d", pending, test.wantPending)
			}

			if terminating != test.wantTerminating {
				t.Errorf("TerminatingCount() = %d, want: %d", terminating, test.wantTerminating)
			}
		})
	}
}

type podOption func(p *corev1.Pod)

func pod(name string, pos ...podOption) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    map[string]string{serving.RevisionLabelKey: testRevision},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
	for _, po := range pos {
		po(p)
	}
	return p
}

func withPhase(ph corev1.PodPhase) podOption {
	return func(p *corev1.Pod) {
		p.Status.Phase = ph
	}
}

func makeReady(p *corev1.Pod) {
	p.Status.Conditions = []corev1.PodCondition{{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}}
}

func withStartTime(t time.Time) podOption {
	tm := metav1.NewTime(t)
	return func(p *corev1.Pod) {
		p.Status.StartTime = &tm
	}
}

func withIP(ip string) podOption {
	return func(p *corev1.Pod) {
		p.Status.PodIP = ip
	}
}

// Shortcut for a much used combo.
func phasedPod(name string, phase corev1.PodPhase) *corev1.Pod {
	return pod(name, withPhase(phase))
}

func podsInPhases(running, pending, terminating int) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, running+pending+terminating)

	for i := 0; i < running; i++ {
		pods = append(pods, phasedPod("running-pod-"+strconv.Itoa(i), corev1.PodRunning))
	}

	now := metav1.Now()
	for i := 0; i < terminating; i++ {
		p := phasedPod("terminating-pod-"+strconv.Itoa(i), corev1.PodRunning)
		p.DeletionTimestamp = &now
		pods = append(pods, p)
	}

	for i := 0; i < pending; i++ {
		pods = append(pods, phasedPod("pending-pod-"+strconv.Itoa(i), corev1.PodPending))
	}
	return pods
}

func TestPodIPsSplitByAge(t *testing.T) {
	now := time.Now()
	const cutOff = time.Minute

	tests := []struct {
		name    string
		pods    []*corev1.Pod
		wantOld []string
		wantNew []string
	}{{
		name: "no pods",
	}, {
		name: "one new pod",
		pods: []*corev1.Pod{
			pod("let-it-be", makeReady, withStartTime(now.Add(-cutOff+time.Second)), withIP("1.1.1.1")),
		},
		wantNew: []string{"1.1.1.1"},
	}, {
		name: "one old pod",
		pods: []*corev1.Pod{
			pod("i-me-mine", makeReady, withStartTime(now.Add(-cutOff-time.Second)), withIP("1.1.1.1")),
		},
		wantOld: []string{"1.1.1.1"},
	}, {
		name: "one pod, not ready",
		pods: []*corev1.Pod{
			pod("two-of-us", withStartTime(now), withIP("1.1.1.1")),
		},
	}, {
		name: "two old pods, one new",
		pods: []*corev1.Pod{
			pod("one-after-909", makeReady, withStartTime(now.Add(-5*time.Second)), withIP("1.9.8.2")),
			pod("the-long-and-winding-road", makeReady, withStartTime(now.Add(-time.Hour)), withIP("1.9.8.4")),
			pod("get-back", makeReady, withStartTime(now.Add(-cutOff)), withIP("1.9.8.8")),
		},
		wantNew: []string{"1.9.8.2"},
		wantOld: []string{"1.9.8.4", "1.9.8.8"},
	}, {
		name: "one pod, but can't use",
		pods: []*corev1.Pod{
			pod("dont-let-me-down", withStartTime(now), withIP("1.1.1.1"), withPhase(corev1.PodPending)),
		},
	}, {
		name: "one pod, but can't use II",
		pods: []*corev1.Pod{
			pod("dig-a-pony", withStartTime(now), withIP("1.1.1.1"), withPhase(corev1.PodRunning),
				func(p *corev1.Pod) {
					n := metav1.Now()
					p.DeletionTimestamp = &n // Pod deleted.
				},
			),
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			kubeClient := fakek8s.NewSimpleClientset()
			podsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Pods()
			for _, p := range tc.pods {
				kubeClient.CoreV1().Pods(testNamespace).Create(context.Background(), p, metav1.CreateOptions{})
				podsClient.Informer().GetIndexer().Add(p)
			}
			podCounter := NewPodAccessor(podsClient.Lister(), testNamespace, testRevision)

			gotOld, gotNew, err := podCounter.PodIPsSplitByAge(cutOff, now)
			if err != nil {
				t.Fatal("PodIPsSplitByAge failed:", err)
			}

			// Pod listing is non deterministic so we arbitrarily sort the IPs.
			sort.Strings(gotOld)
			sort.Strings(gotNew)

			if !cmp.Equal(gotOld, tc.wantOld, cmpopts.EquateEmpty()) {
				t.Error("GotOld wrong answer (-want, +got):\n", cmp.Diff(tc.wantOld, gotOld, cmpopts.EquateEmpty()))
			}
			if !cmp.Equal(gotNew, tc.wantNew, cmpopts.EquateEmpty()) {
				t.Error("GotNew wrong answer (-want, +got):\n", cmp.Diff(tc.wantNew, gotNew, cmpopts.EquateEmpty()))
			}
		})
	}
}
