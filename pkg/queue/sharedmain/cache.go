/*
Copyright 2018 The Knative Authors

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

package sharedmain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"

	"knative.dev/control-protocol/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	"knative.dev/pkg/logging"
)

type CertCache struct {
	logger      *zap.SugaredLogger
	trustConfig netcfg.Trust

	certificate   *tls.Certificate
	ServerTLSConf tls.Config

	certificatesMux sync.RWMutex
	caCerts         []*x509.Certificate
	caLastUpdate    time.Time
}

func (cr *CertCache) init() {
	cr.ServerTLSConf.MinVersion = tls.VersionTLS12
	cr.ServerTLSConf.ClientCAs = x509.NewCertPool()
	cr.ServerTLSConf.GetCertificate = cr.GetCertificate
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
				//Until all ingresses work with updated dataplane certificates  - allow also any legacy certificate
				if match == certificates.LegacyFakeDnsName {
					return nil
				}
			}

			cr.logger.Info("mTLS: Failed to verify client connection for DNSNames: %v\n", cs.PeerCertificates[0].DNSNames)
			return fmt.Errorf("mTLS: Failed to verify client connection for DNSNames: %v", cs.PeerCertificates[0].DNSNames)
		}
	}
}

// NewCertCache starts secretInformer.
func NewCertCache(ctx context.Context, trust netcfg.Trust) *CertCache {
	cr := &CertCache{
		logger:      logging.FromContext(ctx),
		trustConfig: trust,
	}
	cr.init()

	cr.updateCache()

	return cr
}

func (cr *CertCache) updateCache() {
	info, err := os.Stat(caPath)
	if err != nil {
		cr.logger.Warnw("failed to stat secret ca", zap.Error(err))
		return
	}
	if cr.caLastUpdate == info.ModTime() {
		return
	}
	cr.logger.Info("Secret reloaded")

	cr.certificatesMux.Lock()
	defer cr.certificatesMux.Unlock()

	cr.caLastUpdate = info.ModTime()

	//caPath
	caBlock, err := os.ReadFile(caPath)
	if err != nil {
		cr.logger.Warnw("failed to parse secret ca", zap.Error(err))
		return
	}
	block, _ := pem.Decode(caBlock)
	if block == nil {
		cr.logger.Warnw("failed to parse CA")
		return
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		cr.logger.Warnw("failed to parse CA", zap.Error(err))
		return
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		cr.logger.Warnw("failed to parse secret", zap.Error(err))
		return
	}

	cr.certificate = &cert
	for _, usedCa := range cr.caCerts {
		if usedCa.Equal(ca) {
			return
		}
	}
	cr.caCerts = append(cr.caCerts, ca)
	cr.ServerTLSConf.ClientCAs.AddCert(ca)
}

// GetCertificate returns the cached certificates.
func (cr *CertCache) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cr.updateCache()
	return cr.certificate, nil
}
