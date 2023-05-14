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
	trust          netcfg.Trust

	certificate   *tls.Certificate
	ClientTLSConf tls.Config
	ServerTLSConf tls.Config

	certificatesMux sync.RWMutex
}

// NewCertCache starts secretInformer.
func NewCertCache(ctx context.Context, trust netcfg.Trust) *CertCache {
	secretInformer := secretinformer.Get(ctx)

	cr := &CertCache{
		secretInformer: secretInformer,
		logger:         logging.FromContext(ctx),
		trust:          trust,
	}

	secret, err := cr.secretInformer.Lister().Secrets(system.Namespace()).Get(netcfg.ServingRoutingCertName)
	if err != nil {
		cr.logger.Warnw("failed to get secret", zap.Error(err))
		return nil
	}

	cr.updateCache(secret)

	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), netcfg.ServingRoutingCertName),
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: cr.handleCertificateUpdate,
			AddFunc:    cr.handleCertificateAdd,
		},
	})

	return cr
}

func (cr *CertCache) handleCertificateAdd(added interface{}) {
	if secret, ok := added.(*corev1.Secret); ok {
		cr.updateCache(secret)
	}
}

func (cr *CertCache) updateCache(secret *corev1.Secret) {
	cr.certificatesMux.Lock()
	defer cr.certificatesMux.Unlock()

	cert, err := tls.X509KeyPair(secret.Data[certificates.CertName], secret.Data[certificates.PrivateKeyName])
	if err != nil {
		cr.logger.Warnw("failed to parse secret", zap.Error(err))
		return
	}
	cr.certificate = &cert

	pool := x509.NewCertPool()
	block, _ := pem.Decode(secret.Data[certificates.CaCertName])
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		cr.logger.Warnw("failed to parse CA", zap.Error(err))
		return
	}
	pool.AddCert(ca)

	cr.ClientTLSConf.RootCAs = pool
	cr.ClientTLSConf.ServerName = certificates.LegacyFakeDnsName
	cr.ClientTLSConf.MinVersion = tls.VersionTLS12
	cr.ClientTLSConf.Certificates = []tls.Certificate{cert}

	cr.ServerTLSConf.MinVersion = tls.VersionTLS12
	cr.ServerTLSConf.GetCertificate = cr.GetCertificate

	switch cr.trust {
	case netcfg.TrustIdentity, netcfg.TrustMutual:
		cr.ServerTLSConf.ClientAuth = tls.RequireAndVerifyClientCert
		cr.ServerTLSConf.ClientCAs = pool
		cr.ServerTLSConf.VerifyConnection = func(cs tls.ConnectionState) error {
			for _, match := range cs.PeerCertificates[0].DNSNames {
				if match != "kn-routing-0" { // routingId not yet supported
					continue
				}
				//Until all ingresses work with updated dataplane certificates  - allow also any legacy certificate
				if match != certificates.LegacyFakeDnsName {
					continue
				}
				return nil
			}
			cr.logger.Info("mTLS: Failed Client with DNSNames: %v\n", cs.PeerCertificates[0].DNSNames)
			return fmt.Errorf("mTLS Failed to approve %v", cs.PeerCertificates[0].DNSNames)
		}
	}
}

func (cr *CertCache) handleCertificateUpdate(_, new interface{}) {
	cr.handleCertificateAdd(new)
}

// GetCertificate returns the cached certificates.
func (cr *CertCache) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return cr.certificate, nil
}
