package server

import (
	"context"
	"fmt"

	pb "gotoolboxserver/proto/echo"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("echo-server/handler")

type EchoServer struct {
	pb.UnimplementedEchoServiceServer
	db *pgxpool.Pool
}

func NewEchoServer(db *pgxpool.Pool) *EchoServer {
	return &EchoServer{
		db: db,
	}
}

func (s *EchoServer) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	_, span := tracer.Start(ctx, "EchoServer.Echo")
	defer span.End()

	span.SetAttributes(
		attribute.String("echo.input_message", req.Message),
		attribute.Int("echo.input_length", len(req.Message)),
	)

	if req.Message == "" {
		span.SetStatus(codes.Error, "empty message received")
		return nil, fmt.Errorf("message cannot be empty")
	}

	if err := s.logEchoTwice(ctx, req.Message); err != nil {
		span.SetStatus(codes.Error, "failed to log echo")
		return nil, err
	}

	result := fmt.Sprintf("echo: %s", req.Message)
	span.SetAttributes(attribute.String("echo.output_message", result))

	return &pb.EchoResponse{
		Message: result,
	}, nil
}

// logEchoTwice demonstrates a transaction spanning two inserts, so we can see
// how otelpgx represents BEGIN/queries/COMMIT (or ROLLBACK) in the trace.
func (s *EchoServer) logEchoTwice(ctx context.Context, message string) error {
	ctx, span := tracer.Start(ctx, "logEchoTwice")
	defer span.End()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "failed to begin transaction")
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op if already committed

	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", message)
	if err != nil {
		span.SetStatus(codes.Error, "first insert failed")
		return fmt.Errorf("first insert: %w", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", message+" (copy)")
	if err != nil {
		span.SetStatus(codes.Error, "second insert failed")
		return fmt.Errorf("second insert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		span.SetStatus(codes.Error, "commit failed")
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}
