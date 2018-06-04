package main

import (
	"fmt"
	"log"
	"net"

	hello "github.com/elafros/elafros/test/e2e/test_images/helloworld-grpc/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type helloServer struct {
}

func (s *helloServer) Hello(ctx context.Context, req *hello.Request) (*hello.Response, error) {
	return &hello.Response{Msg: fmt.Sprintf("Hello %s", req.Msg)}, nil
}

func main() {
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 8080))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	helloServer := &helloServer{}

	grpcServer := grpc.NewServer()
	hello.RegisterHelloServiceServer(grpcServer, helloServer)
	grpcServer.Serve(lis)
}
