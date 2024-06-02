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
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	CertReloadMessage = "Certificate and/or key have changed on disk and were reloaded."
)

// CertWatcher watches certificate and key files and reloads them if they change on disk.
type CertWatcher struct {
	certPath     string
	certChecksum [sha256.Size]byte
	keyPath      string
	keyChecksum  [sha256.Size]byte

	certificate *tls.Certificate

	logger *zap.SugaredLogger
	ticker *time.Ticker
	stop   chan struct{}
	mux    sync.RWMutex
}

// NewCertWatcher creates a CertWatcher and watches
// the certificate and key files. It reloads the contents on file change.
// Make sure to stop the CertWatcher using Stop() upon destroy.
func NewCertWatcher(certPath, keyPath string, reloadInterval time.Duration, logger *zap.SugaredLogger) (*CertWatcher, error) {
	cw := &CertWatcher{
		certPath: certPath,
		keyPath:  keyPath,
		logger:   logger,
		ticker:   time.NewTicker(reloadInterval),
		stop:     make(chan struct{}),
		mux:      sync.RWMutex{},
	}

	certDir := path.Dir(cw.certPath)
	keyDir := path.Dir(cw.keyPath)

	cw.logger.Info("Starting to watch the following directories for changes",
		zap.String("certDir", certDir), zap.String("keyDir", keyDir))

	// initial load
	if err := cw.loadCert(); err != nil {
		return nil, err
	}

	go cw.watch()

	return cw, nil
}

// Stop shuts down the CertWatcher. Use this with `defer`.
func (cw *CertWatcher) Stop() {
	cw.logger.Info("Stopping file watcher")
	close(cw.stop)
	cw.ticker.Stop()
}

// GetCertificate returns the server certificate for a client-hello request.
func (cw *CertWatcher) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cw.mux.RLock()
	defer cw.mux.RUnlock()
	return cw.certificate, nil
}

func (cw *CertWatcher) watch() {
	for {
		select {
		case <-cw.stop:
			return

		case <-cw.ticker.C:
			// On error, we do not want to stop trying
			if err := cw.loadCert(); err != nil {
				cw.logger.Error(err)
			}
		}
	}
}

func (cw *CertWatcher) loadCert() error {
	var err error
	certFile, err := os.ReadFile(cw.certPath)
	if err != nil {
		return fmt.Errorf("failed to load certificate file in %s: %w", cw.certPath, err)
	}
	keyFile, err := os.ReadFile(cw.keyPath)
	if err != nil {
		return fmt.Errorf("failed to load key file in %s: %w", cw.keyPath, err)
	}

	certChecksum := sha256.Sum256(certFile)
	keyChecksum := sha256.Sum256(keyFile)

	if certChecksum != cw.certChecksum || keyChecksum != cw.keyChecksum {
		keyPair, err := tls.LoadX509KeyPair(cw.certPath, cw.keyPath)
		if err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}

		cw.mux.Lock()
		defer cw.mux.Unlock()

		cw.certificate = &keyPair
		cw.certChecksum = certChecksum
		cw.keyChecksum = keyChecksum

		cw.logger.Info(CertReloadMessage)
	}

	return nil
}
