/*
Copyright 2018 Google LLC.

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
	"context"
	"log"
	"time"

	"github.com/knative/serving/pkg/logging"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"

	// TODO(mattmoor): Move controller deps into receiver
	"github.com/knative/serving/pkg/controller"
)

// SyncEndpoint implements endpoint.Receiver
func (c *Receiver) SyncEndpoint(endpoint *corev1.Endpoints) error {
	eName := endpoint.Name
	namespace := endpoint.Namespace

	// Lookup and see if this endpoints corresponds to a service that
	// we own and hence the Revision that created this service.
	revName := lookupServiceOwner(endpoint)
	if revName == "" {
		return nil
	}
	logger := loggerWithRevisionInfo(c.Logger, namespace, revName)
	ctx := logging.WithLogger(context.TODO(), logger)

	rev, err := c.lister.Revisions(namespace).Get(revName)
	if err != nil {
		logger.Error("Error fetching revision", zap.Error(err))
		return err
	}

	// Check to see if endpoint is the service endpoint
	if eName != controller.GetElaK8SServiceNameForRevision(rev) {
		return nil
	}

	// Check to see if the revision has already been marked as ready or failed
	// and if it is, then there's no need to do anything to it.
	if rev.Status.IsReady() || rev.Status.IsFailed() {
		return nil
	}
	// Don't modify the informer's copy.
	rev = rev.DeepCopy()

	if getIsServiceReady(endpoint) {
		logger.Infof("Endpoint %q is ready", eName)
		if err := c.markRevisionReady(ctx, rev); err != nil {
			logger.Error("Error marking revision ready", zap.Error(err))
			return err
		}
		c.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady", "Revision becomes ready upon endpoint %q becoming ready", endpoint.Name)
		return nil
	}

	revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
	if revisionAge < serviceTimeoutDuration {
		log.Printf("rev %v vs. duration %v", revisionAge, serviceTimeoutDuration)
		return nil
	}

	if err := c.markRevisionFailed(ctx, rev); err != nil {
		logger.Error("Error marking revision failed", zap.Error(err))
		return err
	}
	c.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed", "Revision did not become ready due to endpoint %q", endpoint.Name)
	return nil
}
