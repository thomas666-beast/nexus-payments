package ratelimit

import (
	"net"
	"net/http"
)

// HTTPMiddleware returns middleware that limits requests per client key.
// keyFunc extracts the key from the request; if it returns "", the request
// is treated as unkeyed and allowed (fail-open).
//
// Fail-open is deliberate: a rate limiter must never take down the service
// it protects.
func HTTPMiddleware(l *Limiter, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !l.Allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded","code":"rate_limited"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// KeyByIP returns a keyFunc that uses the client's IP address.
// Honors X-Forwarded-For if present (single proxy assumption).
func KeyByIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the leftmost address (the original client).
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
