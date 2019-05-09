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

package certificate

import (
	"context"
	"fmt"
	"reflect"

	cmv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	certmanagerclientset "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions/certmanager/v1alpha1"
	certmanagerlisters "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/certificate/config"
	"github.com/knative/serving/pkg/reconciler/certificate/resources"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName  = "certificate-controller"
	noCMConditionReason  = "NoCertManagerCertCondition"
	noCMConditionMessage = "The ready condition of Cert Manager Certifiate does not exist."
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// Reconciler implements controller.Reconciler for Certificate resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	knCertificateLister listers.CertificateLister
	cmCertificateLister certmanagerlisters.CertificateLister
	certManagerClient   certmanagerclientset.Interface

	configStore configStore
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	knCertificateInformer informers.CertificateInformer,
	cmCertificateInformer certmanagerinformers.CertificateInformer,
	certManagerClient certmanagerclientset.Interface,
) *controller.Impl {
	c := &Reconciler{
		Base:                reconciler.NewBase(opt, controllerAgentName),
		knCertificateLister: knCertificateInformer.Lister(),
		cmCertificateLister: cmCertificateInformer.Lister(),
		certManagerClient:   certManagerClient,
	}

	impl := controller.NewImpl(c, c.Logger, "Certificate", reconciler.MustNewStatsReporter("Certificate", c.Logger))

	c.Logger.Info("Setting up event handlers")
	knCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	cmCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.EnqueueControllerOf,
		UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		DeleteFunc: impl.EnqueueControllerOf,
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	resyncCertOnCertManagerconfigChange := configmap.TypeFilter(&config.CertManagerConfig{})(func(string, interface{}) {
		impl.GlobalResync(knCertificateInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resyncCertOnCertManagerconfigChange)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Certificate resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)
	ctx = c.configStore.ToContext(ctx)

	original, err := c.knCertificateLister.Certificates(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		logger.Errorf("Knative Certificate %s in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy
	knCert := original.DeepCopy()

	// Reconcile this copy of the Certificate and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, knCert)
	if equality.Semantic.DeepEqual(original.Status, knCert.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(knCert); err != nil {
		logger.Warnw("Failed to update certificate status", zap.Error(err))
		c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Certificate %s: %v", key, err)
		return err
	}
	if err != nil {
		c.Recorder.Event(knCert, corev1.EventTypeWarning, "InternalError", err.Error())
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, knCert *v1alpha1.Certificate) error {
	logger := logging.FromContext(ctx)

	knCert.SetDefaults(ctx)
	knCert.Status.InitializeConditions()

	logger.Info("Reconciling Cert-Manager certificate for Knative cert %s/%s.", knCert.Namespace, knCert.Name)
	cmConfig := config.FromContext(ctx).CertManager
	cmCert := resources.MakeCertManagerCertificate(cmConfig, knCert)
	cmCert, err := c.reconcileCMCertificate(ctx, knCert, cmCert)
	if err != nil {
		return err
	}

	knCert.Status.NotAfter = cmCert.Status.NotAfter
	knCert.Status.ObservedGeneration = knCert.Generation
	// Propagate cert-manager Certificate status to Knative Certificate.
	cmCertReadyCondition := resources.GetReadyCondition(cmCert)
	switch {
	case cmCertReadyCondition == nil:
		knCert.Status.MarkUnknown(noCMConditionReason, noCMConditionMessage)
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionUnknown:
		knCert.Status.MarkUnknown(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionTrue:
		knCert.Status.MarkReady()
	case cmCertReadyCondition.Status == cmv1alpha1.ConditionFalse:
		knCert.Status.MarkNotReady(cmCertReadyCondition.Reason, cmCertReadyCondition.Message)
	}
	return nil
}

func (c *Reconciler) reconcileCMCertificate(ctx context.Context, knCert *v1alpha1.Certificate, desired *cmv1alpha1.Certificate) (*cmv1alpha1.Certificate, error) {
	logger := logging.FromContext(ctx)
	cmCert, err := c.cmCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		cmCert, err = c.certManagerClient.CertmanagerV1alpha1().Certificates(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Cert-Manager certificate", zap.Error(err))
			c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Cert-Manager Certificate %s/%s: %v", desired.Name, desired.Namespace, err)
			return nil, err
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Created",
			"Created Cert-Manager Certificate %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(desired, knCert) {
		knCert.Status.MarkResourceNotOwned("CertManagerCertificate", desired.Name)
		return nil, fmt.Errorf("Knative Certificate %s in namespace %s does not own CertManager Certificate: %s", knCert.Name, knCert.Namespace, desired.Name)
	} else if !equality.Semantic.DeepEqual(cmCert.Spec, desired.Spec) {
		copy := cmCert.DeepCopy()
		copy.Spec = desired.Spec
		updated, err := c.certManagerClient.CertmanagerV1alpha1().Certificates(copy.Namespace).Update(copy)
		if err != nil {
			logger.Errorw("Failed to update Cert-Manager Certificate", zap.Error(err))
			c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to create Cert-Manager Certificate %s/%s: %v", desired.Namespace, desired.Name, err)
			return nil, err
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Cert-Manager Certificate %s/%s", desired.Namespace, desired.Name)
		return updated, nil
	}
	return cmCert, nil
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Certificate) (*v1alpha1.Certificate, error) {
	cert, err := c.knCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(cert.Status, desired.Status) {
		return cert, nil
	}
	// Don't modify the informers copy
	existing := cert.DeepCopy()
	existing.Status = desired.Status

	return c.ServingClientSet.NetworkingV1alpha1().Certificates(existing.Namespace).UpdateStatus(existing)
}
