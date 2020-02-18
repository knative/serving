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

package ingress

import (
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/client-go/kubernetes"
)

const (
	// IstioCanonicalServiceLabelName is the name of label for the Istio Canonical Service for a workload instance.
	IstioCanonicalServiceLabelName = "service.istio.io/canonical-name"

	// IstioCanonicalServiceRevisionLabelName is the name of label for the Istio Canonical Service revision for a workload instance.
	IstioCanonicalServiceRevisionLabelName = "service.istio.io/canonical-revision"
)

// LabelProvider provides a way to add istio labels to a PodSpec.
type LabelProvider struct {
	logger *zap.SugaredLogger

	client kubernetes.Interface
}

// NewLabelProvider provides a label provider
func NewLabelProvider(
	logger *zap.SugaredLogger, client kubernetes.Interface) *LabelProvider {
	return &LabelProvider{
		logger: logger,
		client: client,
	}
}

func (m *LabelProvider) process(oldObj, newObj interface{}) {
	m.logger.Infof("Received object: %#v", newObj)
	deployment, ok := newObj.(*appsv1.Deployment)

	if !ok {
		m.logger.Errorf("Object was not a deployment: %#v", newObj)
		return
	}

	newDeployment := addLabels(deployment)

	if deploymentsEqual(deployment, newDeployment) {
		return
	}

	m.updateDeployment(newDeployment)
}

func (m *LabelProvider) updateDeployment(deployment *appsv1.Deployment) {
	updatedDeployment, err := m.client.AppsV1().Deployments(deployment.Namespace).Update(deployment)

	if err != nil {
		m.logger.Errorf("Failed to update deployment: %v", err)
		return
	}

	m.logger.Infof("Updated deployment: %v", updatedDeployment)
}

func deploymentsEqual(oldDeployment, newDeployment *appsv1.Deployment) bool {
	return equality.Semantic.DeepEqual(newDeployment.Spec, oldDeployment.Spec)
}

func addLabels(deployment *appsv1.Deployment) *appsv1.Deployment {
	newDeployment := deployment.DeepCopy()

	serviceName := deployment.Labels["serving.knative.dev/service"]
	revisionName := deployment.Labels["serving.knative.dev/revision"]

	newDeployment.Labels[IstioCanonicalServiceLabelName] = serviceName
	newDeployment.Labels[IstioCanonicalServiceRevisionLabelName] = revisionName
	newDeployment.Spec.Template.Labels[IstioCanonicalServiceLabelName] = serviceName
	newDeployment.Spec.Template.Labels[IstioCanonicalServiceRevisionLabelName] = revisionName

	return newDeployment
}
