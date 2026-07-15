package server

import (
	"context"
	"fmt"

	pb "gotoolboxserver/proto/echo"

	"github.com/jackc/pgx/v5"
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

	_, err := s.db.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", req.Message)
	if err != nil {
		span.SetStatus(codes.Error, "failed to log echo")
		return nil, fmt.Errorf("db insert failed: %w", err)
	}

	result := fmt.Sprintf("echo: %s", req.Message)
	span.SetAttributes(attribute.String("echo.output_message", result))

	return &pb.EchoResponse{
		Message: result,
	}, nil
}

func (s *EchoServer) SlowEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	ctx, span := tracer.Start(ctx, "EchoServer.SlowEcho")
	defer span.End()

	span.SetAttributes(attribute.String("echo.input_message", req.Message))

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

	return &pb.EchoResponse{Message: result}, nil
}

func (s *EchoServer) FailEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	ctx, span := tracer.Start(ctx, "EchoServer.FailEcho")
	defer span.End()

	span.SetAttributes(attribute.String("echo.input_message", req.Message))

	if req.Message == "" {
		span.SetStatus(codes.Error, "empty message received")
		return nil, fmt.Errorf("message cannot be empty")
	}

	if err := s.logEchoWithFailure(ctx, req.Message); err != nil {
		span.SetStatus(codes.Error, "failed to log echo")
		return nil, err
	}

	// unreachable in practice, since logEchoWithFailure always errors,
	// but kept for symmetry with the other handlers
	result := fmt.Sprintf("echo: %s", req.Message)
	span.SetAttributes(attribute.String("echo.output_message", result))
	return &pb.EchoResponse{Message: result}, nil
}

func (s *EchoServer) DoubleTxEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	ctx, span := tracer.Start(ctx, "EchoServer.DoubleTxEcho")
	defer span.End()

	span.SetAttributes(attribute.String("echo.input_message", req.Message))

	if req.Message == "" {
		span.SetStatus(codes.Error, "empty message received")
		return nil, fmt.Errorf("message cannot be empty")
	}

	// BUG: this begins its own transaction and holds it open via a helper call...
	if err := s.beginAndDoWork(ctx, req.Message); err != nil {
		span.SetStatus(codes.Error, "work failed")
		return nil, err
	}

	result := fmt.Sprintf("echo: %s", req.Message)
	span.SetAttributes(attribute.String("echo.output_message", result))
	return &pb.EchoResponse{Message: result}, nil
}

func (s *EchoServer) FixedTxEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	ctx, span := tracer.Start(ctx, "EchoServer.FixedTxEcho")
	defer span.End()

	span.SetAttributes(attribute.String("echo.input_message", req.Message))

	if req.Message == "" {
		span.SetStatus(codes.Error, "empty message received")
		return nil, fmt.Errorf("message cannot be empty")
	}

	if err := s.beginAndDoWorkFixed(ctx, req.Message); err != nil {
		span.SetStatus(codes.Error, "work failed")
		return nil, err
	}

	result := fmt.Sprintf("echo: %s", req.Message)
	span.SetAttributes(attribute.String("echo.output_message", result))
	return &pb.EchoResponse{Message: result}, nil
}

// beginAndDoWorkFixed opens exactly one transaction, and passes it down to
// every helper it calls — no helper is allowed to open its own.
func (s *EchoServer) beginAndDoWorkFixed(ctx context.Context, message string) error {
	ctx, span := tracer.Start(ctx, "beginAndDoWorkFixed")
	defer span.End()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "failed to begin transaction")
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", message)
	if err != nil {
		span.SetStatus(codes.Error, "insert failed")
		return fmt.Errorf("insert: %w", err)
	}

	// FIX: pass tx down instead of letting this helper start its own
	if err := s.lookupMetadataCorrectly(ctx, tx, message); err != nil {
		span.SetStatus(codes.Error, "metadata lookup failed")
		return fmt.Errorf("metadata lookup: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		span.SetStatus(codes.Error, "commit failed")
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// lookupMetadataCorrectly is the fix: it accepts the caller's tx instead of
// opening its own, so this whole operation only ever uses one connection.
func (s *EchoServer) lookupMetadataCorrectly(ctx context.Context, tx pgx.Tx, message string) error {
	ctx, span := tracer.Start(ctx, "lookupMetadataCorrectly")
	defer span.End()
	span.SetAttributes(attribute.Bool("fix.reuses_caller_tx", true))

	var count int
	row := tx.QueryRow(ctx, "SELECT count(*) FROM echo_log WHERE message = $1", message)
	if err := row.Scan(&count); err != nil {
		span.SetStatus(codes.Error, "count query failed")
		return fmt.Errorf("count query: %w", err)
	}

	span.SetAttributes(attribute.Int("echo.matching_count", count))
	return nil
}

// beginAndDoWork opens a transaction, then — the bug — calls a helper that
// ALSO opens its own transaction against the pool, instead of reusing this
// one. Two live transactions/connections for what should be one logical unit.
func (s *EchoServer) beginAndDoWork(ctx context.Context, message string) error {
	ctx, span := tracer.Start(ctx, "beginAndDoWork")
	defer span.End()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "failed to begin outer transaction")
		return fmt.Errorf("begin outer tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", message)
	if err != nil {
		span.SetStatus(codes.Error, "outer insert failed")
		return fmt.Errorf("outer insert: %w", err)
	}

	// BUG: this should take `tx` as a parameter and use it — instead it opens
	// a brand new transaction against the pool while the outer one is still open.
	if err := s.lookupMetadataWrongly(ctx, message); err != nil {
		span.SetStatus(codes.Error, "metadata lookup failed")
		return fmt.Errorf("metadata lookup: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		span.SetStatus(codes.Error, "commit failed")
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// lookupMetadataWrongly is the bug: it independently begins its own
// transaction against the SAME pool, rather than accepting the caller's tx.
func (s *EchoServer) lookupMetadataWrongly(ctx context.Context, message string) error {
	ctx, span := tracer.Start(ctx, "lookupMetadataWrongly")
	defer span.End()
	span.SetAttributes(attribute.Bool("bug.opens_second_tx", true))

	tx, err := s.db.Begin(ctx) // <-- second connection, independent of the caller's
	if err != nil {
		span.SetStatus(codes.Error, "failed to begin inner transaction")
		return fmt.Errorf("begin inner tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var count int
	row := tx.QueryRow(ctx, "SELECT count(*) FROM echo_log WHERE message = $1", message)
	if err := row.Scan(&count); err != nil {
		span.SetStatus(codes.Error, "count query failed")
		return fmt.Errorf("count query: %w", err)
	}

	span.SetAttributes(attribute.Int("echo.matching_count", count))

	if err := tx.Commit(ctx); err != nil {
		span.SetStatus(codes.Error, "inner commit failed")
		return fmt.Errorf("inner commit: %w", err)
	}

	return nil
}

// logEchoWithFailure demonstrates a transaction where the second insert fails,
// forcing a ROLLBACK instead of a COMMIT — useful for seeing what a failed
// transaction looks like in a trace, versus a successful one.
func (s *EchoServer) logEchoWithFailure(ctx context.Context, message string) error {
	ctx, span := tracer.Start(ctx, "logEchoWithFailure")
	defer span.End()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "failed to begin transaction")
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // this is what actually fires here, since Commit is never reached

	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message) VALUES ($1)", message)
	if err != nil {
		span.SetStatus(codes.Error, "first insert failed")
		return fmt.Errorf("first insert: %w", err)
	}

	// deliberately broken — nonexistent_column doesn't exist on echo_log
	_, err = tx.Exec(ctx, "INSERT INTO echo_log (message, nonexistent_column) VALUES ($1, $2)", message+" (copy)", "x")
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

	_, err = tx.Exec(ctx, "SELECT pg_sleep(2)")
	if err != nil {
		span.SetStatus(codes.Error, "sleep failed")
		return fmt.Errorf("sleep: %w", err)
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
