package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/callumny/kingdom/internal/topology"
)

const (
	// DefaultTimeout bounds each endpoint request.
	DefaultTimeout = 3 * time.Second
	// DefaultMaxConcurrency limits simultaneous endpoint requests.
	DefaultMaxConcurrency = 4
	// DefaultMaxResponseBytes limits response bodies.
	DefaultMaxResponseBytes int64 = 2 << 20
)

// Options configures discovery behavior.
type Options struct {
	Timeout          time.Duration
	MaxConcurrency   int
	MaxResponseBytes int64
	Client           *http.Client
}

// DefaultOptions returns documented safe defaults.
func DefaultOptions() Options {
	return Options{Timeout: DefaultTimeout, MaxConcurrency: DefaultMaxConcurrency, MaxResponseBytes: DefaultMaxResponseBytes, Client: http.DefaultClient}
}

// Model is a normalized discovered model.
type Model struct {
	ID            string
	EndpointID    string
	SizeBytes     int64
	Family        string
	ParameterSize string
	Quantization  string
}

// Result contains endpoint models or an endpoint-specific error.
type Result struct {
	Endpoint topology.Endpoint
	Models   []Model
	Err      error
}

// Discovery performs bounded model discovery.
type Discovery struct {
	o      Options
	client *http.Client
}

// New constructs discovery, replacing non-positive options with defaults.
func New(o Options) *Discovery {
	d := DefaultOptions()
	if o.Timeout > 0 {
		d.Timeout = o.Timeout
	}
	if o.MaxConcurrency > 0 {
		d.MaxConcurrency = o.MaxConcurrency
	}
	if o.MaxResponseBytes > 0 {
		d.MaxResponseBytes = o.MaxResponseBytes
	}
	if o.Client != nil {
		d.Client = o.Client
	}
	client := *d.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Discovery{o: d, client: &client}
}

// DefaultEndpoints returns the standard local providers in stable order.
func DefaultEndpoints() []topology.Endpoint {
	return []topology.Endpoint{
		{ID: "ollama-local", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://localhost:11434"},
		{ID: "mlx-local", Name: "MLX", Kind: topology.KindOpenAICompatible, BaseURL: "http://localhost:8080/v1"},
	}
}

// Discover queries endpoints concurrently while preserving input order.
func (d *Discovery) Discover(ctx context.Context, eps []topology.Endpoint) ([]Result, error) {
	if len(eps) == 0 {
		return []Result{}, nil
	}
	rs := make([]Result, len(eps))
	jobs := make(chan int)
	workers := d.o.MaxConcurrency
	if workers > len(eps) {
		workers = len(eps)
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for n := 0; n < workers; n++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				e := eps[i]
				rs[i].Endpoint = e
				if err := e.Validate(); err != nil {
					rs[i].Err = fmt.Errorf("endpoint %s validation: %w", e.ID, err)
					continue
				}
				cctx, cancel := context.WithTimeout(ctx, d.o.Timeout)
				models, err := d.fetch(cctx, e)
				cancel()
				rs[i].Models, rs[i].Err = models, err
			}
		}()
	}
	for i := range eps {
		select {
		case jobs <- i:
		case <-ctx.Done():
			rs[i].Endpoint = eps[i]
			rs[i].Err = ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return rs, err
	}
	return rs, nil
}

func (d *Discovery) fetch(ctx context.Context, e topology.Endpoint) ([]Model, error) {
	u, err := url.Parse(e.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s parse URL: %w", e.ID, err)
	}
	p := strings.TrimSuffix(u.Path, "/")
	if e.Kind == topology.KindOllama {
		u.Path = p + "/api/tags"
	} else if e.Kind == topology.KindOpenAICompatible {
		u.Path = p + "/models"
	} else {
		return nil, fmt.Errorf("endpoint %s unsupported kind", e.ID)
	}
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s create request: %w", e.ID, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s request: %w", e.ID, err)
	}
	defer resp.Body.Close()
	limit := d.o.MaxResponseBytes
	if limit < math.MaxInt64 {
		limit++
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("endpoint %s read response: %w", e.ID, err)
	}
	if int64(len(b)) > d.o.MaxResponseBytes {
		return nil, fmt.Errorf("endpoint %s response exceeds %d bytes", e.ID, d.o.MaxResponseBytes)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status := fmt.Sprintf("%d", resp.StatusCode)
		if text := http.StatusText(resp.StatusCode); text != "" {
			status += " " + text
		}
		return nil, fmt.Errorf("endpoint %s http status %s: %s", e.ID, status, sanitize(string(b)))
	}
	if e.Kind == topology.KindOllama {
		return parseOllama(b, e.ID)
	}
	return parseOpenAI(b, e.ID)
}

func sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == 127 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	s = strings.Join(strings.Fields(b.String()), " ")
	if len([]byte(s)) <= 256 {
		return s
	}
	x := []byte(s)[:256]
	for !utf8.Valid(x) {
		x = x[:len(x)-1]
	}
	return string(x)
}

func parseOllama(b []byte, endpoint string) ([]Model, error) {
	var v struct {
		Models []struct {
			Name    string `json:"name"`
			Model   string `json:"model"`
			Size    int64  `json:"size"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
				Quantization  string `json:"quantization_level"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("endpoint %s decode Ollama response: %w", endpoint, err)
	}
	out := make([]Model, 0, len(v.Models))
	seen := map[string]bool{}
	for _, m := range v.Models {
		id := strings.TrimSpace(m.Model)
		if id == "" {
			id = strings.TrimSpace(m.Name)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		sz := m.Size
		if sz < 0 {
			sz = 0
		}
		out = append(out, Model{ID: id, EndpointID: endpoint, SizeBytes: sz, Family: m.Details.Family, ParameterSize: m.Details.ParameterSize, Quantization: m.Details.Quantization})
	}
	sortModels(out)
	return out, nil
}
func parseOpenAI(b []byte, endpoint string) ([]Model, error) {
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("endpoint %s decode OpenAI response: %w", endpoint, err)
	}
	out := make([]Model, 0, len(v.Data))
	seen := map[string]bool{}
	for _, m := range v.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Model{ID: id, EndpointID: endpoint})
	}
	sortModels(out)
	return out, nil
}
func sortModels(m []Model) {
	sort.Slice(m, func(i, j int) bool {
		a, b := strings.ToLower(m[i].ID), strings.ToLower(m[j].ID)
		if a == b {
			return m[i].ID < m[j].ID
		}
		return a < b
	})
}
