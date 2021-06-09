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

package sqp

import (
	"github.com/ease-lab/XDTprototype/transport"
	"github.com/ease-lab/XDTprototype/utils"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"io"
	"sync"

	log "github.com/sirupsen/logrus"
	"net"

	"github.com/ease-lab/XDTprototype/proto/crossXDT"
	"github.com/ease-lab/XDTprototype/proto/upXDT"

	"google.golang.org/grpc"
)

var config utils.Config

// bufferPool is responsible for managing bounded buffers of channels to store data
var bufferPool transport.BufferPool

type crossXDTServer struct {
	crossXDT.UnimplementedStreamDataServer
}

type upXDTServer struct {
	upXDT.UnimplementedStreamDataServer
}

// SendData is called by SrcFn to push data to sQP
func (s upXDTServer) SendData(srv upXDT.StreamData_SendDataServer) error {
	chunkCount := 0
	var key string
	var onlyOnce sync.Once
	var channel chan []byte
	var totalChunks int64
	for {
		chunk, err := srv.Recv()
		if err == io.EOF {
			log.Infof("sQP: %d chunks received", chunkCount)
			if config.Routing == utils.STORE_FORWARD {
				bufferPool.StoreChannel(key, totalChunks, channel)
			}
			return srv.SendAndClose(&upXDT.Empty{})
		}
		if err != nil {
			log.Errorf("sQP: receive error: %v", err)
			return err
		}
		log.Debugf("sQP: Key received: %s in chunk %d", key, chunkCount)
		onlyOnce.Do(func() {
			key = chunk.Key
			totalChunks = chunk.TotalChunks
			log.Debugf("sQP: requesting a new channel")
			if config.Routing == utils.CUT_THROUGH {
				channel = bufferPool.CreateChannel()
				bufferPool.StoreChannel(key, totalChunks, channel)
			} else if config.Routing == utils.STORE_FORWARD {
				channel = bufferPool.CreateChannel()
			} else {
				log.Errorf("sQP: Invalid route type. Check config.json")
			}
			log.Debugf("sQP: channel allocated")
			log.Infof("sQP: chunkTotal = %d", totalChunks)
		})
		log.Debugf("sQP: Enquing chunk number %d", chunkCount)
		channel <- chunk.Chunk
		chunkCount += 1
	}
}

// ServeData is the gRPC server to serve the available data to the dQP
func (s crossXDTServer) ServeData(in *crossXDT.Request, srv crossXDT.StreamData_ServeDataServer) error {

	log.Infof("sQP: dQP is fetching key: %s", in.Key)

	chunkCount := 0
	var channel chan []byte
	var chunkTotal int64

	// Check whether the first packet has been received at sQP or not
	for {
		if channel, chunkTotal = bufferPool.GetChannel(in.Key); channel != nil {
			log.Infof("sQP: found chunkTotal %d for key %s", chunkTotal, in.Key)
			break
		}
	}
	// Send packets from the channel one by one
	for {
		select {
		case chunk := <-channel:
			resp := crossXDT.Response{Chunk: chunk, TotalChunks: chunkTotal}
			if err := srv.Send(&resp); err != nil {
				log.Errorf("sQP: send error %v", err)
				bufferPool.FreeChannel(in.Key)
				return err
			}
			log.Debugf("sQP: pushing chunk no. %d to dQP", chunkCount)
			chunkCount += 1
		default:
			if chunkTotal == int64(chunkCount) {
				bufferPool.FreeChannel(in.Key)
				return nil
			}
		}
	}
}

// StartServer starts the SrcQP server
func StartServer(receivedConfig utils.Config) {

	config = receivedConfig
	bufferPool.Init(config)

	lis, err := net.Listen("tcp", config.SQPServerAddr)
	if err != nil {
		log.Fatalf("sQP: failed to listen: %v", err)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()))
	upXDT.RegisterStreamDataServer(server, upXDTServer{})
	crossXDT.RegisterStreamDataServer(server, crossXDTServer{})

	log.Infoln("sQP: start server")
	if err := server.Serve(lis); err != nil {
		log.Fatalf("sQP: failed to serve: %v", err)
	}
}
