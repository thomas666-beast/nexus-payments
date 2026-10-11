package logging

import (
	"context"
	"log/slog"

	"github.com/thomas666-beast/nexus-payments/internal/platform/reqid"
)

// FromContext returns a logger enriched with the request ID from ctx.
func FromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	if id := reqid.FromContext(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}
