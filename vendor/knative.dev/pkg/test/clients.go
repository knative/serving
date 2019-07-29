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

// This file contains an object which encapsulates k8s clients which are useful for e2e tests.

package test

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8styped "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
)

// KubeClient holds instances of interfaces for making requests to kubernetes client.
type KubeClient struct {
	Kube *kubernetes.Clientset
}

// NewSpoofingClient returns a spoofing client to make requests
func NewSpoofingClient(client *KubeClient, logf logging.FormatLogger, domain string, resolvable bool) (*spoof.SpoofingClient, error) {
	return spoof.New(client.Kube, logf, domain, resolvable, Flags.IngressEndpoint)
}

// NewKubeClient instantiates and returns several clientsets required for making request to the
// kube client specified by the combination of clusterName and configPath. Clients can make requests within namespace.
func NewKubeClient(configPath string, clusterName string) (*KubeClient, error) {
	cfg, err := BuildClientConfig(configPath, clusterName)
	if err != nil {
		return nil, err
	}

	k, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &KubeClient{Kube: k}, nil
}

// BuildClientConfig builds the client config specified by the config path and the cluster name
func BuildClientConfig(kubeConfigPath string, clusterName string) (*rest.Config, error) {
	overrides := clientcmd.ConfigOverrides{}
	// Override the cluster name if provided.
	if clusterName != "" {
		overrides.Context.Cluster = clusterName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&overrides).ClientConfig()
}

// UpdateConfigMap updates the config map for specified @name with values
func (client *KubeClient) UpdateConfigMap(name string, configName string, values map[string]string) error {
	configMap, err := client.GetConfigMap(name).Get(configName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for key, value := range values {
		configMap.Data[key] = value
	}

	_, err = client.GetConfigMap(name).Update(configMap)
	return err
}

// GetConfigMap gets the knative serving config map.
func (client *KubeClient) GetConfigMap(name string) k8styped.ConfigMapInterface {
	return client.Kube.CoreV1().ConfigMaps(name)
}

// CreatePod will create a Pod
func (client *KubeClient) CreatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	pods := client.Kube.CoreV1().Pods(pod.GetNamespace())
	return pods.Create(pod)
}

// PodLogs returns Pod logs for given Pod and Container in the namespace
func (client *KubeClient) PodLogs(podName, containerName, namespace string) ([]byte, error) {
	pods := client.Kube.CoreV1().Pods(namespace)
	podList, err := pods.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, podName) {
			result := pods.GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: containerName,
			}).Do()
			return result.Raw()
		}
	}
	return nil, fmt.Errorf("could not find logs for %s/%s", podName, containerName)
}
