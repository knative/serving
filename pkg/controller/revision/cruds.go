/*
Copyright 2018 The Knative Authors.

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

        "github.com/google/go-cmp/cmp"

        authv1alpha1 "github.com/knative/serving/pkg/apis/istio/authentication/v1alpha1"
        "github.com/knative/serving/pkg/apis/istio/v1alpha3"
        "github.com/knative/serving/pkg/apis/serving/v1alpha1"
        "github.com/knative/serving/pkg/controller/revision/resources"
        "github.com/knative/serving/pkg/logging"

        apierrs "k8s.io/apimachinery/pkg/api/errors"
        "k8s.io/apimachinery/pkg/api/equality"

        "go.uber.org/zap"
)

func (c *Controller) reconcileRevisionDestinationRule(ctx context.Context, rev *v1alpha1.Revision) error {
        return c.reconcileDestinationRule(ctx, rev, resources.MakeRevisionDestinationRule(rev))
}

func (c *Controller) reconcileAutoscalerDestinationRule(ctx context.Context, rev *v1alpha1.Revision) error {       
        return c.reconcileDestinationRule(ctx, rev, resources.MakeAutoscalerDestinationRule(rev))
}

func (c *Controller) reconcileDestinationRule(ctx context.Context, rev *v1alpha1.Revision, desiredDestinationRule *v1alpha3.DestinationRule) error {
        ns := desiredDestinationRule.Namespace
        logger := logging.FromContext(ctx)
        destinationRuleName := desiredDestinationRule.Name

        destinationRule, err := c.destinationRuleLister.DestinationRules(ns).Get(destinationRuleName)
        switch rev.Spec.ServingState {
                case v1alpha1.RevisionServingStateActive:
                        // When Active, the DestinationRule should exist and have a particular specification.
                        if apierrs.IsNotFound(err) {
                                rev.Status.MarkDeploying("Deploying")
                                destinationRule, err = c.createDestinationRule(desiredDestinationRule)
                                if err != nil {
                                        logger.Errorf("Error creating DestinationRule %q: %v", destinationRuleName, err)
                                        return err
                                }
                                logger.Infof("Created DestinationRule %q", destinationRuleName)
                                return nil
                        } else if err != nil {
                                logger.Errorf("Error reconciling Active DestinationRule %q: %v", destinationRuleName, err)
                                return err
                        } else {
                                return c.checkAndUpdateDestinationRule(ctx, destinationRule, desiredDestinationRule)
                        }
                case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
                        // When Reserve or Retired, we remove the underlying DestinationRule.
                        if apierrs.IsNotFound(err) {
                                // If it does not exist, then we have nothing to do.
                                return nil
                        }
                        if err := c.deleteDestinationRule(ctx, destinationRule); err != nil {
                                logger.Errorf("Error deleting DestinationRule %q: %v", destinationRuleName, err)
                                return err
                        }
                        logger.Infof("Deleted DestinationRule %q", destinationRuleName)
                        return nil
                default:
                        logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                        return nil
        }
}

func (c *Controller) createDestinationRule(destinationRule *v1alpha3.DestinationRule) (*v1alpha3.DestinationRule, error) {
        ns := destinationRule.Namespace
        return c.ServingClientSet.NetworkingV1alpha3().DestinationRules(ns).Create(destinationRule)
}

func (c *Controller) checkAndUpdateDestinationRule(ctx context.Context, destinationRule *v1alpha3.DestinationRule, desiredDestinationRule *v1alpha3.DestinationRule) error {
        if !equality.Semantic.DeepEqual(destinationRule.Spec, desiredDestinationRule.Spec) {
                logger := logging.FromContext(ctx)
                logger.Infof("Reconciling destination rule diff (-desired, +observed): %v",
                        cmp.Diff(desiredDestinationRule.Spec, destinationRule.Spec))
                destinationRule.Spec = desiredDestinationRule.Spec
                ns := desiredDestinationRule.Namespace
                _, err := c.ServingClientSet.NetworkingV1alpha3().DestinationRules(ns).Update(destinationRule)
                if err != nil {
                        logger.Error("Error updating destination rule", zap.Error(err))
                        return err
                }
        }
        return nil
}

func (c *Controller) deleteDestinationRule(ctx context.Context, destinationRule *v1alpha3.DestinationRule) error {
        logger := logging.FromContext(ctx)
        err := c.ServingClientSet.NetworkingV1alpha3().DestinationRules(destinationRule.Namespace).Delete(destinationRule.Name, fgDeleteOptions)
        if apierrs.IsNotFound(err) {
                return nil
        } else if err != nil {
                logger.Errorf("destinationRule.Delete for %q failed: %s", destinationRule.Name, err)
                return err
        }
        return nil
}

func (c *Controller) reconcileRevisionAuthPolicy(ctx context.Context, rev *v1alpha1.Revision) error {
        return c.reconcileAuthPolicy(ctx, rev, resources.MakeRevisionAuthPolicy(rev))
}

func (c *Controller) reconcileAutoscalerAuthPolicy(ctx context.Context, rev *v1alpha1.Revision) error {
        return c.reconcileAuthPolicy(ctx, rev, resources.MakeAutoscalerAuthPolicy(rev))
}

func (c *Controller) reconcileAuthPolicy(ctx context.Context, rev *v1alpha1.Revision, desiredAuthPolicy *authv1alpha1.Policy) error {
        ns := desiredAuthPolicy.Namespace
        authPolicyName := desiredAuthPolicy.Name
        logger := logging.FromContext(ctx)

        authPolicy, err := c.authenticationPolicyLister.Policies(ns).Get(authPolicyName)
        switch rev.Spec.ServingState {
                case v1alpha1.RevisionServingStateActive:
                        // When Active, the authentication policy should exist and have a particular specification.
                        if apierrs.IsNotFound(err) {
                                rev.Status.MarkDeploying("Deploying")
                                authPolicy, err = c.createAuthPolicy(desiredAuthPolicy)
                                if err != nil {
                                        logger.Errorf("Error creating Authentication Policy %q: %v", authPolicyName, err)
                                        return err
                                }
                                logger.Infof("Created Authentication Policy %q", authPolicyName)
                                return nil
                        } else if err != nil {
                                logger.Errorf("Error reconciling Active Authentication Policy %q: %v", authPolicyName, err)
                                return err
                        } else {
                                return c.checkAndUpdateAuthPolicy(ctx, authPolicy, desiredAuthPolicy)
                        }
                case v1alpha1.RevisionServingStateReserve, v1alpha1.RevisionServingStateRetired:
                        // When Reserve or Retired, we remove the underlying DestinationRule.
                        if apierrs.IsNotFound(err) {
                                // If it does not exist, then we have nothing to do.
                                return nil
                        }
                        if err := c.deleteAuthPolicy(ctx, authPolicy); err != nil {
                                logger.Errorf("Error deleting Authentication Policy %q: %v", authPolicyName, err)
                                return err
                        }
                        logger.Infof("Deleted Authentication Policy %q", authPolicy)
                        return nil
                default:
                        logger.Errorf("Unknown serving state: %v", rev.Spec.ServingState)
                        return nil
        }
}

func (c *Controller) createAuthPolicy(authPolicy *authv1alpha1.Policy) (*authv1alpha1.Policy, error) {
        ns := authPolicy.Namespace
        return c.ServingClientSet.AuthenticationV1alpha1().Policies(ns).Create(authPolicy)
}

func (c *Controller) checkAndUpdateAuthPolicy(ctx context.Context, authPolicy *authv1alpha1.Policy, desiredAuthPolicy *authv1alpha1.Policy) error {
        if !equality.Semantic.DeepEqual(authPolicy.Spec, desiredAuthPolicy.Spec) {
                logger := logging.FromContext(ctx)
                logger.Infof("Reconciling auth policy diff (-desired, +observed): %v",
                        cmp.Diff(desiredAuthPolicy.Spec, authPolicy.Spec))
                authPolicy.Spec = desiredAuthPolicy.Spec
                ns := desiredAuthPolicy.Namespace
                _, err := c.ServingClientSet.AuthenticationV1alpha1().Policies(ns).Update(authPolicy)
                if err != nil {
                        logger.Error("Error updating auth policy", zap.Error(err))
                        return err
                }
        }
        return nil
}

func (c *Controller) deleteAuthPolicy(ctx context.Context, authenticationPolicy *authv1alpha1.Policy) error {
        logger := logging.FromContext(ctx)
        err := c.ServingClientSet.AuthenticationV1alpha1().Policies(authenticationPolicy.Namespace).Delete(authenticationPolicy.Name, fgDeleteOptions)
        if apierrs.IsNotFound(err) {
                return nil
        } else if err != nil {
                logger.Errorf("policy.Delete for %q failed: %s", authenticationPolicy.Name, err)
                return err
        }
        return nil
}
