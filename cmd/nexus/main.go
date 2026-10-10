package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	nexusv1 "github.com/thomas666-beast/nexus-payments/gen/nexus/v1"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/platform/metrics"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
	grpcserver "github.com/thomas666-beast/nexus-payments/internal/transport/grpc"
	"github.com/thomas666-beast/nexus-payments/internal/transport/health"
	restserver "github.com/thomas666-beast/nexus-payments/internal/transport/rest"
	"google.golang.org/grpc"
	"github.com/thomas666-beast/nexus-payments/internal/platform/ratelimit"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dsn := getenv("NEXUS_DSN", "postgres://nexus:nexus_dev_password@localhost:5432/nexus?sslmode=disable")
	grpcAddr := getenv("NEXUS_GRPC_ADDR", ":9090")
	httpAddr := getenv("NEXUS_HTTP_ADDR", ":8080")
	metricsAddr := getenv("NEXUS_METRICS_ADDR", ":9100")
	// Rate limiters: 100 req/s per key, burst 200, LRU cap 50k keys.
	globalLimiter := ratelimit.New(100, 200, 50_000)
	// writeLimiter := ratelimit.New(10, 20, 50_000)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	m := metrics.New(nil)
	eng := ledger.New(nil)
	reg := adapter.NewRegistry()
	svc := service.New(db, eng, reg, service.DefaultConfig(), log, m)

	reaper := service.NewReaper(db, service.DefaultReaperConfig(), log)
	go reaper.Run(ctx)

	reconciler := service.NewReconciler(db, reg, service.DefaultReconcilerConfig(), m, log)
	go reconciler.Run(ctx)

	// gRPC server
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(ratelimit.UnaryServerInterceptor(globalLimiter)),
	)
	nexusv1.RegisterNexusServer(grpcSrv, grpcserver.NewServer(svc))

	// HTTP server (REST + health)
	httpMux := http.NewServeMux()
	restHandler := restserver.NewHandler(svc).Routes(m)
	limitedREST := ratelimit.HTTPMiddleware(globalLimiter, ratelimit.KeyByIP)(restHandler)
	httpMux.Handle("/v1/", limitedREST)

	healthH := health.New(db)
	httpMux.HandleFunc("GET /healthz", healthH.Liveness)
	httpMux.HandleFunc("GET /readyz", healthH.Readiness)
	httpSrv := &http.Server{
		Addr:              httpAddr,
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Metrics server
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsSrv := &http.Server{
		Addr:              metricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 3)
	go func() { errCh <- grpcSrv.Serve(grpcLis) }()
	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- metricsSrv.ListenAndServe() }()

	log.Info("nexus-payments started",
		"grpc", grpcAddr,
		"http", httpAddr,
		"metrics", metricsAddr,
	)

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	grpcSrv.GracefulStop()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = metricsSrv.Shutdown(shutdownCtx)
	log.Info("shutdown complete")
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
