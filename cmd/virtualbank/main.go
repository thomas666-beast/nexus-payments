package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	bankv1 "github.com/thomas666-beast/nexus-payments/gen/bank/v1"
	"github.com/thomas666-beast/nexus-payments/internal/virtualbank"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	bankID := requireEnv("BANK_ID")
	dsn := requireEnv("BANK_DB_DSN")
	grpcAddr := getenv("BANK_GRPC_ADDR", ":7071")

	// Fault injection configuration
	failAfter, _ := strconv.ParseInt(getenv("FAIL_RESERVE_AFTER", "0"), 10, 64)
	failEvery := getenv("FAIL_RESERVE_EVERY", "") == "true"
	failCommit := getenv("FAIL_COMMIT_ALWAYS", "") == "true"
	latencyMs, _ := strconv.ParseInt(getenv("LATENCY_MS", "0"), 10, 64)

	faults := &virtualbank.FaultConfig{
		FailReserveAfter: failAfter,
		FailReserveEvery: failEvery,
		FailCommitAlways: failCommit,
		Latency:          time.Duration(latencyMs) * time.Millisecond,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	svc := virtualbank.New(pool)
	srv := virtualbank.NewServer(svc, bankID, faults)

	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	g := grpc.NewServer()
	bankv1.RegisterBankServer(g, srv)
	reflection.Register(g)

	go func() { _ = g.Serve(lis) }()
	log.Info("virtual bank started",
		"bank_id", bankID,
		"grpc", grpcAddr,
		"fail_reserve_after", failAfter,
		"fail_reserve_every", failEvery,
		"fail_commit_always", failCommit,
		"latency_ms", latencyMs,
	)

	<-ctx.Done()
	log.Info("shutting down")
	g.GracefulStop()
	return nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("env %s is required", key))
	}
	return v
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var _ = errors.Is
