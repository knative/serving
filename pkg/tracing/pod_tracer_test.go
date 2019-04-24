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

package tracing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/knative/serving/pkg/tracing/config"
	openzipkin "github.com/openzipkin/zipkin-go"
	zipkinmodel "github.com/openzipkin/zipkin-go/model"
	zipkinreporter "github.com/openzipkin/zipkin-go/reporter"
	reporterrecorder "github.com/openzipkin/zipkin-go/reporter/recorder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	containerWaiting    string = "ContainerWaiting"
	containerRunning    string = "ContainerRunning"
	containerTerminated string = "ContainerTerminated"
)

func containerStatus(name string, ready bool, state string) *corev1.ContainerStatus {
	status := &corev1.ContainerStatus{
		Name:  name,
		Ready: ready,
	}
	switch state {
	case containerWaiting:
		status.State = corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{},
		}
	case containerRunning:
		status.State = corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		}
	case containerTerminated:
		status.State = corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{},
		}
	}
	return status
}

func podConditions(ready bool) []corev1.PodCondition {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}

	return []corev1.PodCondition{
		{
			Type:   corev1.PodReady,
			Status: status,
		},
	}
}

func withInitContainerStatus(pod *corev1.Pod, status *corev1.ContainerStatus) *corev1.Pod {
	pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses, *status)
	return pod
}

func withContainerStatus(pod *corev1.Pod, status *corev1.ContainerStatus) *corev1.Pod {
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, *status)
	return pod
}

func withConditions(pod *corev1.Pod, conditions []corev1.PodCondition) *corev1.Pod {
	pod.Status.Conditions = conditions
	return pod
}

func testPod(phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

func TestTracePodStartup(t *testing.T) {
	for _, tc := range []struct {
		name        string
		events      []watch.Event
		expectSpans []zipkinmodel.SpanModel
		forceEnd    bool
	}{{
		name: "Simple pod start",
		events: []watch.Event{{
			Type:   watch.Added,
			Object: withConditions(testPod(corev1.PodPending), podConditions(false)),
		}, {
			Type:   watch.Modified,
			Object: withConditions(testPod(corev1.PodRunning), podConditions(true)),
		}},
		expectSpans: []zipkinmodel.SpanModel{
			{},
		},
	}, {
		name: "Missed pod add",
		events: []watch.Event{{
			Type:   watch.Modified,
			Object: withConditions(testPod(corev1.PodRunning), podConditions(true)),
		}},
		expectSpans: []zipkinmodel.SpanModel{
			{},
		},
	}, {
		name: "Single container started",
		events: []watch.Event{{
			Type: watch.Added,
			Object: withContainerStatus(
				withConditions(testPod(corev1.PodPending), podConditions(false)),
				containerStatus("test-container", false, containerWaiting)),
		}, {
			Type: watch.Modified,
			Object: withContainerStatus(
				withConditions(testPod(corev1.PodRunning), podConditions(true)),
				containerStatus("test-container", true, containerRunning)),
		}},
		expectSpans: []zipkinmodel.SpanModel{
			{},
			{},
		},
	}, {
		name: "Single init container started",
		events: []watch.Event{{
			Type: watch.Added,
			Object: withInitContainerStatus(
				withConditions(testPod(corev1.PodPending), podConditions(false)),
				containerStatus("test-init-container", false, containerWaiting)),
		}, {
			Type: watch.Modified,
			Object: withInitContainerStatus(
				withConditions(testPod(corev1.PodPending), podConditions(true)),
				containerStatus("test-init-container", true, containerRunning)),
		}},
		expectSpans: []zipkinmodel.SpanModel{
			{},
			{},
		},
	}, {
		name: "Container terminated",
		events: []watch.Event{{
			Type: watch.Added,
			Object: withContainerStatus(
				withConditions(testPod(corev1.PodPending), podConditions(false)),
				containerStatus("test-container", false, containerWaiting)),
		}, {
			Type: watch.Modified,
			Object: withContainerStatus(
				withConditions(testPod(corev1.PodPending), podConditions(false)),
				containerStatus("test-container", false, containerTerminated)),
		}},
		expectSpans: []zipkinmodel.SpanModel{
			{},
			{},
		},
		forceEnd: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			reporter := reporterrecorder.NewReporter()
			defer reporter.Close()

			endpoint, _ := openzipkin.NewEndpoint("test", "localhost:1234")
			oct := NewOpenCensusTracer(WithZipkinExporter(func(cfg *config.Config) (zipkinreporter.Reporter, error) {
				return reporter, nil
			}, endpoint))
			oct.ApplyConfig(&config.Config{
				Enable: true,
				Debug:  true,
			})
			defer oct.Finish()

			stopCh := make(chan struct{})
			if !tc.forceEnd {
				defer close(stopCh)
			}

			evCh := make(chan watch.Event)

			wg := sync.WaitGroup{}
			wg.Add(1)
			go func() {
				TracePodStartup(context.TODO(), stopCh, evCh)
				wg.Done()
			}()

			for _, ev := range tc.events {
				evCh <- ev
			}
			if tc.forceEnd {
				time.Sleep(20 * time.Millisecond)
				close(stopCh)
			}

			wg.Wait()

			gotSpans := reporter.Flush()
			if len(gotSpans) != len(tc.expectSpans) {
				t.Errorf("Expected %d spans, got %d.", len(tc.expectSpans), len(gotSpans))
			}

			// TODO(greghaynes) better asserting on spans
		})
	}
}
