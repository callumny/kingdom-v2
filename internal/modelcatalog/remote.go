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
	"sync"
)

const maxRemoteResponseBytes int64 = 2 << 20

var ollamaLibraryLink = regexp.MustCompile(`href=["']/library/([^"'/?#]+)`)
var ollamaLibraryCard = regexp.MustCompile(`(?is)<a[^>]+href=["']/library/([^"'/?#]+)[^>]*>(.*?)</a>`)
var htmlElement = regexp.MustCompile(`(?s)<[^>]*>`)
var parameterSize = regexp.MustCompile(`(?i)(?:^|[^[:alnum:].])(\d+(?:\.\d+)?(?:m|b)|\d+x\d+b)(?:$|[^[:alnum:]])`)
var pullCount = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?[kmb]?)\s+pulls`)
var storageSize = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(gb|mb)`)

type Remote struct {
	client           *http.Client
	ollamaURL        string
	ollamaPopularURL string
	huggingURL       string
}

func NewRemote(client *http.Client, ollamaURL, huggingFaceURL string) *Remote {
	if client == nil {
		client = http.DefaultClient
	}
	return &Remote{client: client, ollamaURL: ollamaURL, ollamaPopularURL: ollamaURL, huggingURL: huggingFaceURL}
}

func DefaultRemote(client *http.Client) *Remote {
	remote := NewRemote(client, "https://ollama.com/search", "https://huggingface.co/api/models")
	remote.ollamaPopularURL = "https://ollama.com/library"
	return remote
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

// Popular returns provider-ranked downloadable chat models. Ranks are
// meaningful only within one provider because their download counters use
// different sources and time windows.
func (r *Remote) Popular(ctx context.Context, provider Provider, limit int) ([]Model, error) {
	if limit < 1 {
		limit = 3
	}
	switch provider {
	case Ollama:
		return r.popularOllama(ctx, limit)
	case MLX:
		return r.popularMLX(ctx, limit)
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

func (r *Remote) popularOllama(ctx context.Context, limit int) ([]Model, error) {
	body, err := r.get(ctx, r.ollamaPopularURL, nil)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	models := make([]Model, 0, limit)
	for _, match := range ollamaLibraryCard.FindAllSubmatch(body, -1) {
		id, decodeErr := url.PathUnescape(string(match[1]))
		id = strings.TrimSpace(id)
		content := strings.TrimSpace(htmlElement.ReplaceAllString(string(match[2]), " "))
		lowerContent := strings.ToLower(content)
		if decodeErr != nil || id == "" || seen[id] || strings.Contains(strings.ToLower(id), "embed") || strings.Contains(lowerContent, "embedding") {
			continue
		}
		seen[id] = true
		model := Model{Provider: Ollama, ID: id, PopularityRank: len(models) + 1}
		if pulls := pullCount.FindStringSubmatch(content); len(pulls) == 2 {
			model.Downloads = parseHumanCount(pulls[1])
		}
		models = append(models, model)
		if len(models) == limit {
			break
		}
	}
	r.enrichOllamaDefaults(ctx, models)
	return models, nil
}

func (r *Remote) enrichOllamaDefaults(ctx context.Context, models []Model) {
	type enrichment struct {
		index         int
		parameterSize string
		sizeBytes     int64
	}
	results := make(chan enrichment, len(models))
	var wait sync.WaitGroup
	for index := range models {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			parameter, size, err := r.ollamaDefaultMetadata(ctx, models[index].ID)
			if err == nil {
				results <- enrichment{index: index, parameterSize: parameter, sizeBytes: size}
			}
		}(index)
	}
	wait.Wait()
	close(results)
	for result := range results {
		models[result.index].ParameterSize = result.parameterSize
		models[result.index].SizeBytes = result.sizeBytes
	}
}

func (r *Remote) ollamaDefaultMetadata(ctx context.Context, modelID string) (string, int64, error) {
	base, err := url.Parse(r.ollamaPopularURL)
	if err != nil {
		return "", 0, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + url.PathEscape(modelID)
	body, err := r.get(ctx, base.String(), nil)
	if err != nil {
		return "", 0, err
	}
	var aliasSize int64
	for _, match := range ollamaLibraryCard.FindAllSubmatch(body, -1) {
		tagID, decodeErr := url.PathUnescape(string(match[1]))
		if decodeErr != nil || !strings.HasPrefix(tagID, modelID+":") {
			continue
		}
		content := strings.TrimSpace(htmlElement.ReplaceAllString(string(match[2]), " "))
		suffix := strings.TrimPrefix(tagID, modelID+":")
		size := parseStorageBytes(content)
		if strings.EqualFold(suffix, "latest") {
			aliasSize = size
			continue
		}
		if !strings.Contains(strings.ToLower(content), "latest") {
			continue
		}
		parameter := ""
		if parsed := parameterSize.FindStringSubmatch(suffix); len(parsed) == 2 {
			parameter = strings.ToUpper(parsed[1])
		}
		if size == 0 {
			size = aliasSize
		}
		return parameter, size, nil
	}
	return "", aliasSize, nil
}

func parseStorageBytes(value string) int64 {
	match := storageSize.FindStringSubmatch(value)
	if len(match) != 3 {
		return 0
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil || number < 0 {
		return 0
	}
	multiplier := float64(1_000_000_000)
	if strings.EqualFold(match[2], "mb") {
		multiplier = 1_000_000
	}
	return int64(number * multiplier)
}

func (r *Remote) popularMLX(ctx context.Context, limit int) ([]Model, error) {
	requestLimit := limit * 4
	if requestLimit < limit {
		requestLimit = limit
	}
	values := url.Values{
		"filter":    {"mlx", "text-generation"},
		"sort":      {"downloads"},
		"direction": {"-1"},
		"limit":     {strconv.Itoa(requestLimit)},
	}
	body, err := r.get(ctx, r.huggingURL, values)
	if err != nil {
		return nil, err
	}
	var response []struct {
		ModelID   string          `json:"modelId"`
		ID        string          `json:"id"`
		Downloads int64           `json:"downloads"`
		Pipeline  string          `json:"pipeline_tag"`
		Gated     json.RawMessage `json:"gated"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode MLX popular models: %w", err)
	}
	seen := make(map[string]bool)
	models := make([]Model, 0, limit)
	for _, item := range response {
		id := strings.TrimSpace(item.ModelID)
		if id == "" {
			id = strings.TrimSpace(item.ID)
		}
		pipeline := strings.ToLower(strings.TrimSpace(item.Pipeline))
		if id == "" || seen[id] || gatedModel(item.Gated) || (pipeline != "" && pipeline != "text-generation") {
			continue
		}
		seen[id] = true
		models = append(models, Model{
			Provider:       MLX,
			ID:             id,
			PopularityRank: len(models) + 1,
			Downloads:      item.Downloads,
		})
		if len(models) == limit {
			break
		}
	}
	return models, nil
}

func gatedModel(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != "false" && value != `""`
}

func parseHumanCount(value string) int64 {
	value = strings.ToLower(strings.TrimSpace(value))
	multiplier := float64(1)
	if len(value) > 0 {
		switch value[len(value)-1] {
		case 'k':
			multiplier = 1_000
			value = value[:len(value)-1]
		case 'm':
			multiplier = 1_000_000
			value = value[:len(value)-1]
		case 'b':
			multiplier = 1_000_000_000
			value = value[:len(value)-1]
		}
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || number < 0 {
		return 0
	}
	return int64(number * multiplier)
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
