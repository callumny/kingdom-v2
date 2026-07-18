package modelapi

import (
	"context"
	"errors"
	"github.com/callumny/kingdom/internal/topology"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetryDelayIsCancellationAware(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer s.Close()
	c := NewClient()
	c.RetryDelay = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := c.Chat(ctx, ep(s.URL, topology.KindOllama), "m", nil)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRetryAfterDeltaAndDateAreCapped(t *testing.T) {
	for _, header := range []string{"10", time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)} {
		var calls atomic.Int32
		var first time.Time
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				first = time.Now()
				w.Header().Set("Retry-After", header)
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(`{"message":{"content":"ok"}}`))
		}))
		c := NewClient()
		c.RetryDelay = 0
		_, err := c.Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
		s.Close()
		elapsed := time.Since(first)
		if err != nil || calls.Load() != 2 || elapsed < 1800*time.Millisecond || elapsed > 3*time.Second {
			t.Fatalf("header=%q err=%v calls=%d elapsed=%v", header, err, calls.Load(), elapsed)
		}
	}
}

func TestNoRetryDelayForClientError(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(400) }))
	defer s.Close()
	c := NewClient()
	c.RetryDelay = time.Second
	start := time.Now()
	_, err := c.Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if err == nil || calls.Load() != 1 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}
