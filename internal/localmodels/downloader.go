package localmodels

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type DownloadRequest struct {
	Kind    Kind
	Model   string
	BaseURL string
	// SizeBytes is optional catalogue metadata. Providers that only stream a
	// percentage use it to calculate transferred bytes, speed, and ETA.
	SizeBytes int64
}

type RemoveRequest struct {
	Kind    Kind
	Model   string
	BaseURL string
}

type DownloadProgress struct {
	Provider        Kind
	Model           string
	Status          string
	Percent         int
	DownloadedBytes int64
	TotalBytes      int64
	BytesPerSecond  int64
	ETA             time.Duration
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
	huggingURL  string
	now         func() time.Time
}

func NewDownloader(system StreamSystem, client *http.Client, runtimeRoot, cacheRoot string) *Downloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &Downloader{
		system:      system,
		client:      client,
		runtimeRoot: filepath.Clean(runtimeRoot),
		cacheRoot:   filepath.Clean(cacheRoot),
		huggingURL:  "https://huggingface.co/api/models",
		now:         time.Now,
	}
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

func (d *Downloader) Remove(ctx context.Context, request RemoveRequest) error {
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" {
		return errors.New("model name is required")
	}
	switch request.Kind {
	case KindOllama:
		return d.removeOllama(ctx, request)
	case KindMLX:
		return d.removeMLX(ctx, request)
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
	meter := newTransferMeter(d.now)
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
		progress := meter.progress(request.Model, update.Status, percent, update.Completed, update.Total)
		progress.Provider = KindOllama
		reportDownloadProgress(report, progress)
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
	if request.SizeBytes <= 0 {
		request.SizeBytes = d.resolveMLXSize(ctx, request.Model)
	}
	hf := filepath.Join(d.runtimeRoot, "mlx", "bin", "hf")
	args := []string{"download", request.Model, "--cache-dir", d.cacheRoot, "--max-workers", "8"}
	meter := newTransferMeter(d.now)
	progress := meter.progress(request.Model, "preparing download", 0, 0, request.SizeBytes)
	progress.Provider = KindMLX
	reportDownloadProgress(report, progress)
	err := d.system.Stream(ctx, hf, args, []string{"HF_HUB_CACHE=" + d.cacheRoot}, func(line string) {
		match := percentPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			return
		}
		percent, _ := strconv.Atoi(match[1])
		completed := request.SizeBytes * int64(percent) / 100
		progress := meter.progress(request.Model, "downloading", percent, completed, request.SizeBytes)
		progress.Provider = KindMLX
		reportDownloadProgress(report, progress)
	})
	if err != nil {
		return fmt.Errorf("download MLX model: %w", err)
	}
	progress = meter.progress(request.Model, "download complete", 100, request.SizeBytes, request.SizeBytes)
	progress.Provider = KindMLX
	reportDownloadProgress(report, progress)
	return nil
}

func (d *Downloader) resolveMLXSize(ctx context.Context, repository string) int64 {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || d.client == nil || strings.TrimSpace(d.huggingURL) == "" {
		return 0
	}
	endpoint := strings.TrimRight(d.huggingURL, "/") + "/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	metadataContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(metadataContext, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0
	}
	response, err := d.client.Do(request)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0
	}
	const maxMetadataBytes = 64 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil || len(body) > maxMetadataBytes {
		return 0
	}
	var metadata struct {
		UsedStorage int64 `json:"usedStorage"`
	}
	if json.Unmarshal(body, &metadata) != nil || metadata.UsedStorage < 0 {
		return 0
	}
	return metadata.UsedStorage
}

func (d *Downloader) removeOllama(ctx context.Context, request RemoveRequest) error {
	endpoint, err := localOllamaModelURL(request.BaseURL, "/api/delete", "removals")
	if err != nil {
		return err
	}
	body := strings.NewReader(fmt.Sprintf("{\"model\":%s}\n", mustJSON(request.Model)))
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, body)
	if err != nil {
		return fmt.Errorf("create Ollama removal request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	client := *d.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("remove Ollama model: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("remove Ollama model: HTTP %d", response.StatusCode)
	}
	return nil
}

func (d *Downloader) removeMLX(ctx context.Context, request RemoveRequest) error {
	if d.system == nil {
		return errors.New("MLX cache manager is unavailable")
	}
	if !validRepositoryID(request.Model) {
		return fmt.Errorf("invalid MLX repository %q", request.Model)
	}
	if d.runtimeRoot == "." || d.cacheRoot == "." {
		return errors.New("MLX cache directory is unavailable")
	}
	hf := filepath.Join(d.runtimeRoot, "mlx", "bin", "hf")
	args := []string{"cache", "rm", "model/" + request.Model, "--cache-dir", d.cacheRoot, "--yes"}
	if err := d.system.Stream(ctx, hf, args, []string{"HF_HUB_CACHE=" + d.cacheRoot}, func(string) {}); err != nil {
		return fmt.Errorf("remove MLX model: %w", err)
	}
	return nil
}

func localOllamaPullURL(base string) (string, error) {
	return localOllamaModelURL(base, "/api/pull", "downloads")
}

func localOllamaModelURL(base, path, operation string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid Ollama endpoint")
	}
	host := parsed.Hostname()
	address := net.ParseIP(host)
	if host != "localhost" && (address == nil || !address.IsLoopback()) {
		return "", fmt.Errorf("Ollama %s require a loopback endpoint", operation)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
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

type transferMeter struct {
	started time.Time
	now     func() time.Time
}

func newTransferMeter(now func() time.Time) transferMeter {
	if now == nil {
		now = time.Now
	}
	return transferMeter{started: now(), now: now}
}

func (m transferMeter) progress(model, status string, percent int, completed, total int64) DownloadProgress {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	if completed < 0 {
		completed = 0
	}
	if total < 0 {
		total = 0
	}
	if total > 0 && completed > total {
		completed = total
	}
	progress := DownloadProgress{
		Model:           model,
		Status:          strings.TrimSpace(status),
		Percent:         percent,
		DownloadedBytes: completed,
		TotalBytes:      total,
	}
	elapsed := m.now().Sub(m.started)
	if completed > 0 && elapsed > 0 {
		progress.BytesPerSecond = int64(float64(completed) / elapsed.Seconds())
	}
	if progress.BytesPerSecond > 0 && total > completed {
		progress.ETA = time.Duration(float64(total-completed) / float64(progress.BytesPerSecond) * float64(time.Second))
	}
	return progress
}

func reportDownloadProgress(report DownloadReporter, progress DownloadProgress) {
	if report == nil {
		return
	}
	report(progress)
}
