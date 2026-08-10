// 熔断器：provider 连续失败时拒绝请求，避免每个任务各自重试烧 token
// （docs/12 §4）。
package ai

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned while the breaker refuses new requests. Callers
// must leave the task pending instead of consuming retry attempts.
var ErrCircuitOpen = errors.New("AI provider circuit breaker is open")

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

const (
	breakerFailureThreshold = 5
	breakerBaseDuration     = 60 * time.Second
	breakerMaxDuration      = 15 * time.Minute
)

// Breaker implements a closed/open/half-open circuit with exponential
// backoff. It is safe for concurrent use.
type Breaker struct {
	mu        sync.Mutex
	state     breakerState
	failures  int
	openedAt  time.Time
	duration  time.Duration
	halfTried bool
	now       func() time.Time
}

func newBreaker() *Breaker {
	return &Breaker{
		state:    breakerClosed,
		duration: breakerBaseDuration,
		now:      time.Now,
	}
}

// Allow reports whether a request may proceed. In half-open state exactly one
// probe is admitted; while open, requests are refused.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if b.now().After(b.openedAt.Add(b.duration)) {
			b.state = breakerHalfOpen
			b.halfTried = true
			return true
		}
		return false
	case breakerHalfOpen:
		if b.halfTried {
			return false
		}
		b.halfTried = true
		return true
	}
	return false
}

// Report records the outcome of an admitted request.
func (b *Breaker) Report(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.failures = 0
		b.duration = breakerBaseDuration
		b.state = breakerClosed
		return
	}
	switch b.state {
	case breakerHalfOpen:
		b.state = breakerOpen
		b.openedAt = b.now()
		b.duration = minDuration(b.duration*2, breakerMaxDuration)
	case breakerClosed:
		b.failures++
		if b.failures >= breakerFailureThreshold {
			b.state = breakerOpen
			b.openedAt = b.now()
		}
	}
}

// State returns a stable identifier for status reporting.
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return "open"
	case breakerHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// RetryIn returns the seconds until the open breaker may admit a probe.
func (b *Breaker) RetryIn() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != breakerOpen {
		return 0
	}
	remaining := b.openedAt.Add(b.duration).Sub(b.now())
	if remaining < 0 {
		return 0
	}
	return int(remaining / time.Second)
}

// Reset closes the breaker and restores the base duration.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = breakerClosed
	b.failures = 0
	b.duration = breakerBaseDuration
	b.halfTried = false
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
