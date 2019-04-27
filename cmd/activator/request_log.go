package main

import (
	"net/http"

	"github.com/knative/serving/pkg/activator"
	"github.com/knative/serving/pkg/apis/serving"
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

func requestLogTemplateInputGetter(getRevision activator.RevisionGetter) pkghttp.RequestLogTemplateInputGetter {
	return func(req *http.Request, resp *pkghttp.RequestLogResponse) *pkghttp.RequestLogTemplateInput {
		namespace := pkghttp.LastHeaderValue(req.Header, activator.RevisionHeaderNamespace)
		name := pkghttp.LastHeaderValue(req.Header, activator.RevisionHeaderName)
		revInfo := &pkghttp.RequestLogRevision{
			Namespace: namespace,
			Name:      name,
		}

		revision, err := getRevision(activator.RevisionID{Namespace: namespace, Name: name})
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
