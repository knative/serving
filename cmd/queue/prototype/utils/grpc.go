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
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

func GetGRPCConn(ctx context.Context, url string, dialerRequired bool) (*grpc.ClientConn, error) {
	errorChannel := make(chan error, 1)
	connChannel := make(chan *grpc.ClientConn, 1)
	var conn *grpc.ClientConn
	go func() {
		var err error
		if dialerRequired {
			conn, err = grpc.DialContext(ctx, url, GetGopts()...)
		} else {
			conn, err = grpc.DialContext(ctx, url,
				grpc.WithBlock(),
				grpc.WithInsecure(),
				grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
				grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()))
		}
		if err != nil {
			errorChannel <- err
			return
		} else {
			connChannel <- conn
			return
		}
	}()
	select {
	case <-ctx.Done():
		<-errorChannel
		return nil, ctx.Err()
	case err := <-errorChannel:
		return nil, err
	case conn = <-connChannel:
		return conn, nil
	}
}
