package queue

import (
	"net/http"

	"github.com/knative/serving/pkg/apis/serving"
	"go.uber.org/zap"
)

func PokeActivator(logger *zap.SugaredLogger, namespace, revision string) {
	req, err := http.NewRequest("GET", "http://activator-service.knative-serving.svc.cluster.local:8082", nil)
	if err != nil {
		logger.Errorw("Failed to create request to poke activator", zap.Error(err))
		return
	}
	req.Header = map[string][]string{
		serving.ActivatorRevisionHeaderNamespace: []string{namespace},
		serving.ActivatorRevisionHeaderName:      []string{revision},
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Errorw("Failed to make request to poke activator", zap.Error(err))
		return
	}
	logger.Errorf("Poked activator: %v", resp)
}
