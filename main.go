package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	pb "gotoolboxserver/proto/echo"
	"gotoolboxserver/server"
)

// ---- request ID middleware ----

type ctxKey string

const requestIDKey ctxKey = "requestID"

func newRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// httpRequestIDMiddleware stamps a request ID into context and logs
// start/end — this is the seed for whatever the TUI will eventually consume.
func httpRequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)

		r.Header.Set("X-Request-Id", reqID) // grpc-gateway will forward this via its header matcher

		log.Printf("[%s] --> %s %s", reqID, r.Method, r.URL.Path)
		next.ServeHTTP(w, r.WithContext(ctx))
		log.Printf("[%s] <-- done", reqID)
	})
}

// grpcRequestIDInterceptor does the same thing for direct gRPC calls
// (not routed through the HTTP gateway).
func grpcRequestIDInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqID := newRequestID()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get("x-request-id"); len(ids) > 0 {
			reqID = ids[0]
		}
	}

	ctx = context.WithValue(ctx, requestIDKey, reqID)

	log.Printf("[%s] --> %s", reqID, info.FullMethod)
	resp, err := handler(ctx, req)
	log.Printf("[%s] <-- done (err=%v)", reqID, err)
	return resp, err
}

func customHeaderMatcher(key string) (string, bool) {
	if key == "X-Request-Id" {
		return "x-request-id", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

// ---- server startup ----

func runGRPC() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpcRequestIDInterceptor),
	)
	pb.RegisterEchoServiceServer(grpcServer, server.NewEchoServer())

	log.Println("gRPC server listening on :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("grpc serve error: %v", err)
	}
}

func runHTTP() {
	ctx := context.Background()
	mux := runtime.NewServeMux(
		runtime.WithIncomingHeaderMatcher(customHeaderMatcher),
	)

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err := pb.RegisterEchoServiceHandlerFromEndpoint(ctx, mux, "localhost:50051", opts)
	if err != nil {
		log.Fatalf("failed to register gateway: %v", err)
	}

	log.Println("HTTP gateway listening on :8080")
	if err := http.ListenAndServe(":8080", httpRequestIDMiddleware(mux)); err != nil {
		log.Fatalf("http serve error: %v", err)
	}
}

func main() {
	go runGRPC()
	runHTTP() // blocks
	fmt.Println("shutting down")
}
