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

package net

import (
	"encoding/json"
	"net"
	"net/http"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"
)

// PodRegistrationRequest matches the request structure sent by queue-proxy
type PodRegistrationRequest struct {
	PodName   string `json:"pod_name"`
	PodIP     string `json:"pod_ip"`
	Namespace string `json:"namespace"`
	Revision  string `json:"revision"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
}

// PodRegistrationThrottler defines the interface for throttler operations needed by the pod registration handler
type PodRegistrationThrottler interface {
	HandlePodRegistration(revID types.NamespacedName, podIP string, eventType string, logger *zap.SugaredLogger)
}

// PodRegistrationHandler creates an HTTP handler for receiving pod registration notifications
// The handler integrates with the throttler to create/update pod trackers on startup and ready events
func PodRegistrationHandler(throttler PodRegistrationThrottler, logger *zap.SugaredLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req PodRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warnw("Failed to decode pod registration request",
				"error", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.PodName == "" || req.PodIP == "" || req.Namespace == "" || req.Revision == "" || req.EventType == "" {
			logger.Warnw("Pod registration request missing required fields",
				"pod_name", req.PodName,
				"pod_ip", req.PodIP,
				"namespace", req.Namespace,
				"revision", req.Revision,
				"event_type", req.EventType)
			http.Error(w, "Missing required fields", http.StatusBadRequest)
			return
		}

		// Validate event type is one of the expected values
		validEvents := map[string]bool{
			"startup":   true,
			"ready":     true,
			"not-ready": true,
			"draining":  true,
		}
		if !validEvents[req.EventType] {
			logger.Warnw("Invalid event type",
				"event_type", req.EventType,
				"pod_name", req.PodName,
				"pod_ip", req.PodIP)
			http.Error(w, "Invalid event_type: must be 'startup', 'ready', 'not-ready', or 'draining'", http.StatusBadRequest)
			return
		}

		// Validate pod IP is a valid IP address
		if net.ParseIP(req.PodIP) == nil {
			logger.Warnw("Invalid pod IP address",
				"pod_ip", req.PodIP,
				"pod_name", req.PodName,
				"event_type", req.EventType)
			http.Error(w, "Invalid pod_ip: must be a valid IP address", http.StatusBadRequest)
			return
		}

		revisionID := types.NamespacedName{
			Namespace: req.Namespace,
			Name:      req.Revision,
		}

		logger.Debugw("Received pod registration",
			"event_type", req.EventType,
			"pod_name", req.PodName,
			"pod_ip", req.PodIP,
			"revision", revisionID.String(),
			"timestamp", req.Timestamp)

		// Notify the throttler of the new pod discovery
		throttler.HandlePodRegistration(revisionID, req.PodIP, req.EventType, logger)

		// Return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "accepted",
		})
	}
}

// HandlePodRegistration is exported from the Throttler via a public method
// This allows pod registration requests to notify the throttler of pods
