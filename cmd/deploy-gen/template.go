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

package main

import (
	corev1 "k8s.io/api/core/v1"
)

type Params struct {
	// Component is the name of the system component, e.g. webhook, controller.
	Component string `json:"component"`

	// Namespace is the namespace in which to run.
	Namespace string `json:namespace"`

	// SafeToEvict indicates whether the component operates a critical part of
	// the system that would be disrupted if the Pod were evicted to scale the
	// cluster.
	SafeToEvict bool `json:"safeToEvict"`

	// MetricsDomain provides a root metrics namespace for stackdriver-based metrics.
	// TODO(github.com/knative/pkg/pull/953): Remove this.
	MetricsDomain string `json:"metricsDomain"`

	// ImportPath is the Go import path of the binary to run in this Deployment.
	ImportPath string `json:"importPath"`

	Resources corev1.ResourceRequirements `json:"resources"`

	ExtraLabels                   map[string]string    `json:"extraLabels,omitempty"`
	ExtraPorts                    []corev1.ServicePort `json:"extraPorts,omitempty"`
	ProbePort                     int32                `json:"probePort,omitempty"`
	TerminationGracePeriodSeconds int32                `json:"terminationGracePeriodSeconds,omitempty"`
}

const tmpl = `
# Copyright 2020 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# !!! GENERATED DO NOT MODIFY !!!

apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Component}}
  namespace: {{.Namespace}}
  labels:
    serving.knative.dev/release: devel{{range $key, $value := $.ExtraLabels}}
    {{$key}}: {{$value}}{{end}}
spec:
  selector:
    matchLabels:
      app: {{.Component}}
  template:
    metadata:
      annotations:
        cluster-autoscaler.kubernetes.io/safe-to-evict: "{{.SafeToEvict}}"
      labels:
        app: {{.Component}}
        role: {{.Component}}
        serving.knative.dev/release: devel
    spec:
      serviceAccountName: controller
      containers:
      - name: {{.Component}}
        # This is the Go import path for the binary that is containerized
        # and substituted here.
        # TODO(mattmoor): Adopt ko:// strict mode.
        image: {{.ImportPath}}

        resources:
          requests:{{range $key, $value := $.Resources.Requests}}
            {{$key}}: {{ r2s $value }}{{end}}
          limits:{{range $key, $value := $.Resources.Limits}}
            {{$key}}: {{ r2s $value }}{{end}}

        env:
        # Useful reflective properties.
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        - name: SYSTEM_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        # The names of configmaps to watch.
        - name: CONFIG_LOGGING_NAME
          value: config-logging
        - name: CONFIG_OBSERVABILITY_NAME
          value: config-observability

        # TODO(github.com/knative/pkg/pull/953): Remove this.
        - name: METRICS_DOMAIN
          value: {{.MetricsDomain}}

        securityContext:
          allowPrivilegeEscalation: false

        ports:
        - name: metrics
          containerPort: 9090
        - name: profiling
          containerPort: 8008{{range $port := $.ExtraPorts}}
        - name: {{$port.Name}}
          containerPort: {{$port.TargetPort}}{{end}}
{{if ne $.ProbePort 0 }}
        readinessProbe: &probe
          httpGet:
            port: {{$.ProbePort}}
            httpHeaders:
            - name: k-kubelet-probe
              value: "{{.Component}}"
        livenessProbe: *probe
{{end}}
{{if ne $.TerminationGracePeriodSeconds 0 }}
      terminationGracePeriodSeconds: {{$.TerminationGracePeriodSeconds}}
{{end}}
---
# Without this some mesh scenarios fail because there are exposed ports
# without any corresponding service.
apiVersion: v1
kind: Service
metadata:
  name: {{.Component}}
  namespace: {{.Namespace}}
  labels:
    serving.knative.dev/release: devel{{range $key, $value := $.ExtraLabels}}
    {{$key}}: {{$value}}{{end}}
spec:
  ports:
  - name: http-metrics
    port: 9090
    protocol: TCP
    targetPort: 9090
  - name: http-profiling
    port: 8008
    protocol: TCP
    targetPort: 8008
  {{range $port := $.ExtraPorts}}
  - name: {{$port.Name}}
    port: {{$port.Port}}
    protocol: TCP
    targetPort: {{$port.TargetPort}}{{end}}
  selector:
    app: {{.Component}}
`
