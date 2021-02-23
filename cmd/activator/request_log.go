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

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/metrics"
	"knative.dev/serving/pkg/activator/handler"
	"knative.dev/serving/pkg/apis/serving"
	pkghttp "knative.dev/serving/pkg/http"
)

func updateRequestLogFromConfigMap(logger *zap.SugaredLogger, h *pkghttp.RequestLogHandler) func(configMap *corev1.ConfigMap) {
	return func(configMap *corev1.ConfigMap) {
		obsconfig, err := metrics.NewObservabilityConfigFromConfigMap(configMap)
		if err != nil {
			logger.Errorw("Failed to get observability configmap.", zap.Error(err), "configmap", configMap)
			return
		}

		var newTemplate string
		if obsconfig.EnableRequestLog {
			newTemplate = obsconfig.RequestLogTemplate
		}
		if err := h.SetTemplate(newTemplate); err != nil {
			logger.Errorw("Failed to update the request log template.", zap.Error(err), "template", newTemplate)
		} else {
			logger.Infow("Updated the request log template.", "template", newTemplate)
		}
	}
}

// requestLogTemplateInputGetter gets the template input from the request.
// It assumes the Revision has been set on the context such that
// WithRevision() returns a non-nil revision.
func requestLogTemplateInputGetter(req *http.Request, resp *pkghttp.RequestLogResponse) *pkghttp.RequestLogTemplateInput {
	revision := handler.RevisionFrom(req.Context())

	revInfo := &pkghttp.RequestLogRevision{
		Namespace: revision.Namespace,
		Name:      revision.Name,
	}

	if revision.Labels != nil {
		revInfo.Configuration = revision.Labels[serving.ConfigurationLabelKey]
		revInfo.Service = revision.Labels[serving.ServiceLabelKey]
	}

	return &pkghttp.RequestLogTemplateInput{
		Request:  req,
		Response: resp,
		Revision: revInfo,
	}
}
