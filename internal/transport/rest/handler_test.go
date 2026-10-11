package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
	restserver "github.com/thomas666-beast/nexus-payments/internal/transport/rest"
)

func setupREST(t *testing.T) (http.Handler, *store.DB, *pgxpool.Pool) {
	t.Helper()
	pool := newTestPool(t)
	truncate(t, pool)
	db := &store.DB{Pool: pool}
	ensureSystemAccount(t, db)
	seedAccount(t, db, "alice", "bank-test", "USD")
	seedAccount(t, db, "bob", "bank-test", "USD")
	fundAccount(t, pool, "alice", 1000, "USD")

	reg := adapter.NewRegistry()
	svc := service.New(db, ledger.New(nil), reg, service.DefaultConfig(), nil, nil)
	return restserver.NewHandler(svc).Routes(nil), db, pool
}

func TestREST_InitiateTransfer(t *testing.T) {
	h, _, _ := setupREST(t)

	body := map[string]any{
		"from_account": "alice",
		"to_account":   "bob",
		"amount":       map[string]any{"amount_minor": 250, "currency": "USD"},
	}
	buf, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "rest-test-1")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status want 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "SETTLED" {
		t.Fatalf("status want SETTLED, got %v", resp["status"])
	}
}

func TestREST_IdempotencyReplay(t *testing.T) {
	h, _, _ := setupREST(t)

	body := []byte(`{"from_account":"alice","to_account":"bob","amount":{"amount_minor":100,"currency":"USD"}}`)

	send := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(body))
		req.Header.Set("Idempotency-Key", "rest-replay")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status want 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}

	r1 := send()
	r2 := send()
	if r1["transfer_id"] != r2["transfer_id"] {
		t.Fatalf("replay returned different transfer: %v vs %v", r1["transfer_id"], r2["transfer_id"])
	}
}

func TestREST_InsufficientFunds_422(t *testing.T) {
	h, _, _ := setupREST(t)

	body := []byte(`{"from_account":"alice","to_account":"bob","amount":{"amount_minor":5000,"currency":"USD"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var dto map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto["code"] != "insufficient_funds" {
		t.Fatalf("code want insufficient_funds, got %s", dto["code"])
	}
}

func TestREST_GetBalance(t *testing.T) {
	h, _, _ := setupREST(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts/alice/balance", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var dto map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	bal := dto["balance"].(map[string]any)
	if bal["amount_minor"].(float64) != 1000 {
		t.Fatalf("balance want 1000, got %v", bal["amount_minor"])
	}
}

func TestREST_InvalidRequest_400(t *testing.T) {
	h, _, _ := setupREST(t)

	body := []byte(`{"from_account":"alice","to_account":"alice","amount":{"amount_minor":100,"currency":"USD"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/transfers", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

// keep compiler happy for unused helper imports in future edits
var _ = pgx.ErrNoRows
var _ = context.Background
var _ = time.Now
var _ = domain.USD
