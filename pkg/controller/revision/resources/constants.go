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
	UserContainerName    = "user-container"
	FluentdContainerName = "fluentd-proxy"
	EnvoyContainerName   = "istio-proxy"
	QueueContainerName   = "queue-proxy"

	SidecarIstioInjectAnnotation = "sidecar.istio.io/inject"

	AutoscalerPort       = 8080
	ServicePort    int32 = 80
)

// pseudo-constants
var (
	// See https://github.com/knative/serving/pull/1124#issuecomment-397120430
	// for how CPU and memory values were calculated.

	// Each Knative Serving pod gets 500m cpu initially.
	UserContainerCPU    = resource.MustParse("400m")
	FluentdContainerCPU = resource.MustParse("25m")
	EnvoyContainerCPU   = resource.MustParse("50m")
	QueueContainerCPU   = resource.MustParse("25m")

	// Limit CPU recommendation to 2000m
	UserContainerMaxCPU    = resource.MustParse("1700m")
	FluentdContainerMaxCPU = resource.MustParse("100m")
	EnvoyContainerMaxCPU   = resource.MustParse("200m")
	QueueContainerMaxCPU   = resource.MustParse("200m")

	// Limit memory recommendation to 4G
	UserContainerMaxMemory    = resource.MustParse("3700M")
	FluentdContainerMaxMemory = resource.MustParse("100M")
	EnvoyContainerMaxMemory   = resource.MustParse("100M")
	QueueContainerMaxMemory   = resource.MustParse("100M")
)
