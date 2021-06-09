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

package dqp

import (
	"github.com/ease-lab/XDTprototype/proto/crossXDT"
	"github.com/ease-lab/XDTprototype/proto/downXDT"
	"github.com/ease-lab/XDTprototype/proto/fnInvocation"
	"github.com/ease-lab/XDTprototype/transport"
	"github.com/ease-lab/XDTprototype/utils"
	"context"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"io"
	"net"
	"sync"
)

var config utils.Config

// bufferPool is responsible for managing bounded buffers of channels to store data
var bufferPool transport.BufferPool

type fnInvocationServer struct {
	fnInvocation.UnimplementedInvocationServer
}

type downXDTServer struct {
	downXDT.UnimplementedXDTtoFnServer
}

// XDTDataServe is a gRPC server to serve data to the DstFn
func (s downXDTServer) XDTDataServe(in *downXDT.DataRequest, srv downXDT.XDTtoFn_XDTDataServeServer) error {

	log.Infof("dQP: data being fetched by DstFn using key : %s", in.Key)

	chunkCount := 0
	var channel chan []byte
	var chunkTotal int64
	// Check whether the first packet has been received at dQP or not
	for {
		if channel, chunkTotal = bufferPool.GetChannel(in.Key); channel != nil {
			log.Infof("dQP: found chunkTotal %d for key %s at sQP", chunkTotal, in.Key)
			break
		}
	}

	for {
		select {
		case chunk := <-channel:
			resp := downXDT.Data{Chunk: chunk, TotalChunks: chunkTotal}
			if err := srv.Send(&resp); err != nil {
				log.Errorf("dQP: send error %v", err)
				bufferPool.FreeChannel(in.Key)
				return err
			}
			log.Debugf("dQP: Sending chunk : %d to DstFn", chunkCount)
			chunkCount += 1
		default:
			if chunkTotal == int64(chunkCount) {
				bufferPool.FreeChannel(in.Key)
				return nil
			}
		}
	}
}

func invokeDestinationHandler(ctx context.Context, XDTJSON []byte) error {
	errorRouteInvocation := make(chan error, 1)

	go func() {
		conn, err := grpc.DialContext(ctx, config.DstServerAddr, utils.GetGopts()...)
		if err != nil {
			log.Errorf("dQP: RouteInvocation: did not connect: %v", err)
			errorRouteInvocation <- err
			return
		}
		c := downXDT.NewXDTtoFnClient(conn)
		log.Infof("dQP: DST invocation start")
		_, err = c.XDTFnCall(ctx, &downXDT.InvocationRequest{XDTJSON: XDTJSON})
		if err != nil {
			log.Errorf("dQP: Fn invocation route unsuccessful")
			errorRouteInvocation <- err
			return
		}
		log.Infof("dQP: Fn invocation route successful")
		// need some help in closing this connection in case of an error.
		err = conn.Close()
		if err != nil {
			log.Errorf("dQP: Error closing the connection to Dest")
			errorRouteInvocation <- err
			return
		}
		errorRouteInvocation <- nil
	}()
	select {
	case <-ctx.Done():
		log.Errorf("SDK: context expired at RouteInvocation@dQP")
		<-errorRouteInvocation
		return ctx.Err()
	case err := <-errorRouteInvocation:
		if err != nil {
			log.Infof("dQP: Fn invocation route unsuccessful err: %v", err)
			return err
		} else {
			log.Infof("dQP: Fn invocation route successful")
			return nil
		}
	}
}

// RouteInvocation is a gRPC server to route the function call from SrcFn to the DstFn
func (s fnInvocationServer) RouteInvocation(ctx context.Context, in *fnInvocation.InvocationRequest) (*fnInvocation.Empty, error) {

	log.Infof("dQP: received serialised json: %s", in.XDTJSON)

	var xdtPayload utils.Payload
	if err := json.Unmarshal(in.XDTJSON, &xdtPayload); err != nil {
		log.Error("dQP: RouteInvocation", err)
		return &fnInvocation.Empty{}, err
	}

	log.Infof("dQP: fetched data from sQP using key : %s", xdtPayload.Key)

	errorPullDataFromSrcQP := make(chan error, 1)
	go func() {
		errorPullDataFromSrcQP <- PullDataFromSrcQP(ctx, xdtPayload.Key, in.SQPAddr, config.ChunkSizeInBytes)
	}()
	if config.Routing == utils.STORE_FORWARD {
		log.Infof("dQP: [Store & Forward]: pulling data from sQP")
		select {
		case <-ctx.Done():
			<-errorPullDataFromSrcQP // Wait for f to return.
			return &fnInvocation.Empty{}, ctx.Err()
		case err := <-errorPullDataFromSrcQP:
			if err != nil {
				log.Errorf("dQP: [Store & Forward] Pull data failed")
				return &fnInvocation.Empty{}, err
			}
		}
	}

	err := invokeDestinationHandler(ctx, in.XDTJSON)
	if err != nil {
		return &fnInvocation.Empty{}, err
	}

	if config.Routing == utils.CUT_THROUGH {
		log.Infof("dQP: [cut-through]: pulling data from sQP")
		select {
		case <-ctx.Done():
			<-errorPullDataFromSrcQP
			return &fnInvocation.Empty{}, ctx.Err()
		case err := <-errorPullDataFromSrcQP:
			if err != nil {
				log.Errorf("dQP: [cut-through]: pulling data from sQP failed")
				return &fnInvocation.Empty{}, err
			}
		}
	}
	return &fnInvocation.Empty{}, nil
}

// PullDataFromSrcQP pulls data from src QP to dst QP
func PullDataFromSrcQP(ctx context.Context, key string, sQPAddr string, chunkSizeInBytes int) error {

	conn, err := utils.GetGRPCConn(ctx, sQPAddr, true)
	if err != nil {
		log.Errorf("SRC: can not connect with server %v", err)
		return err
	}

	client := crossXDT.NewStreamDataClient(conn)
	in := &crossXDT.Request{Key: key, ChunkSize: int64(chunkSizeInBytes)}
	stream, err := client.ServeData(ctx, in)
	if err != nil {
		log.Errorf("dQP: open stream error %v", err)
		return err
	}

	chunkCount := 0
	var onlyOnce sync.Once
	var channel chan []byte
	var totalChunks int64
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			log.Infof("dQP: %d chunks received", chunkCount)
			if config.Routing == utils.STORE_FORWARD {
				bufferPool.StoreChannel(key, totalChunks, channel)
			}
			return nil
		}
		if err != nil {
			log.Errorf("dQP: receive error: %v", err)
			return err
		}
		log.Debugf("dQP: Received chunk no. %d", chunkCount)
		onlyOnce.Do(func() {
			totalChunks = chunk.TotalChunks
			log.Debugf("dQP: requesting a new channel")
			if config.Routing == utils.CUT_THROUGH {
				channel = bufferPool.CreateChannel()
				bufferPool.StoreChannel(key, chunk.TotalChunks, channel)
			} else if config.Routing == utils.STORE_FORWARD {
				channel = bufferPool.CreateChannel()
			} else {
				log.Errorf("dQP: Invalid route type. Check config.json")
			}
			log.Debugf("dQP: channel allocated")
			log.Infof("dQP: chunkTotal = %d", chunk.TotalChunks)
		})
		log.Debugf("dQP: Enquing chunk number %d", chunkCount)
		channel <- chunk.Chunk
		chunkCount += 1
	}
}

// StartServer starts DstQP server
func StartServer(receivedConfig utils.Config) {

	config = receivedConfig
	bufferPool.Init(config)

	lis, err := net.Listen("tcp", config.DQPServerAddr)
	if err != nil {
		log.Fatalf("dQP: failed to listen: %v", err)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()))
	downXDT.RegisterXDTtoFnServer(server, downXDTServer{})
	fnInvocation.RegisterInvocationServer(server, fnInvocationServer{})

	log.Infoln("dQP: start server")
	if err := server.Serve(lis); err != nil {
		log.Fatalf("dQP: failed to serve: %v", err)
	}
}
