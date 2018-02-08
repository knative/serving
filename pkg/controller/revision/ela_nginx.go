/*
Copyright 2017 The Kubernetes Authors.

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
	"bytes"
	"log"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	"github.com/google/elafros/pkg/controller/util"

	corev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getNginxConfig(enableQueue bool) (string, error) {
	ctx := nginxConfigContext{
		EnableQueue: enableQueue,
	}
	var buf bytes.Buffer
	if err := nginxConfigTemplate.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MakeNginxConfigMap creates a ConfigMap that gets mounted for nginx container
// on the pod.
func MakeNginxConfigMap(u *v1alpha1.Revision, namespace string) (*corev1.ConfigMap, error) {
	// The request queue is disabled by default. To enable the queue, change this to true.
	var enableQueue = false
	log.Printf("Queue enabled: %t", enableQueue)
	nginxConfiguration, err := getNginxConfig(enableQueue)
	if err != nil {
		return &corev1.ConfigMap{}, err
	}

	return &corev1.ConfigMap{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      util.GetRevisionNginxConfigMapName(u),
			Namespace: namespace,
			Labels: map[string]string{
				elaServiceLabel: u.Spec.Service,
				elaVersionLabel: u.Name,
			},
		},
		Data: map[string]string{
			"nginx.conf": nginxConfiguration,
		},
	}, nil
}
