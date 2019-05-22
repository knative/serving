/*
Copyright 2018 The Knative Authors

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
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// UserContainerName is the name of the user-container in the PodSpec
	UserContainerName = "user-container"
	// FluentdContainerName is the name of the fluentd sidecar when enabled
	FluentdContainerName = "fluentd-proxy"
	// QueueContainerName is the name of the queue proxy side car
	QueueContainerName = "queue-proxy"

	// queueContainerRequestCPUAnnotation is the request.cpu of the queue proxy side car
	queueContainerRequestCPUAnnotation = "queue.sidecar.serving.knative.dev/RequestCpu"

	// queueContainerLimitCPUAnnotation is the limit.cpu of the queue proxy side car
	queueContainerLimitCPUAnnotation = "queue.sidecar.serving.knative.dev/LimitCpu"

	// queueContainerRequestMemoryAnnotation is the request.memory of the queue proxy side car
	queueContainerRequestMemoryAnnotation = "queue.sidecar.serving.knative.dev/RequestMemory"

	// queueContainerLimitMemoryAnnotation is the limit.memory of the queue proxy side car
	queueContainerLimitMemoryAnnotation = "queue.sidecar.serving.knative.dev/LimitMemory"

	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	// IstioOutboundIPRangeAnnotation defines the outbound ip ranges istio allows.
	// TODO(mattmoor): Make this private once we remove revision_test.go
	IstioOutboundIPRangeAnnotation = "traffic.sidecar.istio.io/includeOutboundIPRanges"

	// AppLabelKey is the label defining the application's name.
	AppLabelKey = "app"
)

var (
	// ProgressDeadlineSeconds is the time in seconds we wait for the deployment to
	// be ready before considering it failed.
	ProgressDeadlineSeconds = int32(120)

	// See https://github.com/knative/serving/pull/1124#issuecomment-397120430
	// for how CPU and memory values were calculated.
	fluentdContainerCPU = resource.MustParse("25m")
	queueContainerCPU   = resource.MustParse("25m")
)
