/*
Copyright 2023 The Knative Authors

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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/reconciler"

	"knative.dev/networking/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/controller"
	nsconfigmapinformer "knative.dev/pkg/injection/clients/namespacedkube/informers/core/v1/configmap"
	nssecretinformer "knative.dev/pkg/injection/clients/namespacedkube/informers/core/v1/secret"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
)

// CertCache caches certificates and CA pool.
type CertCache struct {
	secretInformer    v1.SecretInformer
	configmapInformer v1.ConfigMapInformer
	logger            *zap.SugaredLogger

	certificate *tls.Certificate
	TLSConf     tls.Config

	certificatesMux sync.RWMutex
}

// NewCertCache creates and starts the certificate cache that watches Activators certificate.
func NewCertCache(ctx context.Context) (*CertCache, error) {
	nsSecretInformer := nssecretinformer.Get(ctx)
	nsConfigmapInformer := nsconfigmapinformer.Get(ctx)

	cr := &CertCache{
		secretInformer:    nsSecretInformer,
		configmapInformer: nsConfigmapInformer,
		logger:            logging.FromContext(ctx),
	}

	secret, err := cr.secretInformer.Lister().Secrets(system.Namespace()).Get(netcfg.ServingRoutingCertName)
	if err != nil {
		return nil, fmt.Errorf("failed to get activator certificate, secret %s/%s was not found: %w. Enabling system-internal-tls requires the secret to be present and populated with a valid certificate",
			system.Namespace(), netcfg.ServingRoutingCertName, err)
	}

	cr.updateCertificate(secret)
	cr.updateTrustPool()

	nsSecretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), netcfg.ServingRoutingCertName),
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: cr.handleCertificateUpdate,
			AddFunc:    cr.handleCertificateAdd,
		},
	})

	nsConfigmapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.LabelExistsFilterFunc(networking.TrustBundleLabelKey),
		),
		Handler: controller.HandleAll(func(obj interface{}) {
			cr.updateTrustPool()
		}),
	})

	return cr, nil
}

func (cr *CertCache) handleCertificateAdd(added interface{}) {
	if secret, ok := added.(*corev1.Secret); ok {
		cr.updateCertificate(secret)
		cr.updateTrustPool()
	}
}

func (cr *CertCache) handleCertificateUpdate(_, new interface{}) {
	cr.handleCertificateAdd(new)
	cr.updateTrustPool()
}

func (cr *CertCache) updateCertificate(secret *corev1.Secret) {
	cr.certificatesMux.Lock()
	defer cr.certificatesMux.Unlock()

	cert, err := tls.X509KeyPair(secret.Data[certificates.CertName], secret.Data[certificates.PrivateKeyName])
	if err != nil {
		cr.logger.Warnf("failed to parse certificate in secret %s/%s: %v", secret.Namespace, secret.Name, zap.Error(err))
		return
	}
	cr.certificate = &cert
}

// CA can optionally be in `ca.crt` in the `routing-serving-certs` secret
// and/or configured using a trust-bundle via ConfigMap that has the defined label `knative-ca-trust-bundle`.
func (cr *CertCache) updateTrustPool() {
	pool := x509.NewCertPool()

	cr.addSecretCAIfPresent(pool)
	cr.addTrustBundles(pool)

	// Use the trust pool in upstream TLS context
	cr.certificatesMux.Lock()
	defer cr.certificatesMux.Unlock()

	cr.TLSConf.RootCAs = pool
	cr.TLSConf.ServerName = certificates.LegacyFakeDnsName
	cr.TLSConf.MinVersion = tls.VersionTLS13
}

func (cr *CertCache) addSecretCAIfPresent(pool *x509.CertPool) {
	secret, err := cr.secretInformer.Lister().Secrets(system.Namespace()).Get(netcfg.ServingRoutingCertName)
	if err != nil {
		cr.logger.Warnf("Failed to get secret %s/%s: %v", system.Namespace(), netcfg.ServingRoutingCertName, zap.Error(err))
		return
	}
	if len(secret.Data[certificates.CaCertName]) > 0 {
		block, _ := pem.Decode(secret.Data[certificates.CaCertName])
		ca, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			cr.logger.Warnf("CA from Secret %s/%s[%s] is invalid and will be ignored: %v",
				system.Namespace(), netcfg.ServingRoutingCertName, certificates.CaCertName, err)
		} else {
			pool.AddCert(ca)
		}
	}
}

func (cr *CertCache) addTrustBundles(pool *x509.CertPool) {
	selector, err := getLabelSelector(networking.TrustBundleLabelKey)
	if err != nil {
		cr.logger.Error("Failed to get label selector", zap.Error(err))
		return
	}
	cms, err := cr.configmapInformer.Lister().ConfigMaps(system.Namespace()).List(selector)
	if err != nil {
		cr.logger.Warnf("Failed to get ConfigMaps %s/%s with label %s: %v", system.Namespace(),
			netcfg.ServingRoutingCertName, networking.TrustBundleLabelKey, zap.Error(err))
		return
	}

	for _, cm := range cms {
		for _, bundle := range cm.Data {
			ok := pool.AppendCertsFromPEM([]byte(bundle))
			if !ok {
				cr.logger.Warnf("Failed to add CA bundle from ConfigMaps %s/%s as it contains invalid certificates. Bundle: %s", system.Namespace(),
					cm.Name, bundle)
			}
		}
	}
}

// GetCertificate returns the cached certificates.
func (cr *CertCache) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return cr.certificate, nil
}

func getLabelSelector(label string) (labels.Selector, error) {
	selector := labels.NewSelector()
	req, err := labels.NewRequirement(label, selection.Exists, make([]string, 0))
	if err != nil {
		return nil, err
	}
	selector = selector.Add(*req)
	return selector, nil
}
