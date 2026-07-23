package modelcatalog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteSearchNormalizesExactNamesAcrossProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ollama":
			if request.URL.Query().Get("q") != "qwen" {
				t.Errorf("Ollama query=%q", request.URL.RawQuery)
			}
			_, _ = io.WriteString(writer, `<a href="/library/qwen3">one</a><a href="/library/qwen3">duplicate</a><a href="/library/deepseek-r1">two</a>`)
		case "/huggingface":
			if request.URL.Query().Get("search") != "qwen" || request.URL.Query().Get("filter") != "mlx" {
				t.Errorf("Hugging Face query=%q", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `[{"modelId":"mlx-community/Qwen3-8B"},{"id":"mlx-community/Qwen3-4B"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	remote := NewRemote(server.Client(), server.URL+"/ollama", server.URL+"/huggingface")
	ollama, err := remote.Search(context.Background(), Ollama, "qwen", 10)
	if err != nil || len(ollama) != 2 || ollama[0].ID != "deepseek-r1" || ollama[1].ID != "qwen3" {
		t.Fatalf("Ollama=%+v err=%v", ollama, err)
	}
	mlx, err := remote.Search(context.Background(), MLX, "qwen", 10)
	if err != nil || len(mlx) != 2 || mlx[0].ID != "mlx-community/Qwen3-4B" || mlx[1].ID != "mlx-community/Qwen3-8B" {
		t.Fatalf("MLX=%+v err=%v", mlx, err)
	}
}

func TestRemotePopularPreservesProviderRankingAndFiltersUnsupportedModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ollama":
			_, _ = io.WriteString(writer, `
				<a href="/library/nomic-embed-text">embedding 79.4M Pulls</a>
				<a href="/library/llama3.2">tools 1b 3b 77.5M Pulls</a>
				<a href="/library/deepseek-r1">thinking 1.5b 7b 90.3M Pulls</a>
				<a href="/library/llama3.2">duplicate</a>`)
		case "/ollama/llama3.2":
			_, _ = io.WriteString(writer, `<a href="/library/llama3.2:latest">2.0GB</a><a href="/library/llama3.2:3b">latest 2.0GB</a>`)
		case "/ollama/deepseek-r1":
			_, _ = io.WriteString(writer, `<a href="/library/deepseek-r1:latest">4.7GB</a><a href="/library/deepseek-r1:7b">latest 4.7GB</a>`)
		case "/huggingface":
			filters := request.URL.Query()["filter"]
			if len(filters) != 2 || filters[0] != "mlx" || filters[1] != "text-generation" || request.URL.Query().Get("sort") != "downloads" || request.URL.Query().Get("direction") != "-1" {
				t.Errorf("Hugging Face popular query=%q", request.URL.RawQuery)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `[
				{"modelId":"mlx-community/Qwen3-8B-4bit","downloads":900,"pipeline_tag":"text-generation","gated":false},
				{"modelId":"mlx-community/vision-only","downloads":800,"pipeline_tag":"image-to-text","gated":false},
				{"modelId":"mlx-community/private-model","downloads":700,"pipeline_tag":"text-generation","gated":true},
				{"modelId":"mlx-community/Qwen3-4B-4bit","downloads":600,"pipeline_tag":"text-generation","gated":false}
			]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	remote := NewRemote(server.Client(), server.URL+"/ollama", server.URL+"/huggingface")
	ollama, err := remote.Popular(context.Background(), Ollama, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(ollama) != 2 || ollama[0].ID != "llama3.2" || ollama[0].PopularityRank != 1 || ollama[0].Downloads != 77_500_000 || ollama[0].ParameterSize != "3B" || ollama[0].SizeBytes != 2_000_000_000 {
		t.Fatalf("Ollama popular=%+v", ollama)
	}
	if ollama[1].ID != "deepseek-r1" || ollama[1].PopularityRank != 2 || ollama[1].Downloads != 90_300_000 || ollama[1].ParameterSize != "7B" || ollama[1].SizeBytes != 4_700_000_000 {
		t.Fatalf("Ollama second popular=%+v", ollama[1])
	}

	mlx, err := remote.Popular(context.Background(), MLX, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(mlx) != 2 || mlx[0].ID != "mlx-community/Qwen3-8B-4bit" || mlx[0].PopularityRank != 1 || mlx[0].Downloads != 900 {
		t.Fatalf("MLX popular=%+v", mlx)
	}
	if mlx[1].ID != "mlx-community/Qwen3-4B-4bit" || mlx[1].PopularityRank != 2 || mlx[1].Downloads != 600 {
		t.Fatalf("MLX second popular=%+v", mlx[1])
	}
}
