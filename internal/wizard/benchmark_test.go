package wizard

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/topology"
)

type benchmarkResponse struct {
	completion modelapi.Completion
	err        error
}

type fakeCompletionClient struct {
	responses []benchmarkResponse
	calls     []string
}

func (f *fakeCompletionClient) Complete(_ context.Context, _ topology.Endpoint, model string, _ []modelapi.Message, maxTokens int) (modelapi.Completion, error) {
	f.calls = append(f.calls, model+":"+string(rune(maxTokens)))
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response.completion, response.err
}

func TestBenchmarkRunsWarmupThenTimedCapabilityCheckSequentially(t *testing.T) {
	client := &fakeCompletionClient{responses: []benchmarkResponse{
		{completion: modelapi.Completion{Content: "ok"}},
		{completion: modelapi.Completion{Content: `{"tool":{"name":"inspect_setup","arguments":{}}}`, CompletionTokens: 20, GenerationDuration: 500 * time.Millisecond}},
		{completion: modelapi.Completion{Content: "ok"}},
		{completion: modelapi.Completion{Content: `{"tool":{"name":"inspect_setup","arguments":{}}}`, CompletionTokens: 30, GenerationDuration: time.Second}},
	}}
	runner := Benchmarker{Client: client, TimeoutPerModel: time.Second, WarmupTokens: 1, SampleTokens: 24}
	models := benchmarkModels()
	var progress []string
	results := runner.Run(context.Background(), models, func(update BenchmarkProgress) { progress = append(progress, update.Model) })
	if len(results) != 2 || results[0].TokensPerSecond != 40 || results[1].TokensPerSecond != 30 || !results[0].Reliable || !results[1].Reliable {
		t.Fatalf("results=%+v", results)
	}
	if !reflect.DeepEqual(progress, []string{"fast", "fast", "slow", "slow"}) {
		t.Fatalf("progress=%v", progress)
	}
	if winner, ok := FastestReliable(results); !ok || winner.Model.Ref.ModelID != "fast" {
		t.Fatalf("winner=%+v ok=%v", winner, ok)
	}
}

func TestBenchmarkSkipsFailedOrUnreliableModels(t *testing.T) {
	client := &fakeCompletionClient{responses: []benchmarkResponse{
		{err: errors.New("offline")},
		{completion: modelapi.Completion{Content: "ok"}},
		{completion: modelapi.Completion{Content: "not a tool action", CompletionTokens: 100, GenerationDuration: time.Second}},
	}}
	results := (Benchmarker{Client: client, TimeoutPerModel: time.Second}).Run(context.Background(), benchmarkModels(), nil)
	if len(results) != 2 || results[0].Error == "" || results[1].Reliable {
		t.Fatalf("results=%+v", results)
	}
	if _, ok := FastestReliable(results); ok {
		t.Fatal("unreliable model selected")
	}
}

func TestCapabilityActionRejectsExtraTextAndArguments(t *testing.T) {
	for _, value := range []string{
		`{"tool":{"name":"inspect_setup","arguments":{}}} trailing`,
		`{"tool":{"name":"inspect_setup","arguments":{"extra":true}}}`,
		"```json\n{\"tool\":{\"name\":\"inspect_setup\",\"arguments\":{}}}\n```",
	} {
		if validCapabilityAction(value) {
			t.Fatalf("accepted %q", value)
		}
	}
}

func benchmarkModels() []setup.ModelOption {
	endpoint := topology.Endpoint{ID: "e", Name: "Ollama", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:11434"}
	return []setup.ModelOption{
		{Ref: setup.ModelRef{EndpointID: "e", ModelID: "fast"}, Endpoint: endpoint, Installed: true},
		{Ref: setup.ModelRef{EndpointID: "e", ModelID: "slow"}, Endpoint: endpoint, Installed: true},
	}
}
