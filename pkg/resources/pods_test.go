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
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	"knative.dev/serving/pkg/apis/serving"
)

func TestScopedPodsCounter(t *testing.T) {
	kubeClient := fakek8s.NewSimpleClientset()
	podsClient := kubeinformers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Pods()
	createPods := func(pods []*corev1.Pod) {
		for _, p := range pods {
			kubeClient.CoreV1().Pods(testNamespace).Create(p)
			podsClient.Informer().GetIndexer().Add(p)
		}
	}

	podCounter := NewNotRunningPodsCounter(podsClient.Lister(), testNamespace, testService)

	tests := []struct {
		name            string
		pods            []*corev1.Pod
		wantRunning     int
		wantPending     int
		wantTerminating int
		wantErr         bool
	}{{
		name:            "no pods",
		pods:            pods(0, 0, 0),
		wantRunning:     0,
		wantPending:     0,
		wantTerminating: 0,
	}, {
		name:            "one running/two pending/three terminating pod",
		pods:            pods(1, 2, 3),
		wantRunning:     1,
		wantPending:     2,
		wantTerminating: 3,
	}, {
		name:            "ten running/eleven pending/twelve terminating pods",
		pods:            pods(10, 11, 12),
		wantRunning:     10,
		wantPending:     11,
		wantTerminating: 12,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.pods != nil {
				createPods(test.pods)
			}

			pending, terminating, err := podCounter.PendingTerminatingCount()
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

func pod(name string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			Labels:    map[string]string{serving.RevisionLabelKey: testService},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

func pods(running, pending, terminating int) []*corev1.Pod {
	pods := make([]*corev1.Pod, 0, running+pending+terminating)

	for i := 0; i < running; i++ {
		pods = append(pods, pod("running-pod-"+strconv.Itoa(i), corev1.PodRunning))
	}

	now := metav1.Now()
	for i := 0; i < terminating; i++ {
		p := pod("terminating-pod-"+strconv.Itoa(i), corev1.PodRunning)
		p.DeletionTimestamp = &now
		pods = append(pods, p)
	}

	for i := 0; i < pending; i++ {
		pods = append(pods, pod("pending-pod-"+strconv.Itoa(i), corev1.PodPending))
	}

	return pods
}
