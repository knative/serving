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

package core

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/logging"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
)

// SecretAccessor is an interface for accessing Secret.
type SecretAccessor interface {
	GetKubeClient() kubernetes.Interface
	GetSecretLister() corev1listers.SecretLister
}

// ReconcileSecret reconciles Secret to the desired status.
func ReconcileSecret(ctx context.Context, owner kmeta.Accessor, desired *corev1.Secret, accessor SecretAccessor) (*corev1.Secret, error) {
	logger := logging.FromContext(ctx)
	recorder := controller.GetEventRecorder(ctx)
	if recorder == nil {
		return nil, fmt.Errorf("recoder for reconciling Secret %s/%s is not created", desired.Namespace, desired.Name)
	}
	secret, err := accessor.GetSecretLister().Secrets(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		secret, err = accessor.GetKubeClient().CoreV1().Secrets(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Secret", zap.Error(err))
			recorder.Eventf(owner, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Created", "Created Secret %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(secret, owner) {
		// Return an error with NotControlledBy information.
		return nil, kaccessor.NewAccessorError(
			fmt.Errorf("owner: %s with Type %T does not own Secret: %s", owner.GetName(), owner, secret.Name),
			kaccessor.NotOwnResource)
	} else if !equality.Semantic.DeepEqual(secret.Data, desired.Data) {
		// Don't modify the informers copy
		copy := secret.DeepCopy()
		copy.Data = desired.Data
		secret, err = accessor.GetKubeClient().CoreV1().Secrets(copy.Namespace).Update(copy)
		if err != nil {
			logger.Errorw("Failed to update target secret", zap.Error(err))
			recorder.Eventf(owner, corev1.EventTypeWarning, "UpdateFailed", "Failed to update Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return nil, err
		}
		recorder.Eventf(owner, corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", copy.Namespace, copy.Name)
	}
	return secret, nil
}
