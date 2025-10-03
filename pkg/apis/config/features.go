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

package config

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

// Flag is a string value which can be either Enabled, Disabled, or Allowed.
type Flag string

const (
	// FeaturesConfigName is the name of the ConfigMap for the features.
	FeaturesConfigName = "config-features"

	// Enabled turns on an optional behavior.
	Enabled Flag = "Enabled"
	// Disabled turns off an optional behavior.
	Disabled Flag = "Disabled"
	// Allowed neither explicitly disables or enables a behavior.
	// eg. allow a client to control behavior with an annotation or allow a new value through validation.
	Allowed Flag = "Allowed"
	// AllowRootBounded is used by secure-pod-defaults to apply secure defaults without enforcing strict policies;
	// sets RunAsNonRoot to true if not already specified
	AllowRootBounded Flag = "AllowRootBounded"
)

// service annotations under features.knative.dev/*
const (
	// QueueProxyPodInfoFeatureKey gates mouting of podinfo with the value 'enabled'
	QueueProxyPodInfoFeatureKey = "features.knative.dev/queueproxy-podinfo"

	// AllowHTTPFullDuplexFeatureKey gates the use of http1 full duplex per workload
	AllowHTTPFullDuplexFeatureKey = "features.knative.dev/http-full-duplex"
)

// Feature config map keys that are used in schema-tweak
const (
	FeatureContainerSpecAddCapabilities     = "kubernetes.containerspec-addcapabilities"
	FeaturePodSpecAffinity                  = "kubernetes.podspec-affinity"
	FeaturePodSpecDNSConfig                 = "kubernetes.podspec-dnsconfig"
	FeaturePodSpecDNSPolicy                 = "kubernetes.podspec-dnspolicy"
	FeaturePodSpecEmptyDir                  = "kubernetes.podspec-volumes-emptydir"
	FeaturePodSpecFieldRef                  = "kubernetes.podspec-fieldref"
	FeaturePodSpecHostAliases               = "kubernetes.podspec-hostaliases"
	FeaturePodSpecHostIPC                   = "kubernetes.podspec-hostipc"
	FeaturePodSpecHostNetwork               = "kubernetes.podspec-hostnetwork"
	FeaturePodSpecHostPID                   = "kubernetes.podspec-hostpid"
	FeaturePodSpecHostPath                  = "kubernetes.podspec-volumes-hostpath"
	FeaturePodSpecVolumesCSI                = "kubernetes.podspec-volumes-csi"
	FeaturePodSpecInitContainers            = "kubernetes.podspec-init-containers"
	FeaturePodSpecVolumesMountPropagation   = "kubernetes.podspec-volumes-mount-propagation"
	FeaturePodSpecNodeSelector              = "kubernetes.podspec-nodeselector"
	FeaturePodSpecPVClaim                   = "kubernetes.podspec-persistent-volume-claim"
	FeaturePodSpecPriorityClassName         = "kubernetes.podspec-priorityclassname"
	FeaturePodSpecRuntimeClassName          = "kubernetes.podspec-runtimeclassname"
	FeaturePodSpecSchedulerName             = "kubernetes.podspec-schedulername"
	FeaturePodSpecSecurityContext           = "kubernetes.podspec-securitycontext"
	FeaturePodSpecShareProcessNamespace     = "kubernetes.podspec-shareprocessnamespace"
	FeaturePodSpecTolerations               = "kubernetes.podspec-tolerations"
	FeaturePodSpecTopologySpreadConstraints = "kubernetes.podspec-topologyspreadconstraints"
	FeaturePodSpecVolumesImage              = "kubernetes.podspec-volumes-image"
)

func defaultFeaturesConfig() *Features {
	return &Features{
		MultiContainer:                   Enabled,
		MultiContainerProbing:            Disabled,
		PodSpecAffinity:                  Disabled,
		PodSpecTopologySpreadConstraints: Disabled,
		PodSpecHostAliases:               Disabled,
		PodSpecFieldRef:                  Disabled,
		PodSpecNodeSelector:              Disabled,
		PodSpecRuntimeClassName:          Disabled,
		PodSpecSecurityContext:           Disabled,
		PodSpecShareProcessNamespace:     Disabled,
		PodSpecHostIPC:                   Disabled,
		PodSpecHostPID:                   Disabled,
		PodSpecHostNetwork:               Disabled,
		PodSpecPriorityClassName:         Disabled,
		PodSpecSchedulerName:             Disabled,
		ContainerSpecAddCapabilities:     Disabled,
		PodSpecTolerations:               Disabled,
		PodSpecVolumesEmptyDir:           Enabled,
		PodSpecVolumesHostPath:           Disabled,
		PodSpecVolumesMountPropagation:   Disabled,
		PodSpecVolumesCSI:                Disabled,
		PodSpecVolumesImage:              Disabled,
		PodSpecPersistentVolumeClaim:     Disabled,
		PodSpecPersistentVolumeWrite:     Disabled,
		QueueProxyMountPodInfo:           Disabled,
		QueueProxyResourceDefaults:       Disabled,
		PodSpecInitContainers:            Disabled,
		PodSpecDNSPolicy:                 Disabled,
		PodSpecDNSConfig:                 Disabled,
		SecurePodDefaults:                Disabled,
		TagHeaderBasedRouting:            Disabled,
		AutoDetectHTTP2:                  Disabled,
	}
}

// NewFeaturesConfigFromMap creates a Features from the supplied Map
func NewFeaturesConfigFromMap(data map[string]string) (*Features, error) {
	nc := defaultFeaturesConfig()

	if err := cm.Parse(data,
		asFlag("autodetect-http2", &nc.AutoDetectHTTP2),
		asFlag("kubernetes.podspec-persistent-volume-write", &nc.PodSpecPersistentVolumeWrite),
		asFlag("multi-container", &nc.MultiContainer),
		asFlag("multi-container-probing", &nc.MultiContainerProbing),
		asFlag("queueproxy.mount-podinfo", &nc.QueueProxyMountPodInfo),
		asFlag("queueproxy.resource-defaults", &nc.QueueProxyResourceDefaults),
		asSecurePodDefaultsFlag("secure-pod-defaults", &nc.SecurePodDefaults),
		asFlag("tag-header-based-routing", &nc.TagHeaderBasedRouting),
		asFlag(FeatureContainerSpecAddCapabilities, &nc.ContainerSpecAddCapabilities),
		asFlag(FeaturePodSpecAffinity, &nc.PodSpecAffinity),
		asFlag(FeaturePodSpecDNSConfig, &nc.PodSpecDNSConfig),
		asFlag(FeaturePodSpecDNSPolicy, &nc.PodSpecDNSPolicy),
		asFlag(FeaturePodSpecEmptyDir, &nc.PodSpecVolumesEmptyDir),
		asFlag(FeaturePodSpecFieldRef, &nc.PodSpecFieldRef),
		asFlag(FeaturePodSpecHostAliases, &nc.PodSpecHostAliases),
		asFlag(FeaturePodSpecHostIPC, &nc.PodSpecHostIPC),
		asFlag(FeaturePodSpecHostNetwork, &nc.PodSpecHostNetwork),
		asFlag(FeaturePodSpecHostPID, &nc.PodSpecHostPID),
		asFlag(FeaturePodSpecHostPath, &nc.PodSpecVolumesHostPath),
		asFlag(FeaturePodSpecVolumesCSI, &nc.PodSpecVolumesCSI),
		asFlag(FeaturePodSpecVolumesImage, &nc.PodSpecVolumesImage),
		asFlag(FeaturePodSpecInitContainers, &nc.PodSpecInitContainers),
		asFlag(FeaturePodSpecVolumesMountPropagation, &nc.PodSpecVolumesMountPropagation),
		asFlag(FeaturePodSpecNodeSelector, &nc.PodSpecNodeSelector),
		asFlag(FeaturePodSpecPVClaim, &nc.PodSpecPersistentVolumeClaim),
		asFlag(FeaturePodSpecPriorityClassName, &nc.PodSpecPriorityClassName),
		asFlag(FeaturePodSpecRuntimeClassName, &nc.PodSpecRuntimeClassName),
		asFlag(FeaturePodSpecSchedulerName, &nc.PodSpecSchedulerName),
		asFlag(FeaturePodSpecSecurityContext, &nc.PodSpecSecurityContext),
		asFlag(FeaturePodSpecShareProcessNamespace, &nc.PodSpecShareProcessNamespace),
		asFlag(FeaturePodSpecTolerations, &nc.PodSpecTolerations),
		asFlag(FeaturePodSpecTopologySpreadConstraints, &nc.PodSpecTopologySpreadConstraints),
	); err != nil {
		return nil, err
	}
	return nc, nil
}

// NewFeaturesConfigFromConfigMap creates a Features from the supplied ConfigMap
func NewFeaturesConfigFromConfigMap(config *corev1.ConfigMap) (*Features, error) {
	return NewFeaturesConfigFromMap(config.Data)
}

// Features specifies which features are allowed by the webhook.
type Features struct {
	MultiContainer                   Flag
	MultiContainerProbing            Flag
	PodSpecAffinity                  Flag
	PodSpecTopologySpreadConstraints Flag
	PodSpecFieldRef                  Flag
	PodSpecHostAliases               Flag
	PodSpecNodeSelector              Flag
	PodSpecRuntimeClassName          Flag
	PodSpecSecurityContext           Flag
	PodSpecShareProcessNamespace     Flag
	PodSpecHostIPC                   Flag
	PodSpecHostPID                   Flag
	PodSpecHostNetwork               Flag
	PodSpecPriorityClassName         Flag
	PodSpecSchedulerName             Flag
	ContainerSpecAddCapabilities     Flag
	PodSpecTolerations               Flag
	PodSpecVolumesEmptyDir           Flag
	PodSpecVolumesHostPath           Flag
	PodSpecVolumesMountPropagation   Flag
	PodSpecVolumesCSI                Flag
	PodSpecVolumesImage              Flag
	PodSpecInitContainers            Flag
	PodSpecPersistentVolumeClaim     Flag
	PodSpecPersistentVolumeWrite     Flag
	QueueProxyMountPodInfo           Flag
	QueueProxyResourceDefaults       Flag
	PodSpecDNSPolicy                 Flag
	PodSpecDNSConfig                 Flag
	SecurePodDefaults                Flag
	TagHeaderBasedRouting            Flag
	AutoDetectHTTP2                  Flag
}

// asFlag parses the value at key as a Flag into the target, if it exists.
// Only accepts Enabled, Disabled, and Allowed values.
func asFlag(key string, target *Flag) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			for _, flag := range []Flag{Enabled, Allowed, Disabled} {
				if strings.EqualFold(raw, string(flag)) {
					*target = flag
					return nil
				}
			}
		}
		return nil
	}
}

// asSecurePodDefaultsFlag parses the value at key as a Flag into the target, if it exists.
// Accepts Enabled, Disabled, Allowed, and SecureDefaultsOverridable values.
func asSecurePodDefaultsFlag(key string, target *Flag) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			for _, flag := range []Flag{Disabled, AllowRootBounded, Enabled} {
				if strings.EqualFold(raw, string(flag)) {
					*target = flag
					return nil
				}
			}
		}
		return nil
	}
}
