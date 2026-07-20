package localmodels

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/topology"
)

type commandCall struct {
	name string
	args []string
	env  []string
}

type fakeSystem struct {
	paths   map[string]string
	outputs map[string][]byte
	errors  map[string]error
	started []commandCall
	run     []commandCall
}

func (s *fakeSystem) LookPath(name string) (string, error) {
	if path := s.paths[name]; path != "" {
		return path, nil
	}
	return "", errors.New("not found")
}

func (s *fakeSystem) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := commandCall{name: name, args: append([]string(nil), args...)}
	s.run = append(s.run, call)
	key := name + " " + joinArgs(args)
	return append([]byte(nil), s.outputs[key]...), s.errors[key]
}

func (s *fakeSystem) Start(name string, args, env []string) error {
	s.started = append(s.started, commandCall{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)})
	return s.errors[name+" "+joinArgs(args)]
}

type fakeDiscoverer struct{ results map[string]discovery.Result }

func (d fakeDiscoverer) Discover(_ context.Context, endpoints []topology.Endpoint) ([]discovery.Result, error) {
	result := d.results[endpoints[0].ID]
	result.Endpoint = endpoints[0]
	return []discovery.Result{result}, nil
}

func TestInspectReportsStableProvidersWithoutLaunchingAnything(t *testing.T) {
	cache := createCachedMLXModel(t, "mlx-community", "Qwen-4bit")
	system := &fakeSystem{
		paths: map[string]string{"ollama": "/bin/ollama", "lms": "/bin/lms", "mlx_lm.server": "/bin/mlx_lm.server"},
		outputs: map[string][]byte{
			"/bin/lms ls --llm --json --no-launch": []byte(`[
				{"modelKey":"zeta/model"},
				{"modelKey":" alpha/model "},
				{"modelKey":"alpha/model"},
				{"modelKey":""}
			]`),
		},
	}
	discoverer := fakeDiscoverer{results: map[string]discovery.Result{
		"ollama-local":    {Models: []discovery.Model{{ID: "gemma3"}}},
		"lm-studio-local": {Err: errors.New("connection refused")},
		"mlx-local":       {Err: errors.New("connection refused")},
	}}

	runtimes := New(system, discoverer, cache).Inspect(context.Background())

	if len(runtimes) != 3 || runtimes[0].Kind != KindOllama || runtimes[1].Kind != KindLMStudio || runtimes[2].Kind != KindMLX {
		t.Fatalf("runtime order=%+v", runtimes)
	}
	for _, runtime := range runtimes {
		if runtime.InstallHint == "" {
			t.Fatalf("%s has no installation guidance", runtime.Name)
		}
	}
	if !runtimes[0].Installed || !runtimes[0].Running || !reflect.DeepEqual(runtimes[0].Models, []Model{{ID: "gemma3", Loaded: true}}) {
		t.Fatalf("Ollama=%+v", runtimes[0])
	}
	if !runtimes[1].Installed || runtimes[1].Running || !reflect.DeepEqual(runtimes[1].Models, []Model{{ID: "alpha/model"}, {ID: "zeta/model"}}) {
		t.Fatalf("LM Studio=%+v", runtimes[1])
	}
	if !runtimes[2].Installed || runtimes[2].Running || len(runtimes[2].Models) != 1 || runtimes[2].Models[0].ID != "mlx-community/Qwen-4bit" || runtimes[2].Models[0].LocalPath == "" {
		t.Fatalf("MLX=%+v", runtimes[2])
	}
	if len(system.started) != 0 {
		t.Fatalf("inspection launched processes: %+v", system.started)
	}
}

func TestInspectHandlesMissingCLIsMalformedOutputAndPartialProviders(t *testing.T) {
	system := &fakeSystem{paths: map[string]string{"lms": "/bin/lms"}, outputs: map[string][]byte{"/bin/lms ls --llm --json --no-launch": []byte(`{"not":"an array"}`)}}
	runtimes := New(system, fakeDiscoverer{results: map[string]discovery.Result{}}, t.TempDir()).Inspect(context.Background())
	if runtimes[0].Installed || !runtimes[1].Installed || runtimes[2].Installed {
		t.Fatalf("installed flags=%+v", runtimes)
	}
	if runtimes[1].Warning == "" {
		t.Fatalf("malformed LM Studio output was hidden: %+v", runtimes[1])
	}
}

func TestStartUsesArgumentVectorsAndOfflineMLX(t *testing.T) {
	cache := createCachedMLXModel(t, "mlx-community", "Qwen-4bit")
	system := &fakeSystem{paths: map[string]string{"ollama": "/tools/ollama", "lms": "/tools/lms", "mlx_lm.server": "/tools/mlx_lm.server"}, outputs: map[string][]byte{
		"/tools/lms ls --llm --json --no-launch": []byte(`[{"modelKey":"alpha/model"}]`),
	}}
	manager := New(system, fakeDiscoverer{results: map[string]discovery.Result{}}, cache)

	if err := manager.Start(context.Background(), KindOllama, ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), KindLMStudio, "alpha/model"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), KindMLX, "mlx-community/Qwen-4bit"); err != nil {
		t.Fatal(err)
	}

	wantStarted := []commandCall{
		{name: "/tools/ollama", args: []string{"serve"}},
		{name: "/tools/mlx_lm.server", args: []string{"--model", "mlx-community/Qwen-4bit", "--host", "127.0.0.1", "--port", "8080"}, env: []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=" + cache}},
	}
	if !reflect.DeepEqual(system.started, wantStarted) {
		t.Fatalf("started=%+v want=%+v", system.started, wantStarted)
	}
	wantRun := []commandCall{
		{name: "/tools/lms", args: []string{"ls", "--llm", "--json", "--no-launch"}},
		{name: "/tools/lms", args: []string{"server", "start", "--port", "1234", "--bind", "127.0.0.1"}},
		{name: "/tools/lms", args: []string{"load", "alpha/model", "--identifier", "alpha/model", "-y"}},
	}
	if !reflect.DeepEqual(system.run, wantRun) {
		t.Fatalf("run=%+v want=%+v", system.run, wantRun)
	}
}

func TestStartRejectsMissingInstallAndUnknownOrUnsafeModels(t *testing.T) {
	cache := createCachedMLXModel(t, "safe", "model")
	manager := New(&fakeSystem{paths: map[string]string{"lms": "/bin/lms", "mlx_lm.server": "/bin/mlx_lm.server"}, outputs: map[string][]byte{}}, fakeDiscoverer{}, cache)
	for _, item := range []struct {
		kind  Kind
		model string
	}{
		{KindOllama, ""},
		{KindLMStudio, ""},
		{KindMLX, "../../outside"},
		{KindMLX, "unknown/model"},
		{"unknown", "model"},
	} {
		if err := manager.Start(context.Background(), item.kind, item.model); err == nil {
			t.Fatalf("Start(%q, %q) succeeded", item.kind, item.model)
		}
	}
}

type readinessProvider struct {
	states []Runtime
	starts int
}

func (*readinessProvider) kind() Kind { return KindMLX }
func (p *readinessProvider) start(context.Context, string) error {
	p.starts++
	return nil
}
func (p *readinessProvider) inspect(context.Context) Runtime {
	if len(p.states) == 0 {
		return Runtime{Kind: KindMLX}
	}
	state := p.states[0]
	if len(p.states) > 1 {
		p.states = p.states[1:]
	}
	return state
}

func TestStartAndWaitRequiresSelectedModelReadiness(t *testing.T) {
	adapter := &readinessProvider{states: []Runtime{
		{Kind: KindMLX, Running: false},
		{Kind: KindMLX, Running: true, Models: []Model{{ID: "chosen", Loaded: false}}},
		{Kind: KindMLX, Running: true, Models: []Model{{ID: "chosen", Loaded: true}}},
	}}
	manager := &Manager{providers: []provider{adapter}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.StartAndWait(ctx, KindMLX, "chosen"); err != nil {
		t.Fatal(err)
	}
	if adapter.starts != 1 {
		t.Fatalf("starts=%d", adapter.starts)
	}
}

func TestStartAndWaitHonoursCancellation(t *testing.T) {
	adapter := &readinessProvider{states: []Runtime{{Kind: KindMLX, Running: false}}}
	manager := &Manager{providers: []provider{adapter}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := manager.StartAndWait(ctx, KindMLX, "chosen"); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation error=%v", err)
	}
}

func TestEnvironmentOverridesReplaceExistingValues(t *testing.T) {
	got := mergedEnvironment([]string{"PATH=/bin", "HF_HUB_OFFLINE=0", "KEEP=yes"}, []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=/cache"})
	want := []string{"PATH=/bin", "KEEP=yes", "HF_HUB_OFFLINE=1", "HF_HUB_CACHE=/cache"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment=%v want=%v", got, want)
	}
}

func TestBoundedBufferReportsFullWritesAndCapsStorage(t *testing.T) {
	buffer := &boundedBuffer{remaining: 3}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 || string(buffer.Bytes()) != "abc" {
		t.Fatalf("written=%d bytes=%q err=%v", written, buffer.Bytes(), err)
	}
	if written, err := buffer.Write([]byte("more")); err != nil || written != 4 || string(buffer.Bytes()) != "abc" {
		t.Fatalf("second write=%d bytes=%q err=%v", written, buffer.Bytes(), err)
	}
}

func TestMLXCacheIgnoresIncompleteModels(t *testing.T) {
	root := filepath.Join(t.TempDir(), "hub")
	snapshot := filepath.Join(root, "models--owner--incomplete", "snapshots", "abc")
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshot, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	models, err := scanMLXCache(root)
	if err != nil || len(models) != 0 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}

func TestOSSystemOutputIsBoundedAndCancellationAware(t *testing.T) {
	t.Setenv("KINGDOM_LOCALMODELS_HELPER", "1")
	system := OSSystem{}
	output, err := system.Output(context.Background(), os.Args[0], "-test.run=TestLocalModelsHelperProcess", "--", "output")
	if err != nil || len(output) != maxCommandOutput {
		t.Fatalf("output bytes=%d err=%v", len(output), err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := system.Output(ctx, os.Args[0], "-test.run=TestLocalModelsHelperProcess", "--", "sleep"); err == nil {
		t.Fatal("cancelled command succeeded")
	}
	if path, err := system.LookPath(os.Args[0]); err != nil || path == "" {
		t.Fatalf("LookPath=%q err=%v", path, err)
	}
}

func TestLocalModelsHelperProcess(t *testing.T) {
	if os.Getenv("KINGDOM_LOCALMODELS_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "output":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", maxCommandOutput+1024))
	case "sleep":
		time.Sleep(time.Second)
	}
}

func createCachedMLXModel(t *testing.T, owner, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "hub")
	snapshot := filepath.Join(root, "models--"+owner+"--"+name, "snapshots", "abc")
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	for filename, contents := range map[string]string{"config.json": `{}`, "model.safetensors": "weights"} {
		if err := os.WriteFile(filepath.Join(snapshot, filename), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func joinArgs(args []string) string {
	value := ""
	for index, arg := range args {
		if index > 0 {
			value += " "
		}
		value += arg
	}
	return value
}
