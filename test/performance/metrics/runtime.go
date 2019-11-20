/*
Copyright 2019 The Knative Authors

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

package metrics

import (
	"context"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"

	deploymentinformer "knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment"
	netv1alpha1 "knative.dev/serving/pkg/apis/networking/v1alpha1"
	sksinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/serverlessservice"

	// Mysteriously required to support GCP auth (required by k8s libs).
	// Apparently just importing it is enough. @_@ side effects @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

// DeploymentStatus is a struct that wraps the status of a deployment.
type DeploymentStatus struct {
	DesiredReplicas int32
	ReadyReplicas   int32
	// Time is the time when the status is fetched
	Time time.Time
}

// FetchDeploymentsStatus creates a channel that can return the up-to-date DeploymentStatus periodically,
// selected via a label selector (can be more than one deployment).
func FetchDeploymentsStatus(
	ctx context.Context, namespace string, selector labels.Selector,
	duration time.Duration,
) <-chan DeploymentStatus {
	dl := deploymentinformer.Get(ctx).Lister()
	return fetchStatusInternal(ctx, duration, func() ([]*appsv1.Deployment, error) {
		return dl.Deployments(namespace).List(selector)
	})
}

// FetchDeploymentStatus creates a channel that can return the up-to-date DeploymentStatus periodically,
// selected via deployment name (at most one deployment).
func FetchDeploymentStatus(
	ctx context.Context, namespace, name string, duration time.Duration,
) <-chan DeploymentStatus {
	dl := deploymentinformer.Get(ctx).Lister()
	return fetchStatusInternal(ctx, duration, func() ([]*appsv1.Deployment, error) {
		d, err := dl.Deployments(namespace).Get(name)
		if err != nil {
			return []*appsv1.Deployment{}, err
		}
		return []*appsv1.Deployment{d}, nil
	})
}

func fetchStatusInternal(ctx context.Context, duration time.Duration,
	f func() ([]*appsv1.Deployment, error)) <-chan DeploymentStatus {
	ch := make(chan DeploymentStatus)
	startTick(duration, ctx.Done(), func(t time.Time) error {
		// Overlay the desired and ready pod counts.
		deployments, err := f()
		if err != nil {
			log.Printf("Error getting deployment(s): %v", err)
			return err
		}

		for _, d := range deployments {
			ds := DeploymentStatus{
				DesiredReplicas: *d.Spec.Replicas,
				ReadyReplicas:   d.Status.ReadyReplicas,
				Time:            t,
			}
			ch <- ds
		}
		return nil
	})
	return ch
}

// ServerlessServiceStatus is a struct that wraps the status of a serverless service.
type ServerlessServiceStatus struct {
	Mode netv1alpha1.ServerlessServiceOperationMode
	// Time is the time when the status is fetched
	Time time.Time
}

// FetchSKSMode creates a channel that can return the up-to-date ServerlessServiceOperationMode periodically.
func FetchSKSMode(
	ctx context.Context, namespace string, selector labels.Selector,
	duration time.Duration,
) <-chan ServerlessServiceStatus {
	sksl := sksinformer.Get(ctx).Lister()
	ch := make(chan ServerlessServiceStatus)
	startTick(duration, ctx.Done(), func(t time.Time) error {
		// Overlay the SKS "mode".
		skses, err := sksl.ServerlessServices(namespace).List(selector)
		if err != nil {
			log.Printf("Error listing serverless services: %v", err)
			return err
		}
		for _, sks := range skses {
			skss := ServerlessServiceStatus{
				Mode: sks.Spec.Mode,
				Time: t,
			}
			ch <- skss
		}
		return nil
	})

	return ch
}

func startTick(duration time.Duration, stop <-chan struct{}, action func(t time.Time) error) {
	ticker := time.NewTicker(duration)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case t := <-ticker.C:
				if err := action(t); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()
}
