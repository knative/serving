/*
Copyright 2024 The Knative Authors

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
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/serving/pkg/apis/config"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

type override struct {
	crdName string
	entries []entry
}

type entry struct {
	path              string
	description       string
	allowedFields     sets.Set[string]
	featureFlagFields []flagField
	dropRequired      sets.Set[string]

	// Drops list-type
	//   x-kubernetes-list-map-keys:
	//     - name
	//   x-kubernetes-list-type: map
	dropListType bool
}

type flagField struct {
	name string
	flag string
}

var overrides = []override{{
	crdName: "services.serving.knative.dev",
	entries: revSpecOverrides("spec.template.spec"),
}, {
	crdName: "configurations.serving.knative.dev",
	entries: revSpecOverrides("spec.template.spec"),
}, {
	crdName: "revisions.serving.knative.dev",
	entries: revSpecOverrides("spec"),
}, {
	crdName: "podautoscalers.autoscaling.internal.knative.dev",
	entries: []entry{{
		path: "spec.scaleTargetRef",
		allowedFields: sets.New(
			"apiVersion",
			"kind",
			"name",
		),
	}},
}}

func revSpecOverrides(prefixPath string) []entry {
	entries := []entry{{
		allowedFields: sets.New(
			"automountServiceAccountToken",
			"containers",
			"enableServiceLinks",
			"imagePullSecrets",
			"serviceAccountName",
			"volumes",
		).Insert(revisionSpecFields()...),
		featureFlagFields: []flagField{
			{name: "affinity", flag: config.FeaturePodSpecAffinity},
			{name: "dnsConfig", flag: config.FeaturePodSpecDNSConfig},
			{name: "dnsPolicy", flag: config.FeaturePodSpecDNSPolicy},
			{name: "hostAliases", flag: config.FeaturePodSpecHostAliases},
			{name: "hostIPC", flag: config.FeaturePodSpecHostIPC},
			{name: "hostNetwork", flag: config.FeaturePodSpecHostNetwork},
			{name: "hostPID", flag: config.FeaturePodSpecHostPID},
			{name: "initContainers", flag: config.FeaturePodSpecInitContainers},
			{name: "nodeSelector", flag: config.FeaturePodSpecNodeSelector},
			{name: "priorityClassName", flag: config.FeaturePodSpecPriorityClassName},
			{name: "runtimeClassName", flag: config.FeaturePodSpecRuntimeClassName},
			{name: "schedulerName", flag: config.FeaturePodSpecSchedulerName},
			{name: "securityContext", flag: config.FeaturePodSpecSecurityContext},
			{name: "shareProcessNamespace", flag: config.FeaturePodSpecShareProcessNamespace},
			{name: "tolerations", flag: config.FeaturePodSpecTolerations},
			{name: "topologySpreadConstraints", flag: config.FeaturePodSpecTopologySpreadConstraints},
		},
	}, {
		path:         "containers",
		dropListType: true,
		dropRequired: sets.New("name"),
		allowedFields: sets.New(
			"args",
			"command",
			"env",
			"envFrom",
			"image",
			"imagePullPolicy",
			"livenessProbe",
			"name",
			"ports",
			"readinessProbe",
			"resources",
			"securityContext",
			"startupProbe",
			"terminationMessagePath",
			"terminationMessagePolicy",
			"volumeMounts",
			"workingDir",
		),
	}, {
		path:         "containers.ports",
		dropListType: true,
		dropRequired: sets.New("containerPort"),
		allowedFields: sets.New(
			"containerPort",
			"name",
			"protocol",
		),
	}, {
		path: "containers.securityContext",
		allowedFields: sets.New(
			"allowPrivilegeEscalation",
			"capabilities",
			"privileged",
			"readOnlyRootFilesystem",
			"runAsGroup",
			"runAsNonRoot",
			"runAsUser",
			"seccompProfile",
		),
	}, {
		path:        "containers.securityContext.privileged",
		description: "Run container in privileged mode. This can only be set to explicitly to 'false'",
	}, {
		path: "containers.securityContext.capabilities",
		allowedFields: sets.New(
			"add",
			"drop",
		),
	}, {
		path:        "containers.securityContext.capabilities.add",
		description: "This is accessible behind a feature flag - " + config.FeatureContainerSpecAddCapabilities,
	}, {
		path: "containers.resources",
		allowedFields: sets.New(
			"limits",
			"requests",
		),
	}, {
		path: "containers.env",
		allowedFields: sets.New(
			"name",
			"value",
			"valueFrom",
		),
	}, {
		path: "containers.env.valueFrom",
		allowedFields: sets.New(
			"configMapKeyRef",
			"secretKeyRef",
		),
		featureFlagFields: []flagField{{
			name: "fieldRef",
			flag: config.FeaturePodSpecFieldRef,
		}, {
			name: "resourceFieldRef",
			flag: config.FeaturePodSpecFieldRef,
		}},
	}, {
		path: "containers.env.valueFrom.configMapKeyRef",
		allowedFields: sets.New(
			"name",
			"key",
			"optional",
		),
	}, {
		path: "containers.env.valueFrom.secretKeyRef",
		allowedFields: sets.New(
			"name",
			"key",
			"optional",
		),
	}, {
		path: "containers.envFrom",
		allowedFields: sets.New(
			"prefix",
			"configMapRef",
			"secretRef",
		),
	}, {
		path: "containers.envFrom.configMapRef",
		allowedFields: sets.New(
			"name",
			"optional",
		),
	}, {
		path: "containers.envFrom.secretRef",
		allowedFields: sets.New(
			"name",
			"optional",
		),
	}, {
		path: "enableServiceLinks",
		description: "EnableServiceLinks indicates whether information about" +
			"services should be injected into pod's environment variables, " +
			"matching the syntax of Docker links. Optional: Knative defaults this to false.",
	}, {
		path: "containers.volumeMounts",
		allowedFields: sets.New(
			"name",
			"readOnly",
			"mountPath",
			"subPath",
		),
	}, {
		path: "volumes",
		allowedFields: sets.New(
			"name",
			"secret",
			"configMap",
			"projected",
		),
		featureFlagFields: []flagField{{
			name: "emptyDir",
			flag: config.FeaturePodSpecEmptyDir,
		}, {
			name: "persistentVolumeClaim",
			flag: config.FeaturePodSpecPVClaim,
		}, {
			name: "hostPath",
			flag: config.FeaturePodSpecHostPath,
		}},
	}, {
		path: "volumes.secret",
		allowedFields: sets.New(
			"defaultMode",
			"items",
			"optional",
			"secretName",
		),
	}, {
		path: "volumes.secret.items",
		allowedFields: sets.New(
			"key",
			"path",
			"mode",
		),
	}, {
		path: "volumes.configMap",
		allowedFields: sets.New(
			"defaultMode",
			"items",
			"optional",
			"name",
		),
	}, {
		path: "volumes.configMap.items",
		allowedFields: sets.New(
			"key",
			"path",
			"mode",
		),
	}, {
		path: "volumes.projected",
		allowedFields: sets.New(
			"defaultMode",
			"sources",
		),
	}, {
		path: "volumes.projected.sources",
		allowedFields: sets.New(
			// "clusterTrustBundle",
			"configMap",
			"downwardAPI",
			"secret",
			"serviceAccountToken",
		),
	}, {
		path: "volumes.projected.sources.configMap",
		allowedFields: sets.New(
			"items",
			"name",
			"optional",
		),
	}, {
		path: "volumes.projected.sources.configMap.items",
		allowedFields: sets.New(
			"key",
			"path",
			"mode",
		),
	}, {
		path: "volumes.projected.sources.secret",
		allowedFields: sets.New(
			"items",
			"name",
			"optional",
		),
	}, {
		path: "volumes.projected.sources.secret.items",
		allowedFields: sets.New(
			"key",
			"path",
			"mode",
		),
	}, {
		path: "volumes.projected.sources.serviceAccountToken",
		allowedFields: sets.New(
			"audience",
			"expirationSeconds",
			"path",
		),
	}, {
		path: "volumes.projected.sources.downwardAPI",
		allowedFields: sets.New(
			"items",
		),
	}, {
		path: "volumes.projected.sources.downwardAPI.items",
		allowedFields: sets.New(
			"path",
			"fieldRef",
			"mode",
			"resourceFieldRef",
		),
	}}

	probes := []string{"livenessProbe", "readinessProbe", "startupProbe"}

	for _, probe := range probes {
		entries = append(entries, entry{
			path: "containers." + probe,
			allowedFields: sets.New(
				"initialDelaySeconds",
				"timeoutSeconds",
				"periodSeconds",
				"successThreshold",
				"failureThreshold",

				// probe handlers
				"httpGet",
				"exec",
				"tcpSocket",
				"grpc",
			),
		})

		entries = append(entries, entry{
			path:        fmt.Sprintf("containers.%s.periodSeconds", probe),
			description: "How often (in seconds) to perform the probe.",
		}, entry{
			path:         fmt.Sprintf("containers.%s.httpGet", probe),
			dropRequired: sets.New("port"),
			allowedFields: sets.New(
				"host",
				"httpHeaders",
				"path",
				"port",
				"scheme",
			),
		}, entry{
			path:          fmt.Sprintf("containers.%s.exec", probe),
			allowedFields: sets.New("command"),
		}, entry{
			path:         fmt.Sprintf("containers.%s.tcpSocket", probe),
			dropRequired: sets.New("port"),
			allowedFields: sets.New(
				"host",
				"port",
			),
		}, entry{
			path:         fmt.Sprintf("containers.%s.grpc", probe),
			dropRequired: sets.New("port"),
			allowedFields: sets.New(
				"port",
				"service",
			),
		})
	}

	for i := range entries {
		if entries[i].path == "" {
			entries[i].path = prefixPath
		} else {
			entries[i].path = prefixPath + "." + entries[i].path
		}
	}
	return entries
}

func revisionSpecFields() []string {
	var (
		revType = reflect.TypeOf(v1.RevisionSpec{})
		fields  = make([]string, 0, revType.NumField())
	)

	for i := range revType.NumField() {
		if revType.Field(i).Name == "PodSpec" {
			continue
		}

		jsonTag := revType.Field(i).Tag.Get("json")
		fields = append(fields, strings.Split(jsonTag, ",")[0])
	}

	return fields
}
