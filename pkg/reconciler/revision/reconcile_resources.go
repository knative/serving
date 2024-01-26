/*
Copyright 2018 The Knative Authors

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
	"fmt"

	"go.uber.org/zap"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/networking/pkg/certificates"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/networking"
	"knative.dev/serving/pkg/reconciler/revision/resources"
	resourcenames "knative.dev/serving/pkg/reconciler/revision/resources/names"
)

func (c *Reconciler) reconcileDeployment(ctx context.Context, rev *v1.Revision) error {
	ns := rev.Namespace
	deploymentName := resourcenames.Deployment(rev)
	logger := logging.FromContext(ctx).With(zap.String(logkey.Deployment, deploymentName))

	deployment, err := c.deploymentLister.Deployments(ns).Get(deploymentName)
	if apierrs.IsNotFound(err) {
		// Deployment does not exist. Create it.
		rev.Status.MarkResourcesAvailableUnknown(v1.ReasonDeploying, "")
		rev.Status.MarkContainerHealthyUnknown(v1.ReasonDeploying, "")
		if _, err = c.createDeployment(ctx, rev); err != nil {
			return fmt.Errorf("failed to create deployment %q: %w", deploymentName, err)
		}
		logger.Infof("Created deployment %q", deploymentName)
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to get deployment %q: %w", deploymentName, err)
	} else if !metav1.IsControlledBy(deployment, rev) {
		// Surface an error in the revision's status, and return an error.
		rev.Status.MarkResourcesAvailableFalse(v1.ReasonNotOwned, v1.ResourceNotOwnedMessage("Deployment", deploymentName))
		return fmt.Errorf("revision: %q does not own Deployment: %q", rev.Name, deploymentName)
	}

	// The deployment exists, but make sure that it has the shape that we expect.
	deployment, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
	if err != nil {
		return fmt.Errorf("failed to update deployment %q: %w", deploymentName, err)
	}

	rev.Status.PropagateDeploymentStatus(&deployment.Status)

	// If a container keeps crashing (no active pods in the deployment although we want some)
	if *deployment.Spec.Replicas > 0 && deployment.Status.AvailableReplicas == 0 {
		pods, err := c.kubeclient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector)})
		if err != nil {
			logger.Errorw("Error getting pods", zap.Error(err))
			return nil
		}
		if len(pods.Items) > 0 {
			// Arbitrarily grab the very first pod, as they all should be crashing
			pod := pods.Items[0]

			// Update the revision status if pod cannot be scheduled (possibly resource constraints)
			// If pod cannot be scheduled then we expect the container status to be empty.
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
					rev.Status.MarkResourcesAvailableFalse(cond.Reason, cond.Message)
					break
				}
			}

			for _, status := range pod.Status.ContainerStatuses {
				if status.Name == rev.Spec.GetContainer().Name {
					if t := status.LastTerminationState.Terminated; t != nil {
						logger.Infof("marking exiting with: %d/%s", t.ExitCode, t.Message)
						if t.ExitCode == 0 && t.Message == "" {
							// In cases where there is no error message, we should still provide some exit message in the status
							rev.Status.MarkContainerHealthyFalse(v1.ExitCodeReason(t.ExitCode),
								v1.RevisionContainerExitingMessage("container exited with no error"))
						} else {
							rev.Status.MarkContainerHealthyFalse(v1.ExitCodeReason(t.ExitCode), v1.RevisionContainerExitingMessage(t.Message))
						}
					} else if w := status.State.Waiting; w != nil && hasDeploymentTimedOut(deployment) {
						logger.Infof("marking resources unavailable with: %s: %s", w.Reason, w.Message)
						rev.Status.MarkResourcesAvailableFalse(w.Reason, w.Message)
					}
					break
				}
			}
		}
	}

	return nil
}

func (c *Reconciler) reconcileImageCache(ctx context.Context, rev *v1.Revision) error {
	logger := logging.FromContext(ctx)

	ns := rev.Namespace
	// Revisions are immutable.
	// Updating image results to new revision so there won't be any chance of resource leak.
	for _, container := range append(rev.Status.ContainerStatuses, rev.Status.InitContainerStatuses...) {
		imageName := kmeta.ChildName(resourcenames.ImageCache(rev), "-"+container.Name)
		if _, err := c.imageLister.Images(ns).Get(imageName); apierrs.IsNotFound(err) {
			if _, err := c.createImageCache(ctx, rev, container.Name, container.ImageDigest); err != nil {
				return fmt.Errorf("failed to create image cache %q: %w", imageName, err)
			}
			logger.Infof("Created image cache %q", imageName)
		} else if err != nil {
			return fmt.Errorf("failed to get image cache %q: %w", imageName, err)
		}
	}
	return nil
}

func (c *Reconciler) reconcilePA(ctx context.Context, rev *v1.Revision) error {
	ns := rev.Namespace

	deploymentName := resourcenames.Deployment(rev)
	deployment, err := c.deploymentLister.Deployments(ns).Get(deploymentName)
	if err != nil {
		return err
	}

	paName := resourcenames.PA(rev)
	logger := logging.FromContext(ctx)
	logger.Info("Reconciling PA: ", paName)

	pa, err := c.podAutoscalerLister.PodAutoscalers(ns).Get(paName)
	if apierrs.IsNotFound(err) {
		// PA does not exist. Create it.
		pa, err = c.createPA(ctx, rev, deployment)
		if err != nil {
			return fmt.Errorf("failed to create PA %q: %w", paName, err)
		}
		logger.Info("Created PA: ", paName)
	} else if err != nil {
		return fmt.Errorf("failed to get PA %q: %w", paName, err)
	} else if !metav1.IsControlledBy(pa, rev) {
		// Surface an error in the revision's status, and return an error.
		rev.Status.MarkResourcesAvailableFalse(v1.ReasonNotOwned, v1.ResourceNotOwnedMessage("PodAutoscaler", paName))
		return fmt.Errorf("revision: %q does not own PodAutoscaler: %q", rev.Name, paName)
	}

	// Perhaps tha PA spec changed underneath ourselves?
	// We no longer require immutability, so need to reconcile PA each time.
	tmpl := resources.MakePA(rev, deployment)
	logger.Debugf("Desired PASpec: %#v", tmpl.Spec)
	if !equality.Semantic.DeepEqual(tmpl.Spec, pa.Spec) {
		diff, _ := kmp.SafeDiff(tmpl.Spec, pa.Spec) // Can't realistically fail on PASpec.
		logger.Infof("PA %s needs reconciliation, diff(-want,+got):\n%s", pa.Name, diff)

		want := pa.DeepCopy()
		want.Spec = tmpl.Spec
		if pa, err = c.client.AutoscalingV1alpha1().PodAutoscalers(ns).Update(ctx, want, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update PA %q: %w", paName, err)
		}
	}

	logger.Debugf("Observed PA Status=%#v", pa.Status)
	rev.Status.PropagateAutoscalerStatus(&pa.Status)
	return nil
}

func hasDeploymentTimedOut(deployment *appsv1.Deployment) bool {
	// as per https://kubernetes.io/docs/concepts/workloads/controllers/deployment
	for _, cond := range deployment.Status.Conditions {
		// Look for a condition with status False
		if cond.Status != corev1.ConditionFalse {
			continue
		}
		// with Type Progressing and Reason Timeout
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == v1.ReasonProgressDeadlineExceeded {
			return true
		}
	}
	return false
}

func (c *Reconciler) reconcileSecret(ctx context.Context, rev *v1.Revision) error {
	ns := rev.Namespace
	logger := logging.FromContext(ctx)
	logger.Info("Reconciling Secret for system-internal-tls: ", networking.ServingCertName, " at namespace: ", ns)

	secret, err := c.kubeclient.CoreV1().Secrets(ns).Get(ctx, networking.ServingCertName, metav1.GetOptions{})
	if apierrs.IsNotFound(err) {
		namespace, err := c.kubeclient.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get Namespace %s: %w", ns, err)
		}
		if secret, err = c.createSecret(ctx, namespace); err != nil {
			return fmt.Errorf("failed to create Secret %s/%s: %w", networking.ServingCertName, ns, err)
		}
		logger.Info("Created Secret: ", networking.ServingCertName, "at namespace: ", ns)
	} else if err != nil {
		return fmt.Errorf("failed to get secret %s/%s: %w", networking.ServingCertName, ns, err)
	}

	// Verify if secret has been added the data.
	if _, ok := secret.Data[certificates.CertName]; !ok {
		return fmt.Errorf("public cert in the secret is not ready yet")
	}
	if _, ok := secret.Data[certificates.PrivateKeyName]; !ok {
		return fmt.Errorf("private key in the secret is not ready yet")
	}

	return nil
}
