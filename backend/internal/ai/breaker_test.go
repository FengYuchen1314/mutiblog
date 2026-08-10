package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/config"
)

func TestBreakerOpensAfterFiveFailures(t *testing.T) {
	b := newBreaker()
	now := time.Unix(1_700_000_000, 0)
	b.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if !b.Allow() {
			t.Fatalf("request %d refused while closed", i+1)
		}
		b.Report(false)
	}
	if b.State() != "open" {
		t.Fatalf("state = %s, want open", b.State())
	}
	if b.Allow() {
		t.Fatal("request admitted while open")
	}
	if err := errors.Is(ErrCircuitOpen, ErrCircuitOpen); !err {
		t.Fatal("ErrCircuitOpen mismatch")
	}
}

func TestBreakerHalfOpenProbeAndReopen(t *testing.T) {
	b := newBreaker()
	now := time.Unix(1_700_000_000, 0)
	b.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		b.Allow()
		b.Report(false)
	}
	if b.RetryIn() <= 0 {
		t.Fatalf("retryIn = %d", b.RetryIn())
	}
	now = now.Add(61 * time.Second)
	if !b.Allow() {
		t.Fatal("probe not admitted after duration")
	}
	if b.Allow() {
		t.Fatal("second half-open probe admitted")
	}
	b.Report(false)
	if b.State() != "open" || b.RetryIn() <= 0 {
		t.Fatalf("state=%s retryIn=%d after failed probe", b.State(), b.RetryIn())
	}
	if b.duration <= breakerBaseDuration {
		t.Fatalf("duration did not double: %v", b.duration)
	}
}

func TestBreakerClosesOnSuccess(t *testing.T) {
	b := newBreaker()
	now := time.Unix(1_700_000_000, 0)
	b.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		b.Allow()
		b.Report(false)
	}
	now = now.Add(61 * time.Second)
	b.Allow()
	b.Report(true)
	if b.State() != "closed" || b.RetryIn() != 0 {
		t.Fatalf("state=%s retryIn=%d", b.State(), b.RetryIn())
	}
	if b.duration != breakerBaseDuration {
		t.Fatalf("duration not reset: %v", b.duration)
	}
}

func TestBreakerReset(t *testing.T) {
	b := newBreaker()
	for i := 0; i < 5; i++ {
		b.Allow()
		b.Report(false)
	}
	b.Reset()
	if b.State() != "closed" || !b.Allow() {
		t.Fatal("reset did not close the breaker")
	}
}

// TestBreakerSkipsProviderRequests verifies the sixth task is refused without
// hitting the mock provider after five failures.
func TestBreakerSkipsProviderRequests(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	p, err := NewOpenAICompatible(config.AIConfig{
		BaseURL:    server.URL,
		APIKey:     "k",
		Model:      "m",
		Timeout:    config.Duration(time.Second),
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = p.Translate(context.Background(), []Segment{{Text: "x"}}, "en", "zh-CN", "t")
	}
	if calls.Load() != 5 {
		t.Fatalf("calls = %d, want 5", calls.Load())
	}
	_, err = p.Translate(context.Background(), []Segment{{Text: "x"}}, "en", "zh-CN", "t")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("sixth call err = %v, want ErrCircuitOpen", err)
	}
	if calls.Load() != 5 {
		t.Fatalf("sixth call hit provider: calls = %d", calls.Load())
	}
}
