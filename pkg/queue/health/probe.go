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

package health

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	corev1 "k8s.io/api/core/v1"
	network "knative.dev/networking/pkg"
	pkgnet "knative.dev/pkg/network"
)

// HTTPProbeConfigOptions holds the HTTP probe config options
type HTTPProbeConfigOptions struct {
	Timeout time.Duration
	*corev1.HTTPGetAction
	KubeMajor     string
	KubeMinor     string
	MaxProtoMajor int
}

// TCPProbeConfigOptions holds the TCP probe config options
type TCPProbeConfigOptions struct {
	SocketTimeout time.Duration
	Address       string
}

// TCPProbe checks that a TCP socket to the address can be opened.
// Did not reuse k8s.io/kubernetes/pkg/probe/tcp to not create a dependency
// on klog.
func TCPProbe(config TCPProbeConfigOptions) error {
	conn, err := net.DialTimeout("tcp", config.Address, config.SocketTimeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// Returns a transport that uses HTTP/2 if it's known to be supported, and otherwise
// spoofs the request & response versions to HTTP/1.1.
func autoDowngradingTransport(opt HTTPProbeConfigOptions) http.RoundTripper {
	t := pkgnet.NewProberTransport()
	return pkgnet.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// If the user-container can handle HTTP2, we pass through the request as-is.
		// We have to set r.ProtoMajor to 2, since auto transport relies solely on it
		// to decide whether to use h2c or http1.1.
		if opt.MaxProtoMajor == 2 {
			r.ProtoMajor = 2
			return t.RoundTrip(r)
		}

		// Otherwise, save the request HTTP version and downgrade it
		// to HTTP1 before sending.
		version := r.ProtoMajor
		r.ProtoMajor = 1
		resp, err := t.RoundTrip(r)

		// Restore the request & response HTTP version before sending back.
		r.ProtoMajor = version
		if resp != nil {
			resp.ProtoMajor = version
		}
		return resp, err
	})
}

var transport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	//nolint:gosec // We explicitly don't need to check certs here.
	t.TLSClientConfig.InsecureSkipVerify = true
	return t
}()

func getURL(config HTTPProbeConfigOptions) url.URL {
	return url.URL{
		Scheme: string(config.Scheme),
		Host:   net.JoinHostPort(config.Host, config.Port.String()),
		Path:   config.Path,
	}
}

// http2UpgradeProbe checks that an HTTP with HTTP2 upgrade request
// connection can be understood by the address.
// Returns: the highest known proto version supported (0 if not ready or error)
func http2UpgradeProbe(config HTTPProbeConfigOptions) (int, error) {
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}
	url := getURL(config)
	req, err := http.NewRequest(http.MethodOptions, url.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("error constructing probe request %w", err)
	}

	// An upgrade will need to have at least these 3 headers.
	// This is documented at https://tools.ietf.org/html/rfc7540#section-3.2
	req.Header.Add("Connection", "Upgrade, HTTP2-Settings")
	req.Header.Add("Upgrade", "h2c")
	req.Header.Add("HTTP2-Settings", "")

	req.Header.Add(network.UserAgentKey, network.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

	res, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	maxProto := 0

	if IsHTTPProbeUpgradingToH2C(res) {
		maxProto = 2
	} else if IsHTTPProbeReady(res) {
		maxProto = 1
	} else {
		return maxProto, fmt.Errorf("HTTP probe did not respond Ready, got status code: %d", res.StatusCode)
	}

	return maxProto, nil
}

// HTTPProbe checks that HTTP connection can be established to the address.
func HTTPProbe(config HTTPProbeConfigOptions) error {
	if config.MaxProtoMajor == 0 {
		// If we don't know if the connection supports HTTP2, we will try it.
		// Once we get a non-error response, we won't try again.
		// If maxProto is 0, container is not ready, so we don't know whether http2 is supported.
		// If maxProto is 1, we know we're ready, but we also can't upgrade, so just return.
		// If maxProto is 2, we know we can upgrade to http2
		maxProto, err := http2UpgradeProbe(config)
		if err != nil {
			return fmt.Errorf("failed to run HTTP2 upgrade probe with error: %w", err)
		}
		config.MaxProtoMajor = maxProto
		if config.MaxProtoMajor == 1 {
			return nil
		}
	}
	httpClient := &http.Client{
		Transport: autoDowngradingTransport(config),
		Timeout:   config.Timeout,
	}
	url := getURL(config)
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return fmt.Errorf("error constructing probe request %w", err)
	}

	req.Header.Add(network.UserAgentKey, network.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

	for _, header := range config.HTTPHeaders {
		req.Header.Add(header.Name, header.Value)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}

	defer func() {
		// Ensure body is both read _and_ closed so it can be reused for keep-alive.
		// No point handling errors, connection just won't be reused.
		io.Copy(ioutil.Discard, res.Body)
		res.Body.Close()
	}()

	if !IsHTTPProbeReady(res) {
		return fmt.Errorf("HTTP probe did not respond Ready, got status code: %d", res.StatusCode)
	}

	return nil
}

// IsHTTPProbeUpgradingToH2C checks whether the server indicates it's switching to h2c protocol.
func IsHTTPProbeUpgradingToH2C(res *http.Response) bool {
	return res.StatusCode == http.StatusSwitchingProtocols && res.Header.Get("Upgrade") == "h2c"
}

// IsHTTPProbeReady checks whether we received a successful Response
func IsHTTPProbeReady(res *http.Response) bool {
	// response status code between 200-399 indicates success
	return res.StatusCode >= 200 && res.StatusCode < 400
}

// IsHTTPProbeShuttingDown checks whether the Response indicates the prober is shutting down.
func IsHTTPProbeShuttingDown(res *http.Response) bool {
	// status 410 (Gone) indicates the probe returned a shutdown scenario.
	return res.StatusCode == http.StatusGone
}
