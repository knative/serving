/*
Copyright 2018 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

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
        "reflect"
        "time"

        "go.uber.org/zap"
        "k8s.io/apimachinery/pkg/api/equality"

        "github.com/google/go-cmp/cmp"
        "github.com/google/go-cmp/cmp/cmpopts"

        "github.com/knative/serving/pkg/apis/serving/v1alpha1"
        "github.com/knative/serving/pkg/autoscaler"
        "github.com/knative/serving/pkg/controller/revision/config"
        "github.com/knative/serving/pkg/controller/revision/resources"
        resourcenames "github.com/knative/serving/pkg/controller/revision/resources/names"
        "github.com/knative/serving/pkg/logging"
        "github.com/knative/serving/pkg/logging/logkey"
        "github.com/knative/serving/pkg/system"
        
        appsv1 "k8s.io/api/apps/v1"
        corev1 "k8s.io/api/core/v1"
        apierrs "k8s.io/apimachinery/pkg/api/errors"
        "k8s.io/apimachinery/pkg/api/resource"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        vpav1alpha1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/poc.autoscaling.k8s.io/v1alpha1"
)

func (c *Controller) reconcileDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
        ns := rev.Namespace
        deploymentName := resourcenames.Deployment(rev)
        logger := logging.FromContext(ctx).With(zap.String(logkey.Deployment, deploymentName))

        deployment, getDepErr := c.deploymentLister.Deployments(ns).Get(deploymentName)
        switch rev.Spec.ServingState {
        case v1alpha1.RevisionServingStateActive, v1alpha1.RevisionServingStateReserve:
                // When Active or Reserved, deployment should exist and have a particular specification.
                if apierrs.IsNotFound(getDepErr) {
                        // Deployment does not exist. Create it.
                        rev.Status.MarkDeploying("Deploying")
                        var err error
                        deployment, err = c.createDeployment(ctx, rev)
                        if err != nil {
                                logger.Errorf("Error creating deployment %q: %v", deploymentName, err)
                                return err
                        }
                        logger.Infof("Created deployment %q", deploymentName)
                } else if getDepErr != nil {
                        logger.Errorf("Error reconciling deployment %q: %v", deploymentName, getDepErr)
                        return getDepErr
                } else {
                        // Deployment exist. Update the replica count based on the serving state if necessary
                        var changed Changed
                        var err error
                        deployment, changed, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
                        if err != nil {
                                logger.Errorf("Error updating deployment %q: %v", deploymentName, err)
                                return err
                        }
                        if changed == WasChanged {
                                logger.Infof("Updated deployment %q", deploymentName)
                                rev.Status.MarkDeploying("Updating")
                        }
                }

                // Now that we have a Deployment, determine whether there is any relevant
                // status to surface in the Revision.
                if hasDeploymentTimedOut(deployment) {
                        rev.Status.MarkProgressDeadlineExceeded(fmt.Sprintf(
                                "Unable to create pods for more than %d seconds.", resources.ProgressDeadlineSeconds))
                        c.Recorder.Eventf(rev, corev1.EventTypeNormal, "ProgressDeadlineExceeded",
                                "Revision %s not ready due to Deployment timeout", rev.Name)
                }
                return nil

        case v1alpha1.RevisionServingStateRetired:
                // When Retired, we remove the underlying Deployment.
                if apierrs.IsNotFound(getDepErr) {
                        // If it does not exist, then we have nothing to do.
                        return nil
                }
                if err := c.deleteDeployment(ctx, deployment); err != nil {
                        logger.Errorf("Error deleting deployment %q: %v", deploymentName, err)
                        return err
                }
                logger.Infof("Deleted deployment %q", deploymentName)
                rev.Status.MarkInactive()
                return nil

        default:
                logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                return nil
        }
}

func (c *Controller) createDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
        logger := logging.FromContext(ctx)

        var replicaCount int32 = 1
        if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve {
                replicaCount = 0
        }
        deployment := resources.MakeDeployment(rev, c.getLoggingConfig(), c.getNetworkConfig(),
                c.getObservabilityConfig(), c.getAutoscalerConfig(), c.getControllerConfig(), replicaCount)

        // Resolve tag image references to digests.
        if err := c.getResolver().Resolve(deployment); err != nil {
                logger.Error("Error resolving deployment", zap.Error(err))
                rev.Status.MarkContainerMissing(err.Error())
                return nil, fmt.Errorf("Error resolving container to digest: %v", err)
        }

        return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Controller) getObservabilityConfig() *config.Observability {
        c.observabilityConfigMutex.Lock()
        defer c.observabilityConfigMutex.Unlock()
        return c.observabilityConfig
}

func (c *Controller) getNetworkConfig() *config.Network {
        c.networkConfigMutex.Lock()
        defer c.networkConfigMutex.Unlock()
        return c.networkConfig
}

func (c *Controller) getResolver() resolver {
        c.resolverMutex.Lock()
        defer c.resolverMutex.Unlock()
        return c.resolver
}

func (c *Controller) getLoggingConfig() *logging.Config {
        c.loggingConfigMutex.Lock()
        defer c.loggingConfigMutex.Unlock()
        return c.loggingConfig
}

// This is a generic function used both for deployment of user code & autoscaler
func (c *Controller) checkAndUpdateDeployment(ctx context.Context, rev *v1alpha1.Revision, deployment *appsv1.Deployment) (*appsv1.Deployment, Changed, error) {
        logger := logging.FromContext(ctx)

        // TODO(mattmoor): Generalize this to reconcile discrepancies vs. what
        // resources.MakeDeployment() would produce.
        desiredDeployment := deployment.DeepCopy()
        if desiredDeployment.Spec.Replicas == nil {
                var one int32 = 1
                desiredDeployment.Spec.Replicas = &one
        }
        if rev.Spec.ServingState == v1alpha1.RevisionServingStateActive && *desiredDeployment.Spec.Replicas == 0 {
                *desiredDeployment.Spec.Replicas = 1
        } else if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve && *desiredDeployment.Spec.Replicas != 0 {
                *desiredDeployment.Spec.Replicas = 0
        }

        if equality.Semantic.DeepEqual(desiredDeployment.Spec, deployment.Spec) {
                return deployment, Unchanged, nil
        }
        logger.Infof("Reconciling deployment diff (-desired, +observed): %v",
                cmp.Diff(desiredDeployment.Spec, deployment.Spec, cmpopts.IgnoreUnexported(resource.Quantity{})))
        deployment.Spec = desiredDeployment.Spec
        d, err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Update(deployment)
        return d, WasChanged, err
}

// This is a generic function used both for deployment of user code & autoscaler
func (c *Controller) deleteDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
        logger := logging.FromContext(ctx)

        err := c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Delete(deployment.Name, fgDeleteOptions)
        if apierrs.IsNotFound(err) {
                return nil
        } else if err != nil {
                logger.Errorf("deployments.Delete for %q failed: %s", deployment.Name, err)
                return err
        }
        return nil
}

func (c *Controller) reconcileService(ctx context.Context, rev *v1alpha1.Revision) error {
        ns := rev.Namespace
        serviceName := resourcenames.K8sService(rev)
        logger := logging.FromContext(ctx).With(zap.String(logkey.KubernetesService, serviceName))

        rev.Status.ServiceName = serviceName

        service, err := c.serviceLister.Services(ns).Get(serviceName)
        switch rev.Spec.ServingState {
        case v1alpha1.RevisionServingStateActive:
                // When Active, the Service should exist and have a particular specification.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then create it.
                        rev.Status.MarkDeploying("Deploying")
                        service, err = c.createService(ctx, rev, resources.MakeK8sService)
                        if err != nil {
                                logger.Errorf("Error creating Service %q: %v", serviceName, err)
                                return err
                        }
                        logger.Infof("Created Service %q", serviceName)
                } else if err != nil {
                        logger.Errorf("Error reconciling Active Service %q: %v", serviceName, err)
                        return err
                } else {
                        // If it exists, then make sure if looks as we expect.
                        // It may change if a user edits things around our controller, which we
                        // should not allow, or if our expectations of how the service should look
                        // changes (e.g. we update our controller with new sidecars).
                        var changed Changed
                        service, changed, err = c.checkAndUpdateService(ctx, rev, resources.MakeK8sService, service)
                        if err != nil {
                                logger.Errorf("Error updating Service %q: %v", serviceName, err)
                                return err
                        }
                        if changed == WasChanged {
                                logger.Infof("Updated Service %q", serviceName)
                                rev.Status.MarkDeploying("Updating")
                        }
                }

                // We cannot determine readiness from the Service directly.  Instead, we look up
                // the backing Endpoints resource and check it for healthy pods.  The name of the
                // Endpoints resource matches the Service it backs.
                endpoints, err := c.endpointsLister.Endpoints(ns).Get(serviceName)
                if apierrs.IsNotFound(err) {
                        // If it isn't found, then we need to wait for the Service controller to
                        // create it.
                        logger.Infof("Endpoints not created yet %q", serviceName)
                        rev.Status.MarkDeploying("Deploying")
                        return nil
                } else if err != nil {
                        logger.Errorf("Error checking Active Endpoints %q: %v", serviceName, err)
                        return err
                }
                // If the endpoints resource indicates that the Service it sits in front of is ready,
                // then surface this in our Revision status as resources available (pods were scheduled)
                // and container healthy (endpoints should be gated by any provided readiness checks).
                if getIsServiceReady(endpoints) {
                        rev.Status.MarkResourcesAvailable()
                        rev.Status.MarkContainerHealthy()
                        // TODO(mattmoor): How to ensure this only fires once?
                        c.Recorder.Eventf(rev, corev1.EventTypeNormal, "RevisionReady",
                                "Revision becomes ready upon endpoint %q becoming ready", serviceName)
                } else {
                        // If the endpoints is NOT ready, then check whether it is taking unreasonably
                        // long to become ready and if so mark our revision as having timed out waiting
                        // for the Service to become ready.
                        revisionAge := time.Now().Sub(getRevisionLastTransitionTime(rev))
                        if revisionAge >= serviceTimeoutDuration {
                                rev.Status.MarkServiceTimeout()
                                // TODO(mattmoor): How to ensure this only fires once?
                                c.Recorder.Eventf(rev, corev1.EventTypeWarning, "RevisionFailed",
                                        "Revision did not become ready due to endpoint %q", serviceName)
                        }
                }
                return nil

        case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
                // When Reserve or Retired, we remove the underlying Service.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then we have nothing to do.
                        return nil
                }
                if err := c.deleteService(ctx, service); err != nil {
                        logger.Errorf("Error deleting Service %q: %v", serviceName, err)
                        return err
                }
                logger.Infof("Deleted Service %q", serviceName)
                rev.Status.MarkInactive()
                return nil

        default:
                logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                return nil
        }
}

type serviceFactory func(*v1alpha1.Revision) *corev1.Service

func (c *Controller) createService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory) (*corev1.Service, error) {
        // Create the service.
        service := sf(rev)

        return c.KubeClientSet.CoreV1().Services(service.Namespace).Create(service)
}

func (c *Controller) checkAndUpdateService(ctx context.Context, rev *v1alpha1.Revision, sf serviceFactory, service *corev1.Service) (*corev1.Service, Changed, error) {
        logger := logging.FromContext(ctx)

        desiredService := sf(rev)

        // Preserve the ClusterIP field in the Service's Spec, if it has been set.
        desiredService.Spec.ClusterIP = service.Spec.ClusterIP

        if equality.Semantic.DeepEqual(desiredService.Spec, service.Spec) {
                return service, Unchanged, nil
        }
        logger.Infof("Reconciling service diff (-desired, +observed): %v",
                cmp.Diff(desiredService.Spec, service.Spec))
        service.Spec = desiredService.Spec

        d, err := c.KubeClientSet.CoreV1().Services(service.Namespace).Update(service)
        return d, WasChanged, err
}

func (c *Controller) deleteService(ctx context.Context, svc *corev1.Service) error {
        logger := logging.FromContext(ctx)

        err := c.KubeClientSet.CoreV1().Services(svc.Namespace).Delete(svc.Name, fgDeleteOptions)
        if apierrs.IsNotFound(err) {
                return nil
        } else if err != nil {
                logger.Errorf("service.Delete for %q failed: %s", svc.Name, err)
                return err
        }
        return nil
}

func (c *Controller) reconcileFluentdConfigMap(ctx context.Context, rev *v1alpha1.Revision) error {
        logger := logging.FromContext(ctx)
        if !c.getObservabilityConfig().EnableVarLogCollection {
                return nil
        }
        ns := rev.Namespace
        name := resourcenames.FluentdConfigMap(rev)

        configMap, err := c.configMapLister.ConfigMaps(ns).Get(name)
        if apierrs.IsNotFound(err) {
                // ConfigMap doesn't exist, going to create it
                desiredConfigMap := resources.MakeFluentdConfigMap(rev, c.getObservabilityConfig())
                configMap, err = c.KubeClientSet.CoreV1().ConfigMaps(ns).Create(desiredConfigMap)
                if err != nil {
                        logger.Error("Error creating fluentd configmap", zap.Error(err))
                        return err
                }
                logger.Infof("Created fluentd configmap: %q", name)
        } else if err != nil {
                logger.Errorf("configmaps.Get for %q failed: %s", name, err)
                return err
        } else {
                desiredConfigMap := resources.MakeFluentdConfigMap(rev, c.getObservabilityConfig())
                if !equality.Semantic.DeepEqual(configMap.Data, desiredConfigMap.Data) {
                        logger.Infof("Reconciling fluentd configmap diff (-desired, +observed): %v",
                                cmp.Diff(desiredConfigMap.Data, configMap.Data))
                        configMap.Data = desiredConfigMap.Data
                        configMap, err = c.KubeClientSet.CoreV1().ConfigMaps(ns).Update(desiredConfigMap)
                        if err != nil {
                                logger.Error("Error updating fluentd configmap", zap.Error(err))
                                return err
                        }
                }
        }
        return nil
}

func (c *Controller) reconcileAutoscalerService(ctx context.Context, rev *v1alpha1.Revision) error {
        // If an autoscaler image is undefined, then skip the autoscaler reconciliation.
        if c.getControllerConfig().AutoscalerImage == "" {
                return nil
        }

        ns := system.Namespace
        serviceName := resourcenames.Autoscaler(rev)
        logger := logging.FromContext(ctx).With(zap.String(logkey.KubernetesService, serviceName))

        service, err := c.serviceLister.Services(ns).Get(serviceName)
        switch rev.Spec.ServingState {
        case v1alpha1.RevisionServingStateActive:
                // When Active, the Service should exist and have a particular specification.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then create it.
                        service, err = c.createService(ctx, rev, resources.MakeAutoscalerService)
                        if err != nil {
                                logger.Errorf("Error creating Autoscaler Service %q: %v", serviceName, err)
                                return err
                        }
                        logger.Infof("Created Autoscaler Service %q", serviceName)
                } else if err != nil {
                        logger.Errorf("Error reconciling Active Autoscaler Service %q: %v", serviceName, err)
                        return err
                } else {
                        // If it exists, then make sure if looks as we expect.
                        // It may change if a user edits things around our controller, which we
                        // should not allow, or if our expectations of how the service should look
                        // changes (e.g. we update our controller with new sidecars).
                        var changed Changed
                        service, changed, err = c.checkAndUpdateService(
                                ctx, rev, resources.MakeAutoscalerService, service)
                        if err != nil {
                                logger.Errorf("Error updating Autoscaler Service %q: %v", serviceName, err)
                                return err
                        }
                        if changed == WasChanged {
                                logger.Infof("Updated Autoscaler Service %q", serviceName)
                        }
                }

                // TODO(mattmoor): We don't predicate the Revision's readiness on any readiness
                // properties of the autoscaler, but perhaps we should.
                return nil

        case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
                // When Reserve or Retired, we remove the autoscaling Service.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then we have nothing to do.
                        return nil
                }
                if err := c.deleteService(ctx, service); err != nil {
                        logger.Errorf("Error deleting Autoscaler Service %q: %v", serviceName, err)
                        return err
                }
                logger.Infof("Deleted Autoscaler Service %q", serviceName)
                return nil

        default:
                logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                return nil
        }
}

func (c *Controller) reconcileAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) error {
        // If an autoscaler image is undefined, then skip the autoscaler reconciliation.
        if c.getControllerConfig().AutoscalerImage == "" {
                return nil
        }

        ns := system.Namespace
        deploymentName := resourcenames.Autoscaler(rev)
        logger := logging.FromContext(ctx).With(zap.String(logkey.Deployment, deploymentName))

        deployment, getDepErr := c.deploymentLister.Deployments(ns).Get(deploymentName)
        switch rev.Spec.ServingState {
        case v1alpha1.RevisionServingStateActive, v1alpha1.RevisionServingStateReserve:
                // When Active or Reserved, Autoscaler deployment should exist and have a particular specification.
                if apierrs.IsNotFound(getDepErr) {
                        // Deployment does not exist. Create it.
                        var err error
                        deployment, err = c.createAutoscalerDeployment(ctx, rev)
                        if err != nil {
                                logger.Errorf("Error creating Autoscaler deployment %q: %v", deploymentName, err)
                                return err
                        }
                        logger.Infof("Created Autoscaler deployment %q", deploymentName)
                } else if getDepErr != nil {
                        logger.Errorf("Error reconciling Autoscaler deployment %q: %v", deploymentName, getDepErr)
                        return getDepErr
                } else {
                        // Deployment exist. Update the replica count based on the serving state if necessary
                        var err error
                        deployment, _, err = c.checkAndUpdateDeployment(ctx, rev, deployment)
                        if err != nil {
                                logger.Errorf("Error updating deployment %q: %v", deploymentName, err)
                                return err
                        }
                }

                // TODO(mattmoor): We don't predicate the Revision's readiness on any readiness
                // properties of the autoscaler, but perhaps we should.
                return nil

        case v1alpha1.RevisionServingStateRetired:
                // When Reserve or Retired, we remove the underlying Autoscaler Deployment.
                if apierrs.IsNotFound(getDepErr) {
                        // If it does not exist, then we have nothing to do.
                        return nil
                }
                if err := c.deleteDeployment(ctx, deployment); err != nil {
                        logger.Errorf("Error deleting Autoscaler Deployment %q: %v", deploymentName, err)
                        return err
                }
                logger.Infof("Deleted Autoscaler Deployment %q", deploymentName)
                return nil

        default:
                logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                return nil
        }
}

func (c *Controller) createAutoscalerDeployment(ctx context.Context, rev *v1alpha1.Revision) (*appsv1.Deployment, error) {
        var replicaCount int32 = 1
        if rev.Spec.ServingState == v1alpha1.RevisionServingStateReserve {
                replicaCount = 0
        }
        deployment := resources.MakeAutoscalerDeployment(rev, c.getControllerConfig().AutoscalerImage, replicaCount)
        return c.KubeClientSet.AppsV1().Deployments(deployment.Namespace).Create(deployment)
}

func (c *Controller) getControllerConfig() *config.Controller {
        c.controllerConfigMutex.Lock()
        defer c.controllerConfigMutex.Unlock()
        return c.controllerConfig
}

func (c *Controller) reconcileVPA(ctx context.Context, rev *v1alpha1.Revision) error {
        logger := logging.FromContext(ctx)
        if !c.getAutoscalerConfig().EnableVPA {
                return nil
        }

        ns := rev.Namespace
        vpaName := resourcenames.VPA(rev)

        // TODO(mattmoor): Switch to informer lister once it can reliably be sunk.
        vpa, err := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(ns).Get(vpaName, metav1.GetOptions{})
        switch rev.Spec.ServingState {
        case v1alpha1.RevisionServingStateActive:
                // When Active, the VPA should exist and have a particular specification.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then create it.
                        vpa, err = c.createVPA(ctx, rev)
                        if err != nil {
                                logger.Errorf("Error creating VPA %q: %v", vpaName, err)
                                return err
                        }
                        logger.Infof("Created VPA %q", vpaName)
                } else if err != nil {
                        logger.Errorf("Error reconciling Active VPA %q: %v", vpaName, err)
                        return err
                } else {
                        // TODO(mattmoor): Should we checkAndUpdate the VPA, or would it
                        // suffer similar problems to Deployment?
                }

                // TODO(mattmoor): We don't predicate the Revision's readiness on any readiness
                // properties of the autoscaler, but perhaps we should.
                return nil

        case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
                // When Reserve or Retired, we remove the underlying VPA.
                if apierrs.IsNotFound(err) {
                        // If it does not exist, then we have nothing to do.
                        return nil
                }
                if err := c.deleteVPA(ctx, vpa); err != nil {
                        logger.Errorf("Error deleting VPA %q: %v", vpaName, err)
                        return err
                }
                logger.Infof("Deleted VPA %q", vpaName)
                return nil

        default:
                logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                return nil
        }
}

func (c *Controller) createVPA(ctx context.Context, rev *v1alpha1.Revision) (*vpav1alpha1.VerticalPodAutoscaler, error) {
        vpa := resources.MakeVPA(rev)

        return c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(vpa.Namespace).Create(vpa)
}

func (c *Controller) deleteVPA(ctx context.Context, vpa *vpav1alpha1.VerticalPodAutoscaler) error {
        logger := logging.FromContext(ctx)
        if !c.getAutoscalerConfig().EnableVPA {
                return nil
        }

        err := c.vpaClient.PocV1alpha1().VerticalPodAutoscalers(vpa.Namespace).Delete(vpa.Name, fgDeleteOptions)
        if apierrs.IsNotFound(err) {
                return nil
        } else if err != nil {
                logger.Errorf("vpa.Delete for %q failed: %v", vpa.Name, err)
                return err
        }
        return nil
}

func (c *Controller) getAutoscalerConfig() *autoscaler.Config {
        c.autoscalerConfigMutex.Lock()
        defer c.autoscalerConfigMutex.Unlock()
        return c.autoscalerConfig
}

func (c *Controller) updateStatus(rev *v1alpha1.Revision) (*v1alpha1.Revision, error) {
        newRev, err := c.revisionLister.Revisions(rev.Namespace).Get(rev.Name)
        if err != nil {
                return nil, err
        }
        // Check if there is anything to update.
        if !reflect.DeepEqual(newRev.Status, rev.Status) {
                newRev.Status = rev.Status

                // TODO: for CRD there's no updatestatus, so use normal update
                return c.ServingClientSet.ServingV1alpha1().Revisions(rev.Namespace).Update(newRev)
                //      return prClient.UpdateStatus(newRev)
        }
        return rev, nil
}
