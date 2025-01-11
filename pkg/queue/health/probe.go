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
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	netheader "knative.dev/networking/pkg/http/header"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/ptr"
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

// GRPCProbeConfigOptions holds the gRPC probe config options
type GRPCProbeConfigOptions struct {
	Timeout time.Duration
	*corev1.GRPCAction
	KubeMajor string
	KubeMinor string
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
	t.TLSClientConfig.InsecureSkipVerify = true
	return t
}()

func getURL(config HTTPProbeConfigOptions) (*url.URL, error) {
	return url.Parse(string(config.Scheme) + "://" + net.JoinHostPort(config.Host, config.Port.String()) + config.Path)
}

// http2UpgradeProbe checks that an HTTP with HTTP2 upgrade request
// connection can be understood by the address.
// Returns: the highest known proto version supported (0 if not ready or error)
func http2UpgradeProbe(config HTTPProbeConfigOptions) (int, error) {
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
	}
	url, err := getURL(config)
	if err != nil {
		return 0, fmt.Errorf("error constructing probe url %w", err)
	}
	//nolint:noctx // timeout is specified on the http.Client above
	req, err := http.NewRequest(http.MethodOptions, url.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("error constructing probe request %w", err)
	}

	// An upgrade will need to have at least these 3 headers.
	// This is documented at https://tools.ietf.org/html/rfc7540#section-3.2
	req.Header.Add("Connection", "Upgrade, HTTP2-Settings")
	req.Header.Add("Upgrade", "h2c")
	req.Header.Add("HTTP2-Settings", "")

	req.Header.Add(netheader.UserAgentKey, netheader.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

	res, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	maxProto := 0

	if isHTTPProbeUpgradingToH2C(res) {
		maxProto = 2
	} else if isHTTPProbeReady(res) {
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
	url, err := getURL(config)
	if err != nil {
		return fmt.Errorf("error constructing probe url %w", err)
	}
	//nolint:noctx // timeout is specified on the http.Client above
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return fmt.Errorf("error constructing probe request %w", err)
	}

	req.Header.Add(netheader.UserAgentKey, netheader.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

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
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}()

	if !isHTTPProbeReady(res) {
		return fmt.Errorf("HTTP probe did not respond Ready, got status code: %d", res.StatusCode)
	}

	return nil
}

// isHTTPProbeUpgradingToH2C checks whether the server indicates it's switching to h2c protocol.
func isHTTPProbeUpgradingToH2C(res *http.Response) bool {
	return res.StatusCode == http.StatusSwitchingProtocols && res.Header.Get("Upgrade") == "h2c"
}

// isHTTPProbeReady checks whether we received a successful Response
func isHTTPProbeReady(res *http.Response) bool {
	// response status code between 200-399 indicates success
	return res.StatusCode >= 200 && res.StatusCode < 400
}

// GRPCProbe checks that gRPC connection can be established to the address.
func GRPCProbe(config GRPCProbeConfigOptions) error {
	// Use k8s.io/kubernetes/pkg/probe/dialer_others.go to correspond to OSs other than Windows
	dialer := &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				unix.SetsockoptLinger(int(fd), syscall.SOL_SOCKET, syscall.SO_LINGER, &unix.Linger{Onoff: 1, Linger: 1})
			})
		},
	}

	opts := []grpc.DialOption{
		grpc.WithUserAgent(netheader.KubeProbeUAPrefix + config.KubeMajor + "/" + config.KubeMinor),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // credentials are currently not supported
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", addr)
		}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)

	defer cancel()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(config.Port)))
	conn, err := grpc.NewClient(addr, opts...)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to connect service %q within %v: %w", addr, config.Timeout, err)
		}
		return fmt.Errorf("failed to connect service at %q: %w", addr, err)
	}

	defer func() {
		_ = conn.Close()
	}()

	client := grpchealth.NewHealthClient(conn)

	resp, err := client.Check(metadata.NewOutgoingContext(ctx, make(metadata.MD)), &grpchealth.HealthCheckRequest{
		Service: ptr.StringValue(config.Service),
	})

	if err != nil {
		stat, ok := status.FromError(err)
		if ok {
			switch stat.Code() {
			case codes.Unimplemented:
				return fmt.Errorf("this server does not implement the grpc health protocol (grpc.health.v1.Health) %w", err)
			case codes.DeadlineExceeded:
				return fmt.Errorf("health rpc did not complete within %v: %w", config.Timeout, err)
			}
		}
		return fmt.Errorf("health rpc probe failed: %w", err)
	}

	if resp.GetStatus() != grpchealth.HealthCheckResponse_SERVING {
		return fmt.Errorf("service unhealthy (responded with %q)", resp.GetStatus().String())
	}

	return nil
}
