package modelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/callumny/kingdom/internal/topology"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatClient interface {
	Chat(context.Context, topology.Endpoint, string, []Message) (string, error)
}
type Client struct {
	HTTP             *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
	RetryDelay       time.Duration
}

func NewHTTPClient() *Client { return NewClient() }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Timeout: 60 * time.Second, MaxResponseBytes: 2 << 20, RetryDelay: 100 * time.Millisecond}
}
func (c *Client) Chat(ctx context.Context, ep topology.Endpoint, model string, msgs []Message) (string, error) {
	if err := ep.Validate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("model required")
	}
	base := strings.TrimRight(ep.BaseURL, "/")
	path := "/api/chat"
	if ep.Kind == topology.KindOpenAICompatible {
		path = "/chat/completions"
	}
	u, err := url.JoinPath(base, path)
	if err != nil {
		return "", err
	}
	payload := map[string]any{"model": model, "messages": msgs}
	if ep.Kind == topology.KindOllama {
		payload["stream"] = false
	}
	body, _ := json.Marshal(payload)
	attempts := 2
	var last error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		reqctx := ctx
		cancel := func() {}
		if c.Timeout > 0 {
			reqctx, cancel = context.WithTimeout(ctx, c.Timeout)
		}
		req, er := http.NewRequestWithContext(reqctx, http.MethodPost, u, bytes.NewReader(body))
		retryDelay := c.RetryDelay
		if er != nil {
			cancel()
			return "", er
		}
		req.Header.Set("Content-Type", "application/json")
		hc := c.HTTP
		if hc == nil {
			hc = NewClient().HTTP
		} else {
			cp := *hc
			cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			hc = &cp
		}
		resp, er := hc.Do(req)
		if er != nil {
			// Never retry caller cancellation or deadlines (including the
			// client's per-request timeout).
			if ctx.Err() != nil {
				cancel()
				return "", ctx.Err()
			}
			if reqctx.Err() != nil {
				cancel()
				return "", reqctx.Err()
			}
			cancel()
			last = er
			if i+1 < attempts {
				if !waitRetry(ctx, retryDelay) {
					return "", ctx.Err()
				}
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, c.limit()+1))
		resp.Body.Close()
		cancel()
		if int64(len(data)) > c.limit() {
			return "", fmt.Errorf("response exceeds limit")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Report only the numeric status from the transport; Status is
			// caller-controlled on custom RoundTrippers.
			last = fmt.Errorf("http status %d: %s", resp.StatusCode, sanitize(data))
			if resp.StatusCode >= 500 || resp.StatusCode == 429 {
				if h := retryAfter(resp.Header.Get("Retry-After")); h > retryDelay {
					retryDelay = h
				}
				if i+1 < attempts && !waitRetry(ctx, retryDelay) {
					return "", ctx.Err()
				}
				continue
			}
			return "", last
		}
		if readErr != nil {
			last = readErr
			if i+1 < attempts && !waitRetry(ctx, retryDelay) {
				return "", ctx.Err()
			}
			continue
		}
		if len(bytes.TrimSpace(data)) == 0 {
			last = fmt.Errorf("empty response")
			if i+1 < attempts && !waitRetry(ctx, retryDelay) {
				return "", ctx.Err()
			}
			continue
		}
		txt, er := parse(ep.Kind, data)
		if er != nil {
			return "", er
		}
		if strings.TrimSpace(txt) == "" {
			last = fmt.Errorf("empty response")
			if i+1 < attempts && !waitRetry(ctx, retryDelay) {
				return "", ctx.Err()
			}
			continue
		}
		return txt, nil
	}
	return "", last
}
func waitRetry(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil && n >= 0 {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		if x := time.Until(t); x > 0 {
			d = x
		}
	}
	if d > 2*time.Second {
		return 2 * time.Second
	}
	return d
}
func (c *Client) limit() int64 {
	if c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return 2 << 20
}
func sanitize(b []byte) string {
	s := strings.TrimSpace(strings.ToValidUTF8(string(b), "�"))
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return '�'
		}
		return r
	}, s)
	if len(s) <= 512 {
		return s
	}
	// Truncate on a rune boundary so error text is always valid UTF-8.
	var bld strings.Builder
	for _, r := range s {
		n := len(string(r))
		if bld.Len()+n > 512 {
			break
		}
		bld.WriteRune(r)
	}
	return bld.String()
}
func parse(k topology.EndpointKind, b []byte) (string, error) {
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return "", fmt.Errorf("malformed JSON: %w", err)
	}
	if k == topology.KindOllama {
		if x, ok := v["message"].(map[string]any); ok {
			if s, _ := x["content"].(string); s != "" {
				return s, nil
			}
		}
		return "", nil
	}
	if arr, ok := v["choices"].([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			if msg, ok := m["message"].(map[string]any); ok {
				if s, _ := msg["content"].(string); s != "" {
					return s, nil
				}
			}
		}
	}
	return "", nil
}
