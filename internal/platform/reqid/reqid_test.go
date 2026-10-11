package reqid_test

import (
	"context"
	"testing"

	"github.com/thomas666-beast/nexus-payments/internal/platform/reqid"
)

func TestEnsure_CreatesIfMissing(t *testing.T) {
	ctx, id := reqid.Ensure(context.Background())
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if got := reqid.FromContext(ctx); got != id {
		t.Fatalf("ctx id mismatch: %s vs %s", got, id)
	}
}

func TestEnsure_KeepsExisting(t *testing.T) {
	ctx := reqid.WithRequestID(context.Background(), "existing")
	ctx2, id := reqid.Ensure(ctx)
	if id != "existing" {
		t.Fatalf("want existing, got %s", id)
	}
	if reqid.FromContext(ctx2) != "existing" {
		t.Fatal("id not preserved")
	}
}
