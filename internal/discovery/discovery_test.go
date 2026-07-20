package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/callumny/kingdom/internal/topology"
)

func TestDefaultsOptionsAndEndpoints(t *testing.T) {
	wantOptions := Options{
		Timeout:          3 * time.Second,
		MaxConcurrency:   4,
		MaxResponseBytes: 2 << 20,
		Client:           http.DefaultClient,
	}
	if got := DefaultOptions(); !reflect.DeepEqual(got, wantOptions) {
		t.Fatalf("DefaultOptions() = %#v, want %#v", got, wantOptions)
	}

	d := New(Options{Timeout: -time.Second, MaxConcurrency: -1, MaxResponseBytes: -1})
	if !reflect.DeepEqual(d.o, wantOptions) {
		t.Fatalf("negative option fallback = %#v, want %#v", d.o, wantOptions)
	}

	wantEndpoints := []topology.Endpoint{
		{ID: "ollama-local", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://localhost:11434"},
		{ID: "mlx-local", Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://localhost:8080/v1"},
	}
	first := DefaultEndpoints()
	if !reflect.DeepEqual(first, wantEndpoints) {
		t.Fatalf("DefaultEndpoints() = %#v, want %#v", first, wantEndpoints)
	}
	for _, endpoint := range first {
		if err := endpoint.Validate(); err != nil {
			t.Fatalf("default endpoint %q is invalid: %v", endpoint.ID, err)
		}
	}
	first[0].ID = "mutated"
	if got := DefaultEndpoints(); !reflect.DeepEqual(got, wantEndpoints) {
		t.Fatalf("DefaultEndpoints shared mutable state: %#v", got)
	}
}

func TestDiscoverNoEndpoints(t *testing.T) {
	results, err := New(Options{}).Discover(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if results == nil || len(results) != 0 {
		t.Fatalf("results = %#v, want non-nil empty slice", results)
	}
}

func TestProviderPathsMethodsAndNormalization(t *testing.T) {
	type request struct{ method, path string }
	requests := make(chan request, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- request{method: r.Method, path: r.URL.Path}
		w.Header().Set("Content-Type", "text/plain")
		switch r.URL.Path {
		case "/api/tags":
			_, _ = io.WriteString(w, `{"models":[
				{"model":" beta ","size":12,"details":{"family":"llama","parameter_size":"7B","quantization_level":"Q4"}},
				{"name":"Alpha","model":"","size":-1},
				{"model":"beta","size":99,"details":{"family":"wrong"}},
				{"model":"  "},
				{"model":"alpha"}
			]}`)
		case "/prefix/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":" z ","extra":true},{"id":"A"},{"id":"a"},{"id":""},{"id":"z"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	endpoints := []topology.Endpoint{
		{ID: "ollama", Name: "Ollama", Kind: topology.KindOllama, BaseURL: server.URL},
		{ID: "openai", Name: "OpenAI", Kind: topology.KindOpenAICompatible, BaseURL: server.URL + "/prefix/v1"},
	}
	results, err := New(Options{}).Discover(context.Background(), endpoints)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := <-requests
		if got.method != http.MethodGet {
			t.Errorf("method = %q, want GET", got.method)
		}
		if got.path != "/api/tags" && got.path != "/prefix/v1/models" {
			t.Errorf("unexpected request path %q", got.path)
		}
	}

	wantOllama := []Model{
		{ID: "Alpha", EndpointID: "ollama"},
		{ID: "alpha", EndpointID: "ollama"},
		{ID: "beta", EndpointID: "ollama", SizeBytes: 12, Family: "llama", ParameterSize: "7B", Quantization: "Q4"},
	}
	if !reflect.DeepEqual(results[0].Models, wantOllama) || results[0].Err != nil {
		t.Fatalf("Ollama result = %#v, err %v", results[0].Models, results[0].Err)
	}
	wantOpenAI := []Model{{ID: "A", EndpointID: "openai"}, {ID: "a", EndpointID: "openai"}, {ID: "z", EndpointID: "openai"}}
	if !reflect.DeepEqual(results[1].Models, wantOpenAI) || results[1].Err != nil {
		t.Fatalf("OpenAI result = %#v, err %v", results[1].Models, results[1].Err)
	}
}

func TestAny2xxEmptyListsAndSameIDsAcrossEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		switch r.URL.Path {
		case "/api/tags":
			_, _ = io.WriteString(w, `{"models":[{"model":"shared"}]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"shared"}]}`)
		case "/empty/v1/models":
			_, _ = io.WriteString(w, `{"data":[]}`)
		}
	}))
	defer server.Close()

	endpoints := []topology.Endpoint{
		{ID: "one", Name: "One", Kind: topology.KindOllama, BaseURL: server.URL},
		{ID: "two", Name: "Two", Kind: topology.KindOpenAICompatible, BaseURL: server.URL + "/v1"},
		{ID: "empty", Name: "Empty", Kind: topology.KindOpenAICompatible, BaseURL: server.URL + "/empty/v1"},
	}
	results, err := New(Options{}).Discover(context.Background(), endpoints)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Models[0].ID != "shared" || results[1].Models[0].ID != "shared" {
		t.Fatalf("same model ID was not retained across endpoints: %#v", results)
	}
	if results[0].Models[0].EndpointID == results[1].Models[0].EndpointID {
		t.Fatal("same model ID lost endpoint identity")
	}
	if results[2].Err != nil || len(results[2].Models) != 0 {
		t.Fatalf("empty list result = %#v", results[2])
	}
}

func TestInvalidEndpointAndPartialFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasPrefix(r.URL.Path, "/bad/") {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"models":[{"model":"ok"}]}`)
	}))
	defer server.Close()

	endpoints := []topology.Endpoint{
		{ID: "invalid", Name: "Invalid", Kind: "unknown", BaseURL: server.URL},
		{ID: "bad", Name: "Bad", Kind: topology.KindOllama, BaseURL: server.URL + "/bad"},
		{ID: "good", Name: "Good", Kind: topology.KindOllama, BaseURL: server.URL + "/good"},
	}
	results, err := New(Options{}).Discover(context.Background(), endpoints)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, invalid endpoint should not be requested", requests.Load())
	}
	if results[0].Endpoint.ID != "invalid" || results[0].Err == nil || results[1].Err == nil {
		t.Fatalf("expected ordered endpoint failures: %#v", results)
	}
	if results[2].Err != nil || len(results[2].Models) != 1 || results[2].Models[0].ID != "ok" {
		t.Fatalf("partial success lost: %#v", results[2])
	}
}

func TestNon2xxErrorIsSanitizedAndBounded(t *testing.T) {
	body := append([]byte("line1\n\t\x00 "), []byte(strings.Repeat("é", 200))...)
	body = append(body, 0xff)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	endpoint := topology.Endpoint{ID: "broken", Name: "Broken", Kind: topology.KindOllama, BaseURL: server.URL}
	results, err := New(Options{MaxResponseBytes: 1024}).Discover(context.Background(), []topology.Endpoint{endpoint})
	if err != nil || results[0].Err == nil {
		t.Fatalf("Discover error = %v, result = %#v", err, results[0])
	}
	message := results[0].Err.Error()
	if !strings.Contains(message, "500") || strings.ContainsAny(message, "\n\r\t\x00") || !utf8.ValidString(message) {
		t.Fatalf("unsafe status error %q", message)
	}
	marker := strings.LastIndex(message, ": ")
	if marker < 0 || len([]byte(message[marker+2:])) > 256 {
		t.Fatalf("status snippet is not bounded: %q", message)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	endpoint := topology.Endpoint{ID: "redirect", Name: "Redirect", Kind: topology.KindOllama, BaseURL: source.URL}
	results, err := New(Options{}).Discover(context.Background(), []topology.Endpoint{endpoint})
	if err != nil || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "302") {
		t.Fatalf("Discover = (%#v, %v), want endpoint redirect error", results, err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", targetRequests.Load())
	}
}

func TestStatusLineUsesTrustedText(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("failure")}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 attacker\n\tstatus",
			Body:       body,
			Header:     make(http.Header),
		}, nil
	})}
	endpoint := topology.Endpoint{ID: "status", Name: "Status", Kind: topology.KindOllama, BaseURL: "http://localhost"}
	results, err := New(Options{Client: client}).Discover(context.Background(), []topology.Endpoint{endpoint})
	if err != nil || results[0].Err == nil {
		t.Fatalf("Discover = (%#v, %v)", results, err)
	}
	message := results[0].Err.Error()
	if strings.ContainsAny(message, "\n\r\t") || strings.Contains(message, "attacker") || !strings.Contains(message, "500 Internal Server Error") {
		t.Fatalf("untrusted status error %q", message)
	}
}

func TestMalformedEmptyAndOversizedResponses(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		limit int64
		want  string
	}{
		{name: "malformed", body: `{"models":`, limit: 1024, want: "decode Ollama response"},
		{name: "empty", body: "", limit: 1024, want: "decode Ollama response"},
		{name: "oversized", body: strings.Repeat("x", 65), limit: 64, want: "response exceeds 64 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			endpoint := topology.Endpoint{ID: tt.name, Name: tt.name, Kind: topology.KindOllama, BaseURL: server.URL}
			results, err := New(Options{MaxResponseBytes: tt.limit}).Discover(context.Background(), []topology.Endpoint{endpoint})
			if err != nil || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), tt.want) {
				t.Fatalf("err = %v, result error = %v, want %q", err, results[0].Err, tt.want)
			}
		})
	}
}

func TestEndpointTimeoutIsLocalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	endpoint := topology.Endpoint{ID: "slow", Name: "Slow", Kind: topology.KindOllama, BaseURL: server.URL}
	results, err := New(Options{Timeout: 40 * time.Millisecond}).Discover(context.Background(), []topology.Endpoint{endpoint})
	if err != nil {
		t.Fatalf("endpoint timeout became top-level error: %v", err)
	}
	if results[0].Err == nil || !errors.Is(results[0].Err, context.DeadlineExceeded) {
		t.Fatalf("endpoint timeout error = %v", results[0].Err)
	}
}

func TestParentCancellationStopsInflightAndWaitingWork(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer server.Close()

	endpoints := make([]topology.Endpoint, 3)
	for i := range endpoints {
		endpoints[i] = topology.Endpoint{ID: fmt.Sprintf("endpoint-%d", i), Name: "Endpoint", Kind: topology.KindOllama, BaseURL: server.URL}
	}
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		results []Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := New(Options{MaxConcurrency: 1}).Discover(ctx, endpoints)
		done <- outcome{results: results, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("top-level error = %v", got.err)
		}
		for i, result := range got.results {
			if result.Endpoint.ID != endpoints[i].ID || result.Err == nil || !errors.Is(result.Err, context.Canceled) {
				t.Errorf("result[%d] = %#v", i, result)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not stop after parent cancellation")
	}
}

func TestConcurrencyLimitUsesWorkerPool(t *testing.T) {
	const limit = 2
	var active, maximum atomic.Int32
	entered := make(chan struct{}, 6)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer server.Close()

	endpoints := make([]topology.Endpoint, 6)
	for i := range endpoints {
		endpoints[i] = topology.Endpoint{ID: fmt.Sprintf("endpoint-%d", i), Name: "Endpoint", Kind: topology.KindOllama, BaseURL: server.URL}
	}
	type outcome struct {
		results []Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := New(Options{MaxConcurrency: limit, Timeout: time.Second}).Discover(context.Background(), endpoints)
		done <- outcome{results: results, err: err}
	}()
	for i := 0; i < limit; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("expected workers did not enter handlers")
		}
	}
	close(release)
	select {
	case got := <-done:
		if got.err != nil || len(got.results) != len(endpoints) {
			t.Fatalf("Discover = (%d results, %v)", len(got.results), got.err)
		}
		for _, result := range got.results {
			if result.Err != nil {
				t.Errorf("endpoint result error: %v", result.Err)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("discovery did not finish after release")
	}
	if maximum.Load() != limit {
		t.Fatalf("maximum concurrency = %d, want %d", maximum.Load(), limit)
	}
}

func TestResultsRemainInInputOrder(t *testing.T) {
	names := []string{"one", "two", "three"}
	gates := map[string]chan struct{}{}
	for _, name := range names {
		gates[name] = make(chan struct{})
	}
	entered := make(chan string, len(names))
	finished := make(chan string, len(names))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		entered <- name
		<-gates[name]
		_, _ = fmt.Fprintf(w, `{"data":[{"id":%q}]}`, name)
		finished <- name
	}))
	defer server.Close()

	endpoints := make([]topology.Endpoint, len(names))
	for i, name := range names {
		endpoints[i] = topology.Endpoint{ID: name, Name: name, Kind: topology.KindOpenAICompatible, BaseURL: server.URL + "/" + name + "/v1"}
	}
	type outcome struct {
		results []Result
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := New(Options{MaxConcurrency: 3}).Discover(context.Background(), endpoints)
		done <- outcome{results: results, err: err}
	}()
	seen := map[string]bool{}
	for range names {
		select {
		case name := <-entered:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("requests did not all start")
		}
	}
	for _, name := range []string{"three", "two", "one"} {
		if !seen[name] {
			t.Fatalf("request %q did not start", name)
		}
		close(gates[name])
		select {
		case got := <-finished:
			if got != name {
				t.Fatalf("completion = %q, want %q", got, name)
			}
		case <-time.After(time.Second):
			t.Fatalf("request %q did not finish", name)
		}
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	for i, result := range got.results {
		if result.Endpoint.ID != names[i] || result.Err != nil || result.Models[0].ID != names[i] {
			t.Errorf("result[%d] = %#v", i, result)
		}
	}
}

func TestResponseBodiesAreAlwaysClosed(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		limit  int64
	}{
		{name: "success", status: 200, body: `{"models":[]}`, limit: 1024},
		{name: "status", status: 500, body: "bad", limit: 1024},
		{name: "malformed", status: 200, body: "{", limit: 1024},
		{name: "oversized", status: 200, body: strings.Repeat("x", 20), limit: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &trackingBody{Reader: strings.NewReader(tt.body)}
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tt.status, Status: fmt.Sprintf("%d status", tt.status), Body: body, Header: make(http.Header)}, nil
			})}
			endpoint := topology.Endpoint{ID: tt.name, Name: tt.name, Kind: topology.KindOllama, BaseURL: "http://localhost"}
			_, _ = New(Options{Client: client, MaxResponseBytes: tt.limit}).Discover(context.Background(), []topology.Endpoint{endpoint})
			if !body.closed.Load() {
				t.Fatal("response body was not closed")
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type trackingBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)
	return nil
}

func TestDiscoveryIsRaceSafeAcrossRepeatedRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer server.Close()
	endpoints := []topology.Endpoint{{ID: "one", Name: "One", Kind: topology.KindOllama, BaseURL: server.URL}}

	var group sync.WaitGroup
	for range 10 {
		group.Add(1)
		go func() {
			defer group.Done()
			results, err := New(Options{}).Discover(context.Background(), endpoints)
			if err != nil || len(results) != 1 || results[0].Err != nil {
				t.Errorf("Discover = (%#v, %v)", results, err)
			}
		}()
	}
	group.Wait()
}
