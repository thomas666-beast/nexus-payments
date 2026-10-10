package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is the process-wide metrics bundle.
type Metrics struct {
	TransfersTotal     *prometheus.CounterVec
	TransferDuration   prometheus.Histogram
	TransferErrors     *prometheus.CounterVec
	IdempotencyReplays prometheus.Counter
	DBPoolConnections  *prometheus.GaugeVec
	GRPCRequestsTotal  *prometheus.CounterVec
	HTTPRequestsTotal  *prometheus.CounterVec
}

// New creates and registers metrics on reg. Pass nil for the default registry.
func New(reg prometheus.Registerer) *Metrics {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	m := &Metrics{
		TransfersTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_transfers_total",
			Help: "Total number of transfer attempts by final status.",
		}, []string{"status"}),

		TransferDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "nexus_transfer_duration_seconds",
			Help:    "Transfer end-to-end duration in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),

		TransferErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_transfer_errors_total",
			Help: "Transfer errors by category.",
		}, []string{"code"}),

		IdempotencyReplays: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "nexus_idempotency_replays_total",
			Help: "Number of idempotent replays served from storage.",
		}),

		DBPoolConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "nexus_db_pool_connections",
			Help: "Current DB pool connections by state.",
		}, []string{"state"}),

		GRPCRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_grpc_requests_total",
			Help: "Total gRPC requests by method and code.",
		}, []string{"method", "code"}),

		HTTPRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nexus_http_requests_total",
			Help: "Total HTTP requests by method, path, and status.",
		}, []string{"method", "path", "status"}),
	}

	reg.MustRegister(
		m.TransfersTotal,
		m.TransferDuration,
		m.TransferErrors,
		m.IdempotencyReplays,
		m.DBPoolConnections,
		m.GRPCRequestsTotal,
		m.HTTPRequestsTotal,
	)
	return m
}

// NewNoop returns an unregistered Metrics suitable for tests.
// Nil-safe: callers can pass this everywhere a *Metrics is expected.
func NewNoop() *Metrics {
	return &Metrics{
		TransfersTotal:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_transfers"}, []string{"status"}),
		TransferDuration:   prometheus.NewHistogram(prometheus.HistogramOpts{Name: "noop_transfer_duration"}),
		TransferErrors:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_transfer_errors"}, []string{"code"}),
		IdempotencyReplays: prometheus.NewCounter(prometheus.CounterOpts{Name: "noop_idempotency_replays"}),
		DBPoolConnections:  prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "noop_db_pool"}, []string{"state"}),
		GRPCRequestsTotal:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_grpc_requests"}, []string{"method", "code"}),
		HTTPRequestsTotal:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_http_requests"}, []string{"method", "path", "status"}),
	}
}
