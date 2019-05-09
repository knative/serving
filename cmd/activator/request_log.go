/*
Copyright 2019 The Knative Authors

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
	"net/http"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/serving"
	servinglisters "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	pkghttp "github.com/knative/serving/pkg/http"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

func updateRequestLogFromConfigMap(logger *zap.SugaredLogger, h *pkghttp.RequestLogHandler) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		newTemplate := configMap.Data["logging.request-log-template"]
		if err := h.SetTemplate(newTemplate); err != nil {
			logger.Errorw("Failed to update the request log template.", zap.Error(err), "template", newTemplate)
		} else {
			logger.Infow("Updated the request log template.", "template", newTemplate)
		}
	}
}

func requestLogTemplateInputGetter(revisionLister servinglisters.RevisionLister) pkghttp.RequestLogTemplateInputGetter {
	return func(req *http.Request, resp *pkghttp.RequestLogResponse) *pkghttp.RequestLogTemplateInput {
		namespace := pkghttp.LastHeaderValue(req.Header, activator.RevisionHeaderNamespace)
		name := pkghttp.LastHeaderValue(req.Header, activator.RevisionHeaderName)
		revInfo := &pkghttp.RequestLogRevision{
			Namespace: namespace,
			Name:      name,
		}

		revision, err := revisionLister.Revisions(namespace).Get(name)
		if err == nil && revision.Labels != nil {
			revInfo.Configuration = revision.Labels[serving.ConfigurationLabelKey]
			revInfo.Service = revision.Labels[serving.ServiceLabelKey]
		}

		return &pkghttp.RequestLogTemplateInput{
			Request:  req,
			Response: resp,
			Revision: revInfo,
		}
	}
}
