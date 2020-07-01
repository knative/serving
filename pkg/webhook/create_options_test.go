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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	rest "k8s.io/client-go/rest"
)

type tp struct{}

func newTestPods(client rest.Interface, namespace string) PodInterface {
	return PodInterface(&tp{})
}

func (*tp) CreateWithOptions(ctx context.Context,
	pod *corev1.Pod, opts metav1.CreateOptions) (result *corev1.Pod, err error) {
	return &corev1.Pod{}, nil
}

type tp2 struct{}

func newFailTestPods(client rest.Interface, namespace string) PodInterface {
	return PodInterface(&tp2{})
}

func (*tp2) CreateWithOptions(ctx context.Context,
	pod *corev1.Pod, opts metav1.CreateOptions) (result *corev1.Pod, err error) {
	return nil, errors.New("fail-reason")
}
