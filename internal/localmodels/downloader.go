package localmodels

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type DownloadRequest struct {
	Kind    Kind
	Model   string
	BaseURL string
}

type DownloadProgress struct {
	Model   string
	Status  string
	Percent int
}

type DownloadReporter func(DownloadProgress)

type StreamSystem interface {
	LookPath(string) (string, error)
	Stream(context.Context, string, []string, []string, func(string)) error
}

type Downloader struct {
	system      StreamSystem
	client      *http.Client
	runtimeRoot string
	cacheRoot   string
}

func NewDownloader(system StreamSystem, client *http.Client, runtimeRoot, cacheRoot string) *Downloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &Downloader{system: system, client: client, runtimeRoot: filepath.Clean(runtimeRoot), cacheRoot: filepath.Clean(cacheRoot)}
}

func (d *Downloader) Download(ctx context.Context, request DownloadRequest, report DownloadReporter) error {
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return errors.New("model name is required")
	}
	switch request.Kind {
	case KindOllama:
		return d.downloadOllama(ctx, request, report)
	case KindMLX:
		return d.downloadMLX(ctx, request, report)
	default:
		return fmt.Errorf("unknown model provider %q", request.Kind)
	}
}

func (d *Downloader) downloadOllama(ctx context.Context, request DownloadRequest, report DownloadReporter) error {
	endpoint, err := localOllamaPullURL(request.BaseURL)
	if err != nil {
		return err
	}
	body := strings.NewReader(fmt.Sprintf("{\"model\":%s,\"stream\":true}\n", mustJSON(request.Model)))
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create Ollama download request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := d.client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("download Ollama model: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download Ollama model: HTTP %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	succeeded := false
	for scanner.Scan() {
		var update struct {
			Status    string `json:"status"`
			Error     string `json:"error"`
			Completed int64  `json:"completed"`
			Total     int64  `json:"total"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &update); err != nil {
			return fmt.Errorf("decode Ollama download progress: %w", err)
		}
		if update.Error != "" {
			return errors.New(update.Error)
		}
		percent := 0
		if update.Total > 0 {
			percent = int(update.Completed * 100 / update.Total)
		}
		if strings.EqualFold(update.Status, "success") {
			percent = 100
			succeeded = true
		}
		reportDownload(report, request.Model, update.Status, percent)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Ollama download progress: %w", err)
	}
	if !succeeded {
		return errors.New("Ollama download ended before success")
	}
	return nil
}

var percentPattern = regexp.MustCompile(`(?:^|\D)(\d{1,3})%`)

func (d *Downloader) downloadMLX(ctx context.Context, request DownloadRequest, report DownloadReporter) error {
	if d.system == nil {
		return errors.New("MLX downloader is unavailable")
	}
	if !validRepositoryID(request.Model) {
		return fmt.Errorf("invalid MLX repository %q", request.Model)
	}
	hf := filepath.Join(d.runtimeRoot, "mlx", "bin", "hf")
	args := []string{"download", request.Model, "--cache-dir", d.cacheRoot, "--max-workers", "8"}
	reportDownload(report, request.Model, "preparing download", 0)
	err := d.system.Stream(ctx, hf, args, []string{"HF_HUB_CACHE=" + d.cacheRoot}, func(line string) {
		match := percentPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			return
		}
		percent, _ := strconv.Atoi(match[1])
		reportDownload(report, request.Model, "downloading", percent)
	})
	if err != nil {
		return fmt.Errorf("download MLX model: %w", err)
	}
	reportDownload(report, request.Model, "download complete", 100)
	return nil
}

func localOllamaPullURL(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid Ollama endpoint")
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		return "", errors.New("Ollama downloads require a loopback endpoint")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/pull"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func validRepositoryID(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, character := range part {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-", character) {
				continue
			}
			return false
		}
	}
	return true
}

func mustJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func reportDownload(report DownloadReporter, model, status string, percent int) {
	if report == nil {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	report(DownloadProgress{Model: model, Status: strings.TrimSpace(status), Percent: percent})
}
