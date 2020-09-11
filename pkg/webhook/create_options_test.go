/*
Copyright 2020 The Knative Authors.

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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
)

type tp struct{}

func newTestPods(client rest.Interface, namespace string) podInterface {
	return podInterface(&tp{})
}

func (*tp) createWithOptions(ctx context.Context,
	pod *corev1.Pod, opts metav1.CreateOptions) (result *corev1.Pod, err error) {
	return &corev1.Pod{}, nil
}

type tp2 struct{}

func newFailTestPods(client rest.Interface, namespace string) podInterface {
	return podInterface(&tp2{})
}

func (*tp2) createWithOptions(ctx context.Context,
	pod *corev1.Pod, opts metav1.CreateOptions) (result *corev1.Pod, err error) {
	return nil, errors.New("fail-reason")
}

func TestCreateWithOptions(t *testing.T) {
	ctx, _ := fakekubeclient.With(context.Background())
	client := kubeclient.Get(ctx)
	pod := &corev1.Pod{}
	client.CoreV1().Pods("namespace").Create(ctx, pod, metav1.CreateOptions{})

	newPods(client.CoreV1().RESTClient(), "namespace")
}
