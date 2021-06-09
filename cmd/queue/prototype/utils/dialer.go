// MIT License
//
// Copyright (c) 2021 Shyam Jesalpura and EASE lab
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package utils

import (
	"context"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"net"
	"os"
	"syscall"
	"time"
)

func GetGopts() []grpc.DialOption {
	backoffConfig := backoff.DefaultConfig
	backoffConfig.MaxDelay = time.Duration(LoadConfig.RPCTimeoutMaxBackoff) * time.Millisecond
	connParams := grpc.ConnectParams{
		Backoff: backoffConfig,
	}

	gopts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithInsecure(),
		grpc.FailOnNonTempDialError(true),
		grpc.WithConnectParams(connParams),
		grpc.WithContextDialer(contextDialer),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
	}

	return gopts
}

type dialResult struct {
	c   net.Conn
	err error
}

func contextDialer(ctx context.Context, address string) (net.Conn, error) {
	if deadline, ok := ctx.Deadline(); ok {
		return timeoutDialer(address, time.Until(deadline))
	}
	return timeoutDialer(address, 0)
}

func isConnRefused(err error) bool {
	if err != nil {
		if nerr, ok := err.(*net.OpError); ok {
			if serr, ok := nerr.Err.(*os.SyscallError); ok {
				if serr.Err == syscall.ECONNREFUSED {
					return true
				}
			}
		}
	}
	return false
}

func timeoutDialer(address string, timeout time.Duration) (net.Conn, error) {
	var (
		stopC = make(chan struct{})
		synC  = make(chan *dialResult)
	)
	go func() {
		defer close(synC)
		for {
			select {
			case <-stopC:
				return
			default:
				c, err := net.DialTimeout("tcp", address, timeout)
				if isConnRefused(err) {
					<-time.After(time.Duration(LoadConfig.RPCRetryDelay) * time.Millisecond)
					continue
				}
				if err != nil {
					log.Debug("Reconnecting after an error")
					<-time.After(time.Duration(LoadConfig.RPCRetryDelay) * time.Millisecond)
					continue
				}

				synC <- &dialResult{c, err}
				return
			}
		}
	}()
	select {
	case dr := <-synC:
		return dr.c, dr.err
	case <-time.After(timeout):
		close(stopC)
		go func() {
			dr := <-synC
			if dr != nil && dr.c != nil {
				dr.c.Close()
			}
		}()
		return nil, errors.Errorf("dial %s: timeout", address)
	}
}
