package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/thomas666-beast/nexus-payments/internal/platform/ratelimit"
)

func TestLimiter_AllowsWithinBurst(t *testing.T) {
	// 10/sec, burst 3.
	l := ratelimit.New(10, 3, 100)
	for i := 0; i < 3; i++ {
		if !l.Allow("client-a") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("client-a") {
		t.Fatal("4th request should be rejected (burst=3)")
	}
}

func TestLimiter_PerKeyIsolation(t *testing.T) {
	l := ratelimit.New(10, 2, 100)
	// Client A exhausts its bucket.
	l.Allow("a")
	l.Allow("a")
	if l.Allow("a") {
		t.Fatal("client a should be limited")
	}
	// Client B is unaffected.
	if !l.Allow("b") {
		t.Fatal("client b should not be limited by client a")
	}
}

func TestLimiter_LRUEviction(t *testing.T) {
	l := ratelimit.New(10, 1, 3) // capacity 3
	l.Allow("a")
	l.Allow("b")
	l.Allow("c")
	if got := l.Size(); got != 3 {
		t.Fatalf("size want 3, got %d", got)
	}
	l.Allow("d") // evicts LRU
	if got := l.Size(); got != 3 {
		t.Fatalf("size after eviction want 3, got %d", got)
	}
}

func TestHTTPMiddleware_Rejects429(t *testing.T) {
	l := ratelimit.New(1, 1, 100)
	h := ratelimit.HTTPMiddleware(l, ratelimit.KeyByIP)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request: allowed.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request want 200, got %d", rec.Code)
	}

	// Second request from same IP: rejected.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request want 429, got %d", rec2.Code)
	}
}

func TestHTTPMiddleware_DifferentIPs(t *testing.T) {
	l := ratelimit.New(1, 1, 100)
	h := ratelimit.HTTPMiddleware(l, ratelimit.KeyByIP)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	req1.RemoteAddr = "10.0.0.1:1234"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("ip1 want 200, got %d", rec1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "10.0.0.2:1234"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("ip2 want 200, got %d", rec2.Code)
	}
}

func TestLimiter_RefillsOverTime(t *testing.T) {
	// 50/sec = one token every 20ms. Burst 1.
	l := ratelimit.New(50, 1, 10)
	if !l.Allow("k") {
		t.Fatal("first should pass")
	}
	if l.Allow("k") {
		t.Fatal("immediately after, should be limited")
	}
	time.Sleep(50 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("after refill, should pass")
	}
}