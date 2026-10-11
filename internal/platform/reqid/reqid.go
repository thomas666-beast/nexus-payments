package reqid

import (
	"context"

	"github.com/google/uuid"
)

type contextKey struct{}

// FromContext returns the request ID stored in ctx, or empty string.
func FromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

// WithRequestID stores id in ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// New returns a fresh request ID.
func New() string { return uuid.NewString() }

// Ensure returns the existing request ID if present, otherwise creates one
// and returns the derived context.
func Ensure(ctx context.Context) (context.Context, string) {
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := New()
	return WithRequestID(ctx, id), id
}
