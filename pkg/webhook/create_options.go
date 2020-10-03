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

package webhook

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	scheme "k8s.io/client-go/kubernetes/scheme"
	rest "k8s.io/client-go/rest"
)

// TODO(https://github.com/knative/serving/issues/7143) We will remove this entire file once we pull in
// k8s version 1.18 client libraries where CreateOptions is included officially.
// This file is mostly pieces backported from the current version of the client.
var newCreateWithOptions func(client rest.Interface, namespace string) podInterface = newPods

type pods struct {
	client rest.Interface
	ns     string
}

type podInterface interface {
	createWithOptions(ctx context.Context, pod *corev1.Pod, opts metav1.CreateOptions) (*corev1.Pod, error)
}

// newPods returns a Pods
func newPods(client rest.Interface, namespace string) podInterface {
	p := &pods{
		client: client,
		ns:     namespace,
	}
	return podInterface(p)
}

// CreateWithOptions takes the representation of a pod and creates it.
//Returns the server's representation of the pod, and an error, if there is any.
func (c *pods) createWithOptions(ctx context.Context, pod *corev1.Pod, opts metav1.CreateOptions) (result *corev1.Pod, err error) {
	result = &corev1.Pod{}
	err = c.client.Post().Namespace(c.ns).Resource("pods").VersionedParams(&opts, scheme.ParameterCodec).Body(pod).Do(ctx).Into(result)
	return
}
