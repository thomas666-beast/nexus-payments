package virtualbank

import (
	"errors"
	"sync/atomic"
	"time"
)

// FaultConfig lets you inject failures via environment variables, so the
// same binary can act as a healthy bank or a broken one depending on env.
type FaultConfig struct {
	FailReserveAfter int64         // 0 = never fail
	FailReserveEvery bool          // if true, every reserve fails
	FailCommitAlways bool
	Latency          time.Duration

	reserveCount int64
}

var ErrInjected = errors.New("injected bank failure")

func (f *FaultConfig) BeforeReserve() error {
	if f.Latency > 0 {
		time.Sleep(f.Latency)
	}
	if f.FailReserveEvery {
		return ErrInjected
	}
	if f.FailReserveAfter > 0 {
		n := atomic.AddInt64(&f.reserveCount, 1)
		if n > f.FailReserveAfter {
			return ErrInjected
		}
	}
	return nil
}

func (f *FaultConfig) BeforeCommit() error {
	if f.Latency > 0 {
		time.Sleep(f.Latency)
	}
	if f.FailCommitAlways {
		return ErrInjected
	}
	return nil
}
