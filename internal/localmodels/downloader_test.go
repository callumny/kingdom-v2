package localmodels

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeStreamSystem struct {
	path  string
	lines []string
	call  commandCall
}

func (s *fakeStreamSystem) LookPath(string) (string, error) { return s.path, nil }
func (s *fakeStreamSystem) Stream(_ context.Context, name string, args, env []string, output func(string)) error {
	s.call = commandCall{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)}
	for _, line := range s.lines {
		output(line)
	}
	return nil
}

func TestDownloaderStreamsExactOllamaProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/pull" {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"model":"qwen3:8b","stream":true}`+"\n" {
			t.Fatalf("body=%s", body)
		}
		_, _ = io.WriteString(writer, "{\"status\":\"pulling manifest\"}\n{\"status\":\"downloading\",\"completed\":250000000,\"total\":1000000000}\n{\"status\":\"success\",\"completed\":1000000000,\"total\":1000000000}\n")
	}))
	defer server.Close()

	downloader := NewDownloader(nil, server.Client(), "", "")
	now := time.Unix(0, 0)
	downloader.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	var progress []DownloadProgress
	err := downloader.Download(context.Background(), DownloadRequest{Kind: KindOllama, Model: "qwen3:8b", BaseURL: server.URL}, func(update DownloadProgress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != 3 || progress[1].Percent != 25 || progress[2].Percent != 100 {
		t.Fatalf("progress=%+v", progress)
	}
	if got := progress[1]; got.DownloadedBytes != 250_000_000 || got.TotalBytes != 1_000_000_000 || got.BytesPerSecond <= 0 || got.ETA <= 0 {
		t.Fatalf("Ollama transfer metrics=%+v", got)
	}
}

func TestDownloaderUsesManagedHFCommandAndParsesMLXProgress(t *testing.T) {
	runtimeRoot := t.TempDir()
	cacheRoot := filepath.Join(t.TempDir(), "hub")
	hf := filepath.Join(runtimeRoot, "mlx", "bin", "hf")
	system := &fakeStreamSystem{path: "/fallback/hf", lines: []string{"Fetching 4 files: 25%", "Fetching 4 files: 75%"}}
	downloader := NewDownloader(system, nil, runtimeRoot, cacheRoot)
	now := time.Unix(0, 0)
	downloader.now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	var progress []DownloadProgress
	if err := downloader.Download(context.Background(), DownloadRequest{Kind: KindMLX, Model: "mlx-community/Qwen3-4B-4bit", SizeBytes: 4_000_000_000}, func(update DownloadProgress) {
		progress = append(progress, update)
	}); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"download", "mlx-community/Qwen3-4B-4bit", "--cache-dir", cacheRoot, "--max-workers", "8"}
	if system.call.name != hf || !reflect.DeepEqual(system.call.args, wantArgs) {
		t.Fatalf("call=%+v want name=%q args=%v", system.call, hf, wantArgs)
	}
	if len(progress) < 4 || progress[1].Percent != 25 || progress[2].Percent != 75 || progress[len(progress)-1].Percent != 100 {
		t.Fatalf("progress=%+v", progress)
	}
	if got := progress[1]; got.DownloadedBytes != 1_000_000_000 || got.TotalBytes != 4_000_000_000 || got.BytesPerSecond <= 0 || got.ETA <= 0 {
		t.Fatalf("MLX transfer metrics=%+v", got)
	}
}

func TestDownloaderResolvesSelectedMLXModelSizeBeforeDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/models/mlx-community/Qwen3-4B-4bit" {
			t.Fatalf("metadata path=%q", request.URL.Path)
		}
		_, _ = io.WriteString(writer, `{"usedStorage":4000000000}`)
	}))
	defer server.Close()

	system := &fakeStreamSystem{lines: []string{"Fetching 4 files: 25%"}}
	downloader := NewDownloader(system, server.Client(), t.TempDir(), t.TempDir())
	downloader.huggingURL = server.URL + "/api/models"
	var progress []DownloadProgress
	if err := downloader.Download(context.Background(), DownloadRequest{Kind: KindMLX, Model: "mlx-community/Qwen3-4B-4bit"}, func(update DownloadProgress) {
		progress = append(progress, update)
	}); err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 || progress[1].TotalBytes != 4_000_000_000 || progress[1].DownloadedBytes != 1_000_000_000 {
		t.Fatalf("resolved MLX progress=%+v", progress)
	}
}

func TestDownloaderRemovesOllamaModelThroughLoopbackAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/api/delete" {
			t.Fatalf("request=%s %s", request.Method, request.URL.Path)
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"model":"qwen3:8b"}`+"\n" {
			t.Fatalf("body=%s", body)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	downloader := NewDownloader(nil, server.Client(), "", "")
	if err := downloader.Remove(context.Background(), RemoveRequest{Kind: KindOllama, Model: "qwen3:8b", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}
}

func TestDownloaderRemovesMLXModelThroughManagedHFCache(t *testing.T) {
	runtimeRoot := t.TempDir()
	cacheRoot := filepath.Join(t.TempDir(), "hub")
	hf := filepath.Join(runtimeRoot, "mlx", "bin", "hf")
	system := &fakeStreamSystem{}
	downloader := NewDownloader(system, nil, runtimeRoot, cacheRoot)

	if err := downloader.Remove(context.Background(), RemoveRequest{Kind: KindMLX, Model: "mlx-community/Qwen3-4B-4bit"}); err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{"cache", "rm", "model/mlx-community/Qwen3-4B-4bit", "--cache-dir", cacheRoot, "--yes"}
	if system.call.name != hf || !reflect.DeepEqual(system.call.args, wantArgs) {
		t.Fatalf("call=%+v want name=%q args=%v", system.call, hf, wantArgs)
	}
}

func TestDownloaderRejectsUnsafeModelRemovalRequests(t *testing.T) {
	downloader := NewDownloader(&fakeStreamSystem{}, nil, "", "")
	tests := []RemoveRequest{
		{Kind: KindOllama, Model: "qwen", BaseURL: "https://example.com"},
		{Kind: KindMLX, Model: "not-a-repository"},
	}
	for _, request := range tests {
		if err := downloader.Remove(context.Background(), request); err == nil {
			t.Fatalf("unsafe removal accepted: %+v", request)
		}
	}

	failed := NewDownloader(&failingStreamSystem{err: errors.New("cache busy")}, nil, "/runtime", "/cache")
	if err := failed.Remove(context.Background(), RemoveRequest{Kind: KindMLX, Model: "org/model"}); err == nil {
		t.Fatal("MLX command failure was ignored")
	}
}

type failingStreamSystem struct{ err error }

func (s *failingStreamSystem) LookPath(string) (string, error) { return "", nil }
func (s *failingStreamSystem) Stream(context.Context, string, []string, []string, func(string)) error {
	return s.err
}
