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
	"errors"
	"sync"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/control-protocol/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/controller"
	secretinformer "knative.dev/pkg/injection/clients/namespacedkube/informers/core/v1/secret"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
)

// CertCache caches certificates and CA pool.
type CertCache struct {
	secretInformer v1.SecretInformer
	logger         *zap.SugaredLogger

	certificates *tls.Certificate
	pool         *x509.CertPool

	certificatesMux sync.RWMutex
}

// NewCertCache starts secretInformer.
func NewCertCache(ctx context.Context) *CertCache {
	secretInformer := secretinformer.Get(ctx)

	cr := &CertCache{
		secretInformer: secretInformer,
		certificates:   nil,
		pool:           nil,
		logger:         logging.FromContext(ctx),
	}

	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), netcfg.ServingInternalCertName),
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: cr.handleCertificateUpdate,
			AddFunc:    cr.handleCertificateAdd,
			DeleteFunc: cr.handleCertificateDelete,
		},
	})

	return cr
}

func (cr *CertCache) handleCertificateAdd(added interface{}) {
	if secret, ok := added.(*corev1.Secret); ok {
		cr.certificatesMux.Lock()
		defer cr.certificatesMux.Unlock()

		cert, err := tls.X509KeyPair(secret.Data[certificates.CertName], secret.Data[certificates.PrivateKeyName])
		if err != nil {
			cr.logger.Warnw("failed to parse secret", zap.Error(err))
			return
		}
		cr.certificates = &cert

		pool := x509.NewCertPool()
		block, _ := pem.Decode(secret.Data[certificates.CaCertName])
		ca, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			cr.logger.Warnw("Failed to parse CA: %v", zap.Error(err))
			return
		}
		pool.AddCert(ca)

		cr.pool = pool
	}
}

func (cr *CertCache) handleCertificateUpdate(_, new interface{}) {
	cr.handleCertificateAdd(new)
}

func (cr *CertCache) handleCertificateDelete(_ interface{}) {
	cr.certificatesMux.Lock()
	defer cr.certificatesMux.Unlock()
	cr.certificates = nil
	cr.pool = nil
}

// GetCertificate returns the cached certificates.
func (cr *CertCache) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cr.certificatesMux.RLock()
	defer cr.certificatesMux.RUnlock()
	return cr.certificates, nil
}

// ValidateCert validates the certificate with the CA pool and DNS name.
func (cr *CertCache) ValidateCert(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	cr.certificatesMux.RLock()
	defer cr.certificatesMux.RUnlock()

	for _, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return err
		}

		opts := x509.VerifyOptions{
			Roots:         cr.pool,
			DNSName:       certificates.FakeDnsName,
			Intermediates: x509.NewCertPool(),
		}

		if _, err := cert.Verify(opts); err != nil {
			cr.logger.Warnw("invalid cert found", zap.Error(err))
		} else {
			return nil
		}
	}
	return errors.New("failed to validate certificate")
}
