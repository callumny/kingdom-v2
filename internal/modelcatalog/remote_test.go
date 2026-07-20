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
