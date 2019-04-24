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
	"fmt"

	"go.opencensus.io/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	csWaiting = "Waiting"
	csRunning = "Running"
	csReady   = "Ready"
)

type containerState string

type containerTrace struct {
	span  *trace.Span
	state containerState
}

// TracePodStartup creates spans detailing the startup process of the pod who's events arrive over eventCh
func TracePodStartup(ctx context.Context, stopCh <-chan struct{}, eventCh <-chan watch.Event) (*trace.Span, error) {
	var (
		podCreating    *trace.Span
		podCreatingCtx context.Context
		podState       containerState
	)
	initContainerSpans := make(map[string]*containerTrace)
	containerSpans := make(map[string]*containerTrace)

	for {
		select {
		case ev := <-eventCh:
			if pod, ok := ev.Object.(*corev1.Pod); ok {
				if podState == "" {
					podCreatingCtx, podCreating = trace.StartSpan(ctx, "pod_creating")
					podState = csWaiting
				}

				// Set (init)containerSpans
				for _, csCase := range []struct {
					src           []corev1.ContainerStatus
					dest          map[string]*containerTrace
					initContainer bool
				}{{
					src:           pod.Status.InitContainerStatuses,
					dest:          initContainerSpans,
					initContainer: true,
				}, {
					src:           pod.Status.ContainerStatuses,
					dest:          containerSpans,
					initContainer: false,
				}} {
					for _, cs := range csCase.src {
						ct, ok := csCase.dest[cs.Name]
						if !ok {
							_, span := trace.StartSpan(podCreatingCtx, fmt.Sprintf("container_startup_%s", cs.Name))
							csCase.dest[cs.Name] = &containerTrace{
								span:  span,
								state: csWaiting,
							}
							ct = csCase.dest[cs.Name]
						}
						span := ct.span

						if cs.State.Terminated != nil {
							if !csCase.initContainer {
								span.Annotate([]trace.Attribute{
									trace.StringAttribute(fmt.Sprintf("tracepod.container-%s.error", cs.Name), "terminated"),
								}, "ContainerTerminated")
							}
							span.End()
						}

						if cs.State.Running != nil && ct.state != csRunning {
							span.Annotate([]trace.Attribute{
								trace.StringAttribute(fmt.Sprintf("tracepod.container-%s.running", cs.Name), "running"),
							}, "ContainerRunning")
							ct.state = csRunning
						}

						if cs.Ready {
							ct.state = csReady
							span.End()
						}
					}
				}

				if pod.Status.Phase == corev1.PodRunning {
					if podCreating != nil && podState == csWaiting {
						podState = csRunning
						podCreating.Annotate([]trace.Attribute{
							trace.StringAttribute("tracepod.running", "running"),
						}, "PodTrace")
					}
				}

				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
						if podCreating != nil {
							podState = csReady
							podCreating.End()
						}
						return podCreating, nil
					}
				}
			}
		case <-stopCh:
			if podCreating != nil {
				podCreating.Annotate([]trace.Attribute{
					trace.StringAttribute("tracepod.error", "trace exit"),
				}, "PodTrace")
				podCreating.End()
			}
			return podCreating, nil
		}
	}
}
