package server

import (
	"context"
	"fmt"

	pb "gotoolboxserver/proto/echo"
)

type EchoServer struct {
	pb.UnimplementedEchoServiceServer
}

func NewEchoServer() *EchoServer {
	return &EchoServer{}
}

func (s *EchoServer) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{
		Message: fmt.Sprintf("echo: %s", req.Message),
	}, nil
}
