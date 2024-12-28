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
			{name: "affinity", flag: "kubernetes.podspec-affinity"},
			{name: "dnsConfig", flag: "kubernetes.podspec-dnsconfig"},
			{name: "dnsPolicy", flag: "kubernetes.podspec-dnspolicy"},
			{name: "hostAliases", flag: "kubernetes.podspec-hostaliases"},
			{name: "hostIPC", flag: "kubernetes.podspec-hostipc"},
			{name: "hostNetwork", flag: "kubernetes.podspec-hostnetwork"},
			{name: "hostPID", flag: "kubernetes.podspec-hostpid"},
			{name: "initContainers", flag: "kubernetes.podspec-init-containers"},
			{name: "nodeSelector", flag: "kubernetes.podspec-nodeselector"},
			{name: "priorityClassName", flag: "kubernetes.podspec-priorityclassname"},
			{name: "runtimeClassName", flag: "kubernetes.podspec-runtimeclassname"},
			{name: "schedulerName", flag: "kubernetes.podspec-schedulername"},
			{name: "securityContext", flag: "kubernetes.podspec-securitycontext"},
			{name: "shareProcessNamespace", flag: "kubernetes.podspec-shareprocessnamespace"},
			{name: "tolerations", flag: "kubernetes.podspec-tolerations"},
			{name: "topologySpreadConstraints", flag: "kubernetes.podspec-topologyspreadconstraints"},
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
			"readOnlyRootFilesystem",
			"runAsGroup",
			"runAsNonRoot",
			"runAsUser",
			"seccompProfile",
		),
	}, {
		path: "containers.securityContext.capabilities",
		allowedFields: sets.New(
			"add",
			"drop",
		),
	}, {
		path:        "containers.securityContext.capabilities.add",
		description: "This is accessible behind a feature flag - kubernetes.containerspec-addcapabilities",
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
			flag: "kubernetes.podspec-fieldref",
		}, {
			name: "resourceFieldRef",
			flag: "kubernetes.podspec-fieldref",
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
			flag: "kubernetes.podspec-emptydir",
		}, {
			name: "persistentVolumeClaim",
			flag: "kubernetes.podspec-persistent-volume-claim",
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
			path: fmt.Sprintf("containers.%s", probe),
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
		fields  []string
		revType = reflect.TypeOf(v1.RevisionSpec{})
	)

	for i := 0; i < revType.NumField(); i++ {
		if revType.Field(i).Name == "PodSpec" {
			continue
		}

		jsonTag := revType.Field(i).Tag.Get("json")
		fields = append(fields, strings.Split(jsonTag, ",")[0])
	}

	return fields
}
