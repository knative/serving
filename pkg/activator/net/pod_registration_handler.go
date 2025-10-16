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

// PodRegistrationHandler creates an HTTP handler for receiving pod registration notifications
// The handler integrates with the throttler to create/update pod trackers on startup and ready events
func PodRegistrationHandler(throttler *Throttler, logger *zap.SugaredLogger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req PodRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Debugw("Failed to decode pod registration request",
				"error", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate required fields
		if req.PodName == "" || req.PodIP == "" || req.Namespace == "" || req.Revision == "" || req.EventType == "" {
			logger.Debugw("Pod registration request missing required fields",
				"pod_name", req.PodName,
				"pod_ip", req.PodIP,
				"namespace", req.Namespace,
				"revision", req.Revision,
				"event_type", req.EventType)
			http.Error(w, "Missing required fields", http.StatusBadRequest)
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
