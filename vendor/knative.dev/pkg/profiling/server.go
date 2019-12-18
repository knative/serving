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

package profiling

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strconv"
	"sync"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

const (
	// ProfilingPort specifies the port where profiling data is available when profiling is enabled
	ProfilingPort = 8008

	// profilingKey is the name of the key in config-observability config map
	// that indicates whether profiling is enabled
	profilingKey = "profiling.enable"
)

// Handler holds the main HTTP handler and a flag indicating
// whether the handler is active
type Handler struct {
	enabled    bool
	enabledMux sync.Mutex
	handler    http.Handler
	log        *zap.SugaredLogger
}

// NewHandler create a new ProfilingHandler which serves runtime profiling data
// according to the given context path
func NewHandler(logger *zap.SugaredLogger, enableProfiling bool) *Handler {
	const pprofPrefix = "/debug/pprof/"

	mux := http.NewServeMux()
	mux.HandleFunc(pprofPrefix, pprof.Index)
	mux.HandleFunc(pprofPrefix+"cmdline", pprof.Cmdline)
	mux.HandleFunc(pprofPrefix+"profile", pprof.Profile)
	mux.HandleFunc(pprofPrefix+"symbol", pprof.Symbol)
	mux.HandleFunc(pprofPrefix+"trace", pprof.Trace)

	logger.Infof("Profiling enabled: %t", enableProfiling)

	return &Handler{
		enabled: enableProfiling,
		handler: mux,
		log:     logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.enabledMux.Lock()
	defer h.enabledMux.Unlock()
	if h.enabled {
		h.handler.ServeHTTP(w, r)
	} else {
		http.NotFoundHandler().ServeHTTP(w, r)
	}
}

func ReadProfilingFlag(config map[string]string) (bool, error) {
	profiling, ok := config[profilingKey]
	if !ok {
		return false, nil
	}
	enabled, err := strconv.ParseBool(profiling)
	if err != nil {
		return false, fmt.Errorf("failed to parse the profiling flag: %w", err)
	}
	return enabled, nil
}

// UpdateFromConfigMap modifies the Enabled flag in the Handler
// according to the value in the given ConfigMap
func (h *Handler) UpdateFromConfigMap(configMap *corev1.ConfigMap) {
	enabled, err := ReadProfilingFlag(configMap.Data)
	if err != nil {
		h.log.Errorw("Failed to update the profiling flag", zap.Error(err))
		return
	}
	h.enabledMux.Lock()
	defer h.enabledMux.Unlock()
	if h.enabled != enabled {
		h.enabled = enabled
		h.log.Infof("Profiling enabled: %t", h.enabled)
	}
}

// NewServer creates a new http server that exposes profiling data on the default profiling port
func NewServer(handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    ":" + strconv.Itoa(ProfilingPort),
		Handler: handler,
	}
}
