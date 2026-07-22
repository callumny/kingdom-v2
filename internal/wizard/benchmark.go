package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type CompletionClient interface {
	Complete(context.Context, topology.Endpoint, string, []modelapi.Message, int) (modelapi.Completion, error)
}

type BenchmarkPhase string

const (
	BenchmarkWarming BenchmarkPhase = "warming"
	BenchmarkTesting BenchmarkPhase = "testing"
)

type BenchmarkProgress struct {
	Index int
	Total int
	Model string
	Phase BenchmarkPhase
}

type BenchmarkResult struct {
	Model           setup.ModelOption
	TokensPerSecond float64
	Reliable        bool
	Error           string
}

type Benchmarker struct {
	Client          CompletionClient
	TimeoutPerModel time.Duration
	WarmupTokens    int
	SampleTokens    int
}

func (b Benchmarker) Run(ctx context.Context, models []setup.ModelOption, report func(BenchmarkProgress)) []BenchmarkResult {
	results := make([]BenchmarkResult, 0, len(models))
	for index, model := range models {
		result := BenchmarkResult{Model: model}
		if err := ctx.Err(); err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		modelContext, cancel := context.WithTimeout(ctx, b.timeout())
		if report != nil {
			report(BenchmarkProgress{Index: index, Total: len(models), Model: model.Ref.ModelID, Phase: BenchmarkWarming})
		}
		_, err := b.Client.Complete(modelContext, model.Endpoint, model.Ref.ModelID, []modelapi.Message{{Role: "user", Content: "Reply with only: ready"}}, b.warmupTokens())
		if err != nil {
			cancel()
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		if report != nil {
			report(BenchmarkProgress{Index: index, Total: len(models), Model: model.Ref.ModelID, Phase: BenchmarkTesting})
		}
		completion, err := b.Client.Complete(modelContext, model.Endpoint, model.Ref.ModelID, []modelapi.Message{{Role: "user", Content: capabilityPrompt}}, b.sampleTokens())
		cancel()
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.Reliable = validCapabilityAction(completion.Content)
		result.TokensPerSecond = tokensPerSecond(completion)
		results = append(results, result)
	}
	return results
}

const capabilityPrompt = `Return only this JSON object, with no markdown: {"tool":{"name":"inspect_setup","arguments":{}}}`

func validCapabilityAction(content string) bool {
	var response struct {
		Tool struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"tool"`
	}
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || response.Tool.Name != "inspect_setup" || len(response.Tool.Arguments) != 0 {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}

func tokensPerSecond(completion modelapi.Completion) float64 {
	duration := completion.GenerationDuration.Seconds()
	if duration <= 0 {
		return 0
	}
	tokens := completion.CompletionTokens
	if tokens <= 0 {
		tokens = max(1, utf8.RuneCountInString(completion.Content)/4)
	}
	return math.Round((float64(tokens)/duration)*10) / 10
}

func FastestReliable(results []BenchmarkResult) (BenchmarkResult, bool) {
	var fastest BenchmarkResult
	found := false
	for _, result := range results {
		if result.Error != "" || !result.Reliable || result.TokensPerSecond <= 0 {
			continue
		}
		if !found || result.TokensPerSecond > fastest.TokensPerSecond {
			fastest = result
			found = true
		}
	}
	return fastest, found
}

func (b Benchmarker) timeout() time.Duration {
	if b.TimeoutPerModel > 0 {
		return b.TimeoutPerModel
	}
	return 30 * time.Second
}

func (b Benchmarker) warmupTokens() int {
	if b.WarmupTokens > 0 {
		return b.WarmupTokens
	}
	return 4
}

func (b Benchmarker) sampleTokens() int {
	if b.SampleTokens > 0 {
		return b.SampleTokens
	}
	return 24
}

func (r BenchmarkResult) Label() string {
	if r.Error != "" {
		return r.Error
	}
	quality := "tool check failed"
	if r.Reliable {
		quality = "reliable"
	}
	return fmt.Sprintf("%.1f tok/s · %s", r.TokensPerSecond, quality)
}
