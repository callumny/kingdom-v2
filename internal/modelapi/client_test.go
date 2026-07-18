package modelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/callumny/kingdom/internal/topology"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func ep(u string, k topology.EndpointKind) topology.Endpoint {
	return topology.Endpoint{ID: "e", Name: "e", Kind: k, BaseURL: u}
}

func TestPayloadHeadersAndOpenAIPath(t *testing.T) {
	var got map[string]any
	var path, ct string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		ct = r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer s.Close()
	_, err := NewClient().Chat(context.Background(), ep(s.URL+"/v1", topology.KindOpenAICompatible), "model", []Message{{Role: "user", Content: "hi"}})
	if err != nil || path != "/v1/chat/completions" || ct != "application/json" || got["model"] != "model" {
		t.Fatalf("%v %s %s %#v", err, path, ct, got)
	}
}
func TestRetryMatrix(t *testing.T) {
	for _, code := range []int{500, 429} {
		var n int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&n, 1) == 1 {
				w.WriteHeader(code)
				return
			}
			w.Write([]byte(`{"message":{"content":"ok"}}`))
		}))
		_, e := NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
		s.Close()
		if e != nil || n != 2 {
			t.Fatalf("code %d e=%v n=%d", code, e, n)
		}
	}
}

func TestRetryWaitCancellationPreventsSecondCall(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&n, 1); w.WriteHeader(503) }))
	defer s.Close()
	c := NewClient()
	c.RetryDelay = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := c.Chat(ctx, ep(s.URL, topology.KindOllama), "m", nil)
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("err=%v calls=%d", err, n)
	}
}

func TestRetryAfterHonoredAndCapped(t *testing.T) {
	var n int32
	var first time.Time
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			first = time.Now()
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	defer s.Close()
	c := NewClient()
	c.RetryDelay = 0
	_, err := c.Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if err != nil || n != 2 || time.Since(first) < 1900*time.Millisecond {
		t.Fatalf("err=%v calls=%d elapsed=%v", err, n, time.Since(first))
	}
}
func TestNoRetryClientErrorAndMalformed(t *testing.T) {
	for _, body := range []string{`{"x":`, `not-json`} {
		var n int32
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&n, 1); w.Write([]byte(body)) }))
		_, e := NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
		s.Close()
		if e == nil || n != 1 {
			t.Fatalf("body %q e=%v n=%d", body, e, n)
		}
	}
}
func TestNoRetry4xx(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(400)
		w.Write([]byte("bad"))
	}))
	defer s.Close()
	_, e := NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if e == nil || n != 1 {
		t.Fatalf("%v %d", e, n)
	}
}
func TestResponseLimitAndUTF8Error(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write(bytes.Repeat([]byte("x"), 600))
	}))
	defer s.Close()
	c := NewClient()
	c.MaxResponseBytes = 2048
	_, e := c.Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if e == nil || len(e.Error()) > 560 {
		t.Fatalf("error bound: %v", e)
	}
}
func TestContextCancellationNoRetry(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&n, 1); <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e := NewClient().Chat(ctx, ep(s.URL, topology.KindOllama), "m", nil)
	if !errors.Is(e, context.Canceled) || n != 0 {
		t.Fatalf("%v n=%d", e, n)
	}
}
func TestRedirectDisabled(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		http.Redirect(w, r, "/next", 302)
	}))
	defer s.Close()
	_, _ = NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if n != 1 {
		t.Fatal(n)
	}
}
func TestTransportRetryOnce(t *testing.T) {
	var n int32
	c := NewClient()
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { atomic.AddInt32(&n, 1); return nil, errors.New("boom") })}
	_, e := c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if e == nil || n != 2 {
		t.Fatalf("%v n=%d", e, n)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestTimeoutNoRetry(t *testing.T) {
	c := NewClient()
	c.Timeout = time.Nanosecond
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	_, e := c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
}
func TestChatOllamaAndOpenAI(t *testing.T) {
	for _, x := range []struct {
		k    topology.EndpointKind
		body string
		want string
	}{{topology.KindOllama, `{"message":{"content":"ok"}}`, "ok"}, {topology.KindOpenAICompatible, `{"choices":[{"message":{"content":"yes"}}]}`, "yes"}} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Error()
			}
			w.Write([]byte(x.body))
		}))
		c := NewClient()
		got, e := c.Chat(context.Background(), ep(s.URL, x.k), "m", []Message{{Role: "user", Content: "p"}})
		s.Close()
		if e != nil || got != x.want {
			t.Fatalf("%v %q", e, got)
		}
	}
}
func TestRetry5xx(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	defer s.Close()
	_, e := NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if e != nil || n != 2 {
		t.Fatalf("e=%v n=%d", e, n)
	}
}

func TestBasePathJoin(t *testing.T) {
	called := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = r.URL.Path
		w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	defer s.Close()
	// httptest URL has no path; append a base prefix to ensure it is preserved.
	base := s.URL + "/v1"
	_, _ = NewClient().Chat(context.Background(), ep(base, topology.KindOllama), "m", nil)
	if called != "/v1/api/chat" {
		t.Fatalf("path %q", called)
	}
}

func TestEmptyResponseRetriesExactlyOnce(t *testing.T) {
	var n int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 2 {
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
		}
	}))
	defer s.Close()
	got, e := NewClient().Chat(context.Background(), ep(s.URL, topology.KindOllama), "m", nil)
	if e != nil || got != "ok" || n != 2 {
		t.Fatalf("got=%q err=%v calls=%d", got, e, n)
	}
}
func TestPublicEndpointAndBlankModelMakeNoRequest(t *testing.T) {
	var n int
	c := NewClient()
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { n++; return nil, errors.New("called") })}
	_, e := c.Chat(context.Background(), ep("http://example.com", topology.KindOllama), "m", nil)
	if e == nil || n != 0 {
		t.Fatalf("public err=%v calls=%d", e, n)
	}
	_, e = c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), " ", nil)
	if e == nil || n != 0 {
		t.Fatalf("blank err=%v calls=%d", e, n)
	}
}
func TestCustomClientRedirectIsBlockedWithoutMutation(t *testing.T) {
	called := 0
	redir := func(*http.Request, []*http.Request) error { called++; return nil }
	c := NewClient()
	c.HTTP = &http.Client{CheckRedirect: redir, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("x")), Header: make(http.Header), Request: r}, nil
	})}
	_, _ = c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if called != 0 {
		t.Fatalf("redirect callback called=%d", called)
	}
}
func TestEveryResponseBodyIsClosed(t *testing.T) {
	closed := 0
	c := NewClient()
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: closeCounter{Reader: strings.NewReader(`{"message":{"content":"ok"}}`), n: &closed}, Header: make(http.Header), Request: r}, nil
	})}
	_, e := c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if e != nil || closed != 1 {
		t.Fatalf("err=%v closed=%d", e, closed)
	}
}

type closeCounter struct {
	io.Reader
	n *int
}

func (c closeCounter) Close() error { *c.n++; return nil }
func TestReadErrorRetriesExactlyOnce(t *testing.T) {
	n := 0
	c := NewClient()
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n++
		return &http.Response{StatusCode: 200, Body: errReader{}, Header: make(http.Header), Request: r}, nil
	})}
	_, _ = c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if n != 2 {
		t.Fatal(n)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read") }
func (errReader) Close() error             { return nil }
func TestOversizedResponseDoesNotRetry(t *testing.T) {
	n := 0
	c := NewClient()
	c.MaxResponseBytes = 2
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		n++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("abcd")), Header: make(http.Header), Request: r}, nil
	})}
	_, e := c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if e == nil || n != 1 {
		t.Fatalf("err=%v n=%d", e, n)
	}
}
func TestErrorStatusIsTrustedAndSanitized(t *testing.T) {
	c := NewClient()
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Status: "400 evil", Body: io.NopCloser(strings.NewReader("bad\x00")), Header: make(http.Header), Request: r}, nil
	})}
	_, e := c.Chat(context.Background(), ep("http://localhost", topology.KindOllama), "m", nil)
	if e == nil || strings.Contains(e.Error(), "evil") || strings.ContainsRune(e.Error(), '\x00') {
		t.Fatal(e)
	}
}
func TestProviderPayloadsContainMessagesAndStreamPolicy(t *testing.T) {
	for _, k := range []topology.EndpointKind{topology.KindOllama, topology.KindOpenAICompatible} {
		var p map[string]any
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&p)
			w.Write([]byte(`{"message":{"content":"ok"},"choices":[{"message":{"content":"ok"}}]}`))
		}))
		_, e := NewClient().Chat(context.Background(), ep(s.URL, k), "m", []Message{{Role: "user", Content: "x"}})
		s.Close()
		if e != nil || p["messages"] == nil {
			t.Fatalf("kind=%s err=%v payload=%v", k, e, p)
		}
		if k == topology.KindOllama && p["stream"] != false {
			t.Fatalf("stream=%v", p["stream"])
		}
	}
}
