package localmodels

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

func TestLMStudioCLIInventoryMergesWithLiveLocalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, `{"data":[{"id":"alpha/model"}]}`)
	}))
	defer server.Close()

	system := &fakeSystem{
		paths: map[string]string{"lms": "/tools/lms"},
		outputs: map[string][]byte{
			"/tools/lms ls --llm --json --no-launch": []byte(`[{"modelKey":"alpha/model"},{"modelKey":"beta/model"}]`),
		},
	}
	provider := &lmStudioProvider{
		system:     system,
		discoverer: discovery.New(discovery.DefaultOptions()),
		endpoint: topology.Endpoint{
			ID:      "lm-studio-test",
			Name:    "LM Studio",
			Kind:    topology.KindOpenAICompatible,
			BaseURL: server.URL + "/v1",
		},
	}

	status := provider.inspect(context.Background())
	if !status.Installed || !status.Running || status.Warning != "" || len(status.Models) != 2 {
		t.Fatalf("status=%+v", status)
	}
	if !status.Models[0].Loaded || status.Models[1].Loaded {
		t.Fatalf("loaded merge=%+v", status.Models)
	}
}
