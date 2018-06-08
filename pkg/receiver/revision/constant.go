/*
Copyright 2018 Google LLC.

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

package revision

import (
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	userContainerName string = "user-container"
	userPortName      string = "user-port"
	userPort                 = 8080

	fluentdContainerName string = "fluentd-proxy"
	queueContainerName   string = "queue-proxy"
	// queueSidecarName set by -queueSidecarName flag
	queueHTTPPortName string = "queue-http-port"

	requestQueueContainerName string = "request-queue"

	controllerAgentName = "revision-controller"
	autoscalerPort      = 8080

	serviceTimeoutDuration       = 5 * time.Minute
	sidecarIstioInjectAnnotation = "sidecar.istio.io/inject"
	// TODO (arvtiwar): this should be a config option.
	progressDeadlineSeconds int32 = 120
)

var (
	elaPodReplicaCount   = int32(1)
	elaPodMaxUnavailable = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
	elaPodMaxSurge       = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
)
