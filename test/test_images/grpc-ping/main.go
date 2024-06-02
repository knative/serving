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
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"knative.dev/pkg/network"
	ping "knative.dev/serving/test/test_images/grpc-ping/proto"
)

const (
	defaultPort = "8080"
)

var (
	delay    int64
	hostname string
)

func pong(req *ping.Request) *ping.Response {
	return &ping.Response{Msg: req.Msg + hostname + os.Getenv("SUFFIX")}
}

type server struct{}

func (s *server) Ping(ctx context.Context, req *ping.Request) (*ping.Response, error) {
	log.Print("Received ping: ", req.Msg)

	time.Sleep(time.Duration(delay) * time.Millisecond)

	resp := pong(req)

	log.Print("Sending pong: ", resp.Msg)
	return resp, nil
}

func (s *server) PingStream(stream ping.PingService_PingStreamServer) error {
	log.Printf("Starting stream")
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			log.Printf("Ending stream")
			return nil
		}
		if err != nil {
			log.Print("Failed to receive ping: ", err)
			return err
		}

		log.Print("Received ping: ", req.Msg)

		resp := pong(req)

		log.Print("Sending pong: ", resp.Msg)
		err = stream.Send(resp)
		if err != nil {
			log.Print("Failed to send pong: ", err)
			return err
		}
	}
}

func main() {
	log.Print("Starting server on ", getPort())

	delay, _ = strconv.ParseInt(os.Getenv("DELAY"), 10, 64)
	log.Printf("Using DELAY of %d ms", delay)

	if wantHostname, _ := strconv.ParseBool(os.Getenv("HOSTNAME")); wantHostname {
		hostname, _ = os.Hostname()
		log.Print("Setting hostname in response ", hostname)
	}

	g := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(g, health.NewServer())
	ping.RegisterPingServiceServer(g, &server{})

	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && r.Header.Get("Content-Type") == "application/grpc" {
			g.ServeHTTP(w, r)
		}
	}

	s := network.NewServer(":"+getPort(), http.HandlerFunc(handler))
	log.Fatal(s.ListenAndServe())
}

func getPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return defaultPort
}
