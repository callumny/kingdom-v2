package localmodels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
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
		_, _ = io.WriteString(writer, "{\"status\":\"pulling manifest\"}\n{\"status\":\"downloading\",\"completed\":25,\"total\":100}\n{\"status\":\"success\",\"completed\":100,\"total\":100}\n")
	}))
	defer server.Close()

	downloader := NewDownloader(nil, server.Client(), "", "")
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
}

func TestDownloaderUsesManagedHFCommandAndParsesMLXProgress(t *testing.T) {
	runtimeRoot := t.TempDir()
	cacheRoot := filepath.Join(t.TempDir(), "hub")
	hf := filepath.Join(runtimeRoot, "mlx", "bin", "hf")
	system := &fakeStreamSystem{path: "/fallback/hf", lines: []string{"Fetching 4 files: 25%", "Fetching 4 files: 75%"}}
	downloader := NewDownloader(system, nil, runtimeRoot, cacheRoot)
	var progress []DownloadProgress
	if err := downloader.Download(context.Background(), DownloadRequest{Kind: KindMLX, Model: "mlx-community/Qwen3-4B-4bit"}, func(update DownloadProgress) {
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
}
