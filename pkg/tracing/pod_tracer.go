package tracing

import (
	"context"
	"fmt"
	"time"

	zipkin "github.com/openzipkin/zipkin-go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	csWaiting = "Waiting"
	csRunning = "Running"
	csRead    = "Ready"
)

type containerState string

type containerTrace struct {
	span  zipkin.Span
	state containerState
}

func TracePodStartup(ctx context.Context, tracer *zipkin.Tracer, kubeClient kubernetes.Interface, namespace string, lo *metav1.ListOptions) (zipkin.Span, error) {
	wi, err := kubeClient.CoreV1().Pods(namespace).Watch(*lo)
	if err != nil {
		return nil, err
	}
	defer wi.Stop()

	var (
		podCreating    zipkin.Span
		podCreatingCtx context.Context
	)
	initContainerSpans := make(map[string]*containerTrace)
	containerSpans := make(map[string]*containerTrace)

	for {
		select {
		case ev := <-wi.ResultChan():
			if pod, ok := ev.Object.(*corev1.Pod); ok {
				if podCreating == nil {
					podCreating, podCreatingCtx = tracer.StartSpanFromContext(ctx, "pod_creating")
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
							span, _ := tracer.StartSpanFromContext(podCreatingCtx, fmt.Sprintf("container_startup_%s", cs.Name))
							csCase.dest[cs.Name] = &containerTrace{
								span:  span,
								state: csWaiting,
							}
							ct = csCase.dest[cs.Name]
						}
						span := ct.span

						if cs.State.Terminated != nil {
							if !csCase.initContainer {
								zipkin.TagError.Set(span, "terminated")
							}
							span.Finish()
						}

						if cs.State.Running != nil && ct.state != csRunning {
							span.Annotate(time.Now(), "running")
							ct.state = csRunning
						}

						if cs.Ready {
							span.Finish()
						}
					}
				}

				if pod.Status.Phase == corev1.PodRunning {
					if podCreating != nil {
						podCreating.Finish()
					}
					break
				}
			}
		}
	}

	return podCreating, nil
}
