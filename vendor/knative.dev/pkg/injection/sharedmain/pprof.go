/*
Copyright 2025 The Knative Authors

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

package sharedmain

import (
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/observability/runtime"
)

type pprofServer struct {
	*runtime.ProfilingServer
	log *zap.SugaredLogger
}

func newProfilingServer(logger *zap.SugaredLogger) *pprofServer {
	s := runtime.NewProfilingServer()

	return &pprofServer{
		ProfilingServer: s,
		log:             logger,
	}
}

func (s *pprofServer) UpdateFromConfigMap(cm *corev1.ConfigMap) {
	cfg, err := runtime.NewFromMap(cm.Data)
	if err != nil {
		s.log.Errorw("Failed to update the profiling flag", zap.Error(err))
		return
	}
	s.UpdateFromConfig(cfg)
}

func (s *pprofServer) UpdateFromConfig(cfg runtime.Config) {
	enabled := cfg.ProfilingEnabled()
	if s.ProfilingServer.SetEnabled(enabled) != enabled {
		s.log.Info("Profiling enabled: ", enabled)
	}
}
