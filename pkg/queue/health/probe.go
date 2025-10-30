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
	"strings"
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
	//nolint: noctx
	conn, err := net.DialTimeout("tcp", config.Address, config.SocketTimeout)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// proberTransport is a reusable transport optimized for health check probes.
// The transport auto-selects between HTTP/1.1 and H2C based on the request's ProtoMajor field.
var proberTransport = pkgnet.NewProberTransport()

// cleartextProbeTransport returns a RoundTripper that wraps the prober transport
// and sets the appropriate protocol hint for the given protocol version.
// protoMajor should be 1 for HTTP/1.1 or 2 for H2C.
func cleartextProbeTransport(protoMajor int) http.RoundTripper {
	return pkgnet.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// Set the protocol hint for the auto-selecting prober transport
		r.ProtoMajor = protoMajor
		return proberTransport.RoundTrip(r)
	})
}

func getURL(config HTTPProbeConfigOptions) (*url.URL, error) {
	return url.Parse(string(config.Scheme) + "://" + net.JoinHostPort(config.Host, config.Port.String()) + "/" + strings.TrimPrefix(config.Path, "/"))
}

// detectHTTPProtocolVersion detects the highest HTTP protocol version supported by the server.
// Attempts H2C first, falls back to HTTP/1 if that fails or returns non-ready status.
// Returns 2 (H2C ready), 1 (HTTP/1 ready), or 0 (not ready/error).
func detectHTTPProtocolVersion(config HTTPProbeConfigOptions) (int, error) {
	// http.Client does not fallback to from h2c to http1, we need to make two requests ourselves
	httpClient := &http.Client{
		Timeout:   config.Timeout,
		Transport: cleartextProbeTransport(2),
	}

	url, err := getURL(config)
	if err != nil {
		return 0, fmt.Errorf("error constructing probe url %w", err)
	}

	// do a simple GET request as Kubernetes does, avoid non-standard methods like HEAD
	//nolint:noctx // timeout is specified on the http.Client above
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("error constructing probe request %w", err)
	}
	req.Header.Add(netheader.UserAgentKey, netheader.KubeProbeUAPrefix+config.KubeMajor+"/"+config.KubeMinor)

	if res, err := httpClient.Do(req); err == nil {
		defer res.Body.Close()

		// ignore non-ready http2 responses and continue with http1, http2 might not be properly supported
		if isHTTPProbeReady(res) {
			return 2, nil
		}
	}

	// fallback to check http1
	httpClient.Transport = cleartextProbeTransport(1)
	res, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}

	defer res.Body.Close()

	if isHTTPProbeReady(res) {
		return 1, nil
	}

	return 0, fmt.Errorf("HTTP probe did not respond Ready, got status code: %d", res.StatusCode)
}

// HTTPProbe checks that HTTP connection can be established to the address.
func HTTPProbe(config HTTPProbeConfigOptions) error {
	if config.MaxProtoMajor == 0 {
		// If we don't know if the connection supports HTTP2, we will try it.
		// NOTE: the result is not cached right now, every probe attempts http2 detection again

		// If maxProto is 0, container is not ready, so we don't know whether http2 is supported.
		// If maxProto is 1, we know we're ready, but we also can't upgrade, so just return.
		// If maxProto is 2, we know we can upgrade to http2
		maxProto, err := detectHTTPProtocolVersion(config)
		if err != nil {
			return fmt.Errorf("failed to run HTTP protocol probe with error: %w", err)
		}
		config.MaxProtoMajor = maxProto
		if config.MaxProtoMajor == 1 {
			// probe already passed for HTTP/1.1 during auto detection, return early
			return nil
		}
	}

	httpClient := &http.Client{
		Transport: cleartextProbeTransport(config.MaxProtoMajor),
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
