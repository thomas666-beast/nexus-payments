package breaker

import (
	"context"
	"errors"
	"time"

	"github.com/sony/gobreaker"
	"github.com/thomas666-beast/nexus-payments/internal/adapter"
	"github.com/thomas666-beast/nexus-payments/internal/domain"
)

var ErrCircuitOpen = errors.New("bank circuit breaker is open")

// Settings configures the breaker.
type Settings struct {
	MaxRequests uint32        // allowed requests in HALF_OPEN before deciding
	Interval    time.Duration // rolling window for counting failures in CLOSED
	Timeout     time.Duration // how long to stay OPEN before probing
	Threshold   uint32        // consecutive failures to trip
}

// DefaultSettings returns production-sane defaults.
func DefaultSettings() Settings {
	return Settings{
		MaxRequests: 1,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		Threshold:   5,
	}
}

// Adapter wraps any adapter.Adapter with a circuit breaker.
type Adapter struct {
	inner   adapter.Adapter
	breaker *gobreaker.CircuitBreaker
}

// New wraps inner with a breaker. Pass zero Settings for defaults.
func New(inner adapter.Adapter, s Settings) *Adapter {
	if s.Threshold == 0 {
		s = DefaultSettings()
	}
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "bank:" + inner.BankID(),
		MaxRequests: s.MaxRequests,
		Interval:    s.Interval,
		Timeout:     s.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= s.Threshold
		},
	})
	return &Adapter{inner: inner, breaker: cb}
}

func (a *Adapter) BankID() string { return a.inner.BankID() }

func (a *Adapter) Reserve(ctx context.Context, req adapter.ReserveRequest) (adapter.Reservation, error) {
	v, err := a.breaker.Execute(func() (any, error) {
		return a.inner.Reserve(ctx, req)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return adapter.Reservation{}, ErrCircuitOpen
		}
		return adapter.Reservation{}, err
	}
	return v.(adapter.Reservation), nil
}

func (a *Adapter) Commit(ctx context.Context, res adapter.Reservation) error {
	_, err := a.breaker.Execute(func() (any, error) {
		return nil, a.inner.Commit(ctx, res)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return ErrCircuitOpen
		}
		return err
	}
	return nil
}

func (a *Adapter) Rollback(ctx context.Context, res adapter.Reservation) error {
	// Rollback is a best-effort cleanup. Never let the breaker block it —
	// releasing funds should succeed even if the bank is flapping.
	return a.inner.Rollback(ctx, res)
}

func (a *Adapter) Reconcile(ctx context.Context, from, to time.Time) ([]domain.TransferID, error) {
	v, err := a.breaker.Execute(func() (any, error) {
		return a.inner.Reconcile(ctx, from, to)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, ErrCircuitOpen
		}
		return nil, err
	}
	return v.([]domain.TransferID), nil
}

// State exposes the current breaker state. Test-only helper.
func (a *Adapter) State() gobreaker.State { return a.breaker.State() }
