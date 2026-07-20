package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxRemoteResponseBytes int64 = 2 << 20

var ollamaLibraryLink = regexp.MustCompile(`href=["']/library/([^"'/?#]+)`)

type Remote struct {
	client     *http.Client
	ollamaURL  string
	huggingURL string
}

func NewRemote(client *http.Client, ollamaURL, huggingFaceURL string) *Remote {
	if client == nil {
		client = http.DefaultClient
	}
	return &Remote{client: client, ollamaURL: ollamaURL, huggingURL: huggingFaceURL}
}

func DefaultRemote(client *http.Client) *Remote {
	return NewRemote(client, "https://ollama.com/search", "https://huggingface.co/api/models")
}

func (r *Remote) Search(ctx context.Context, provider Provider, query string, limit int) ([]Model, error) {
	if limit < 1 {
		limit = 10
	}
	switch provider {
	case Ollama:
		return r.searchOllama(ctx, query, limit)
	case MLX:
		return r.searchMLX(ctx, query, limit)
	default:
		return nil, fmt.Errorf("unknown model provider %q", provider)
	}
}

func (r *Remote) get(ctx context.Context, rawURL string, query url.Values) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse model search URL: %w", err)
	}
	parsed.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create model search request: %w", err)
	}
	request.Header.Set("User-Agent", "kingdom-cli/2")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("search models: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRemoteResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read model search: %w", err)
	}
	if int64(len(body)) > maxRemoteResponseBytes {
		return nil, fmt.Errorf("model search response exceeds %d bytes", maxRemoteResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model search returned HTTP %d", response.StatusCode)
	}
	return body, nil
}

func (r *Remote) searchOllama(ctx context.Context, query string, limit int) ([]Model, error) {
	body, err := r.get(ctx, r.ollamaURL, url.Values{"q": {strings.TrimSpace(query)}})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var models []Model
	for _, match := range ollamaLibraryLink.FindAllSubmatch(body, -1) {
		id, decodeErr := url.PathUnescape(string(match[1]))
		id = strings.TrimSpace(id)
		if decodeErr != nil || id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, Model{Provider: Ollama, ID: id})
	}
	return sortedLimited(models, limit), nil
}

func (r *Remote) searchMLX(ctx context.Context, query string, limit int) ([]Model, error) {
	values := url.Values{"search": {strings.TrimSpace(query)}, "filter": {"mlx"}, "sort": {"downloads"}, "direction": {"-1"}, "limit": {strconv.Itoa(limit)}}
	body, err := r.get(ctx, r.huggingURL, values)
	if err != nil {
		return nil, err
	}
	var response []struct {
		ModelID string `json:"modelId"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode MLX model search: %w", err)
	}
	models := make([]Model, 0, len(response))
	for _, item := range response {
		id := strings.TrimSpace(item.ModelID)
		if id == "" {
			id = strings.TrimSpace(item.ID)
		}
		models = append(models, Model{Provider: MLX, ID: id})
	}
	return sortedLimited(models, limit), nil
}

func sortedLimited(models []Model, limit int) []Model {
	byIdentity := make(map[Identity]Model, len(models))
	for _, model := range models {
		if model.ID != "" {
			byIdentity[model.Identity()] = model
		}
	}
	models = models[:0]
	for _, model := range byIdentity {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID) })
	if len(models) > limit {
		models = models[:limit]
	}
	return models
}
