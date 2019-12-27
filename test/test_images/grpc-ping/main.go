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

package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"

	"google.golang.org/grpc"

	"knative.dev/pkg/network"
	servingnetwork "knative.dev/serving/pkg/network"
	ping "knative.dev/serving/test/test_images/grpc-ping/proto"
)

func pong(req *ping.Request) *ping.Response {
	return &ping.Response{Msg: req.Msg + os.Getenv("SUFFIX")}
}

type server struct{}

func (s *server) Ping(ctx context.Context, req *ping.Request) (*ping.Response, error) {
	log.Printf("Received ping: %v", req.Msg)

	resp := pong(req)

	log.Printf("Sending pong: %v", resp.Msg)
	return resp, nil
}

func (s *server) PingStream(stream ping.PingService_PingStreamServer) error {
	log.Printf("Starting stream")
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Printf("Ending stream")
			return nil
		}
		if err != nil {
			log.Printf("Failed to receive ping: %v", err)
			return err
		}

		log.Printf("Received ping: %v", req.Msg)

		resp := pong(req)

		log.Printf("Sending pong: %v", resp.Msg)
		err = stream.Send(resp)
		if err != nil {
			log.Printf("Failed to send pong: %v", err)
			return err
		}
	}
}

func httpWrapper(g *grpc.Server) http.Handler {
	return servingnetwork.NewProbeHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ProtoMajor == 2 && r.Header.Get("Content-Type") == "application/grpc" {
				g.ServeHTTP(w, r)
			}
		}),
	)
}

func main() {
	log.Printf("Starting server on %s", os.Getenv("PORT"))

	g := grpc.NewServer()
	s := network.NewServer(":"+os.Getenv("PORT"), httpWrapper(g))

	ping.RegisterPingServiceServer(g, &server{})

	log.Fatal(s.ListenAndServe())
}
