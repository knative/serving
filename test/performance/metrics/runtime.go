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

	"k8s.io/apimachinery/pkg/labels"
	deploymentinformer "knative.dev/pkg/injection/informers/kubeinformers/appsv1/deployment"
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

// FetchDeploymentStatus creates a channel that can return the up-to-date DeploymentStatus periodically.
func FetchDeploymentStatus(
	ctx context.Context, namespace string, selector labels.Selector,
	duration time.Duration, stop <-chan struct{},
) <-chan DeploymentStatus {
	dl := deploymentinformer.Get(ctx).Lister()
	ch := make(chan DeploymentStatus)
	startTick(duration, stop, func(t time.Time) error {
		// Overlay the desired and ready pod counts.
		deployments, err := dl.Deployments(namespace).List(selector)
		if err != nil {
			log.Printf("Error listing deployments: %v", err)
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
	duration time.Duration, stop <-chan struct{},
) <-chan ServerlessServiceStatus {
	sksl := sksinformer.Get(ctx).Lister()
	ch := make(chan ServerlessServiceStatus)
	startTick(duration, stop, func(t time.Time) error {
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
		for {
			select {
			case t := <-ticker.C:
				if err := action(t); err != nil {
					break
				}
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
}
