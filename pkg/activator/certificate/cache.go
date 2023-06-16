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
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/tools/cache"

	"knative.dev/networking/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/controller"
	secretinformer "knative.dev/pkg/injection/clients/namespacedkube/informers/core/v1/secret"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	"knative.dev/serving/pkg/activator/handler"
)

// CertCache caches certificates and CA pool.
type CertCache struct {
	secretInformer v1.SecretInformer
	logger         *zap.SugaredLogger
	trustConfig    netcfg.Trust

	certificate   *tls.Certificate
	ClientTLSConf tls.Config
	ServerTLSConf tls.Config
}

func newCertCache(ctx context.Context, trust netcfg.Trust, secretInformer v1.SecretInformer) *CertCache {
	cr := &CertCache{
		secretInformer: secretInformer,
		logger:         logging.FromContext(ctx),
		trustConfig:    trust,
	}
	cr.ClientTLSConf.ServerName = certificates.LegacyFakeDnsName
	cr.ClientTLSConf.MinVersion = tls.VersionTLS13
	cr.ClientTLSConf.RootCAs = x509.NewCertPool()
	cr.ClientTLSConf.GetClientCertificate = cr.GetClientCertificate

	cr.ServerTLSConf.MinVersion = tls.VersionTLS12
	cr.ServerTLSConf.ClientCAs = x509.NewCertPool()
	cr.ServerTLSConf.GetCertificate = cr.GetCertificate
	cr.ServerTLSConf.GetConfigForClient = cr.GetConfigForClient
	switch cr.trustConfig {
	case netcfg.TrustIdentity, netcfg.TrustMutual:
		cr.ServerTLSConf.ClientAuth = tls.RequireAndVerifyClientCert
		cr.ServerTLSConf.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				// Should never happen on a server side
				cr.logger.Info("mTLS: Failed to verify client connection. Certificate is missing\n")
				return fmt.Errorf("mTLS: Failed to verify client connection. Certificate is missing")
			}
			for _, match := range cs.PeerCertificates[0].DNSNames {
				// Activator currently supports a single routingId which is the default "0"
				// Working with other routingId is not yet implemented
				if match == certificates.DataPlaneRoutingName("0") {
					return nil
				}
			}

			cr.logger.Info("mTLS: Failed to verify client connection for DNSNames: %v\n", cs.PeerCertificates[0].DNSNames)
			return fmt.Errorf("mTLS: Failed to verify client connection for DNSNames: %v", cs.PeerCertificates[0].DNSNames)
		}
	}

	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), netcfg.ServingRoutingCertName),
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: cr.handleCertificateUpdate,
			AddFunc:    cr.handleCertificateAdd,
		},
	})
	return cr
}

// NewCertCache starts secretInformer.
func NewCertCache(ctx context.Context, trust netcfg.Trust) *CertCache {
	secretInformer := secretinformer.Get(ctx)
	cr := newCertCache(ctx, trust, secretInformer)

	secret, err := cr.secretInformer.Lister().Secrets(system.Namespace()).Get(netcfg.ServingRoutingCertName)
	if err != nil {
		cr.logger.Warnw("failed to get secret", zap.Error(err))
		return nil
	}

	cr.updateCache(secret)
	return cr
}

func (cr *CertCache) handleCertificateAdd(added interface{}) {
	if secret, ok := added.(*corev1.Secret); ok {
		cr.updateCache(secret)
	}
}

func (cr *CertCache) updateCache(secret *corev1.Secret) {
	handler.TlsConfLock()
	defer handler.TlsConfUnlock()

	cert, err := tls.X509KeyPair(secret.Data[certificates.CertName], secret.Data[certificates.PrivateKeyName])
	if err != nil {
		cr.logger.Warnw("failed to parse secret", zap.Error(err))
		return
	}
	cr.certificate = &cert

	cr.ClientTLSConf.RootCAs.AppendCertsFromPEM(secret.Data[certificates.CaCertName])
	cr.ServerTLSConf.ClientCAs.AppendCertsFromPEM(secret.Data[certificates.CaCertName])
}

func (cr *CertCache) handleCertificateUpdate(_, new interface{}) {
	cr.handleCertificateAdd(new)
}

// GetCertificate returns the cached certificates.
func (cr *CertCache) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	handler.TlsConfLock()
	defer handler.TlsConfUnlock()
	return cr.certificate, nil
}

func (cr *CertCache) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	handler.TlsConfLock()
	defer handler.TlsConfUnlock()
	return cr.certificate, nil
}

func (cr *CertCache) GetConfigForClient(*tls.ClientHelloInfo) (*tls.Config, error) {
	handler.TlsConfLock()
	defer handler.TlsConfUnlock()
	// Clone the certificate Pool such that the one used by the server will be different from the one that will get updated is CA is replaced.
	serverTLSConf := cr.ServerTLSConf.Clone()
	serverTLSConf.ClientCAs = serverTLSConf.ClientCAs.Clone()
	return serverTLSConf, nil
}
