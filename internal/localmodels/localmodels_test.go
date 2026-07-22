package localmodels

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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
	paths     map[string]string
	outputs   map[string][]byte
	errors    map[string]error
	started   []commandCall
	run       []commandCall
	startHook func(commandCall)
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
	call := commandCall{name: name, args: append([]string(nil), args...), env: append([]string(nil), env...)}
	s.started = append(s.started, call)
	if s.startHook != nil {
		s.startHook(call)
	}
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
		paths: map[string]string{"ollama": "/bin/ollama", "mlx_lm.server": "/bin/mlx_lm.server"},
	}
	discoverer := fakeDiscoverer{results: map[string]discovery.Result{
		"ollama-local": {Models: []discovery.Model{{ID: "gemma3", SizeBytes: 5_200_000_000, ParameterSize: "8B", Quantization: "Q4_K_M"}}},
		"mlx-local":    {Err: errors.New("connection refused")},
	}}

	runtimes := New(system, discoverer, cache).Inspect(context.Background())

	if len(runtimes) != 2 || runtimes[0].Kind != KindOllama || runtimes[1].Kind != KindMLX {
		t.Fatalf("runtime order=%+v", runtimes)
	}
	for _, runtime := range runtimes {
		if runtime.InstallHint == "" {
			t.Fatalf("%s has no installation guidance", runtime.Name)
		}
	}
	if !runtimes[0].Installed || !runtimes[0].Running || !reflect.DeepEqual(runtimes[0].Models, []Model{{ID: "gemma3", Loaded: true, SizeBytes: 5_200_000_000, ParameterSize: "8B", Quantization: "Q4_K_M"}}) {
		t.Fatalf("Ollama=%+v", runtimes[0])
	}
	if !runtimes[1].Installed || runtimes[1].Running || len(runtimes[1].Models) != 1 || runtimes[1].Models[0].ID != "mlx-community/Qwen-4bit" || runtimes[1].Models[0].LocalPath == "" {
		t.Fatalf("MLX=%+v", runtimes[1])
	}
	if len(system.started) != 0 {
		t.Fatalf("inspection launched processes: %+v", system.started)
	}
}

func TestInspectHandlesMissingCLIs(t *testing.T) {
	system := &fakeSystem{}
	runtimes := New(system, fakeDiscoverer{results: map[string]discovery.Result{}}, t.TempDir()).Inspect(context.Background())
	if len(runtimes) != 2 || runtimes[0].Installed || runtimes[1].Installed {
		t.Fatalf("installed flags=%+v", runtimes)
	}
}

func TestStartUsesArgumentVectorsAndOfflineMLX(t *testing.T) {
	cache := createCachedMLXModel(t, "mlx-community", "Qwen-4bit")
	system := &fakeSystem{paths: map[string]string{"ollama": "/tools/ollama", "mlx_lm.server": "/tools/mlx_lm.server"}}
	manager := New(system, fakeDiscoverer{results: map[string]discovery.Result{}}, cache)

	if err := manager.Start(context.Background(), KindOllama, ""); err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(context.Background(), KindMLX, "mlx-community/Qwen-4bit"); err != nil {
		t.Fatal(err)
	}

	wantStarted := []commandCall{
		{name: "/tools/ollama", args: []string{"serve"}, env: []string{"OLLAMA_HOST=localhost:11434"}},
		{name: "/tools/mlx_lm.server", args: []string{"--model", "mlx-community/Qwen-4bit", "--host", "127.0.0.1", "--port", "8080"}, env: []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=" + cache}},
	}
	if !reflect.DeepEqual(system.started, wantStarted) {
		t.Fatalf("started=%+v want=%+v", system.started, wantStarted)
	}
	if len(system.run) != 0 {
		t.Fatalf("unexpected synchronous commands: %+v", system.run)
	}
}

func TestConfigureAndStartOllamaUsesSelectedLoopbackPort(t *testing.T) {
	system := &fakeSystem{paths: map[string]string{"ollama": "/tools/ollama"}}
	manager := New(system, fakeDiscoverer{results: map[string]discovery.Result{}}, t.TempDir())
	if err := manager.ConfigureAndStart(context.Background(), KindOllama, 12001); err != nil {
		t.Fatal(err)
	}
	want := commandCall{name: "/tools/ollama", args: []string{"serve"}, env: []string{"OLLAMA_HOST=127.0.0.1:12001"}}
	if len(system.started) != 1 || !reflect.DeepEqual(system.started[0], want) {
		t.Fatalf("started=%+v want=%+v", system.started, want)
	}
}

func TestEnsureOllamaServersStartsOnlyMissingEndpoints(t *testing.T) {
	discoverer := &endpointReadiness{running: map[string]bool{"http://127.0.0.1:12000": true}}
	system := &fakeSystem{paths: map[string]string{"ollama": "/tools/ollama"}}
	system.startHook = discoverer.markStarted
	manager := New(system, discoverer, t.TempDir())
	endpoints := []topology.Endpoint{
		{ID: "large", Name: "large", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12000"},
		{ID: "small", Name: "small", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12001"},
	}

	if err := manager.EnsureOllamaServers(context.Background(), endpoints); err != nil {
		t.Fatal(err)
	}
	want := commandCall{name: "/tools/ollama", args: []string{"serve"}, env: []string{"OLLAMA_HOST=127.0.0.1:12001"}}
	if len(system.started) != 1 || !reflect.DeepEqual(system.started[0], want) {
		t.Fatalf("started=%+v want=%+v", system.started, want)
	}
}

func TestEnsureOllamaServersStartsEachUniquePortOnce(t *testing.T) {
	discoverer := &endpointReadiness{running: make(map[string]bool)}
	system := &fakeSystem{paths: map[string]string{"ollama": "/tools/ollama"}}
	system.startHook = discoverer.markStarted
	manager := New(system, discoverer, t.TempDir())
	endpoints := []topology.Endpoint{
		{ID: "large", Name: "large", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12000"},
		{ID: "small", Name: "small", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12001"},
		{ID: "large-again", Name: "large", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1:12000"},
	}

	if err := manager.EnsureOllamaServers(context.Background(), endpoints); err != nil {
		t.Fatal(err)
	}
	want := []commandCall{
		{name: "/tools/ollama", args: []string{"serve"}, env: []string{"OLLAMA_HOST=127.0.0.1:12000"}},
		{name: "/tools/ollama", args: []string{"serve"}, env: []string{"OLLAMA_HOST=127.0.0.1:12001"}},
	}
	if !reflect.DeepEqual(system.started, want) {
		t.Fatalf("started=%+v want=%+v", system.started, want)
	}
}

func TestEnsureOllamaServersRejectsUnsafeEndpoints(t *testing.T) {
	manager := New(&fakeSystem{paths: map[string]string{"ollama": "/tools/ollama"}}, fakeDiscoverer{}, t.TempDir())
	for _, endpoint := range []topology.Endpoint{
		{ID: "remote", Kind: topology.KindOllama, BaseURL: "http://192.168.1.20:11434"},
		{ID: "wrong-kind", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:11434"},
		{ID: "missing-port", Kind: topology.KindOllama, BaseURL: "http://127.0.0.1"},
	} {
		if err := manager.EnsureOllamaServers(context.Background(), []topology.Endpoint{endpoint}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %+v", endpoint)
		}
	}
}

func TestEnsureMLXServersStartsEachModelOnItsAssignedPort(t *testing.T) {
	cache := createCachedMLXModel(t, "mlx-community", "large")
	createCachedMLXModelAt(t, cache, "mlx-community", "small")
	discoverer := &mlxEndpointReadiness{loaded: make(map[string]string)}
	system := &fakeSystem{paths: map[string]string{"mlx_lm.server": "/tools/mlx_lm.server"}}
	system.startHook = discoverer.markStarted
	manager := New(system, discoverer, cache)
	servers := []ModelServer{
		{Model: "mlx-community/large", Endpoint: topology.Endpoint{ID: "large", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:13000/v1"}},
		{Model: "mlx-community/small", Endpoint: topology.Endpoint{ID: "small", Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:13001/v1"}},
	}

	if err := manager.EnsureMLXServers(context.Background(), servers); err != nil {
		t.Fatal(err)
	}
	want := []commandCall{
		{name: "/tools/mlx_lm.server", args: []string{"--model", "mlx-community/large", "--host", "127.0.0.1", "--port", "13000"}, env: []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=" + cache}},
		{name: "/tools/mlx_lm.server", args: []string{"--model", "mlx-community/small", "--host", "127.0.0.1", "--port", "13001"}, env: []string{"HF_HUB_OFFLINE=1", "HF_HUB_CACHE=" + cache}},
	}
	if !reflect.DeepEqual(system.started, want) {
		t.Fatalf("started=%+v want=%+v", system.started, want)
	}
}

func TestEnsureMLXServersMovesAnOccupiedPortAndReturnsTheRuntimeEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupiedPort := listener.Addr().(*net.TCPAddr).Port
	if occupiedPort == 65535 {
		t.Skip("cannot allocate a following port")
	}

	cache := createCachedMLXModel(t, "mlx-community", "small")
	discoverer := &mlxEndpointReadiness{loaded: make(map[string]string)}
	system := &fakeSystem{paths: map[string]string{"mlx_lm.server": "/tools/mlx_lm.server"}}
	system.startHook = discoverer.markStarted
	manager := New(system, discoverer, cache)
	servers := []ModelServer{{
		Model: "mlx-community/small",
		Endpoint: topology.Endpoint{
			ID:      "small",
			Kind:    topology.KindOpenAICompatible,
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", occupiedPort),
		},
	}}

	if err := manager.EnsureMLXServers(context.Background(), servers); err != nil {
		t.Fatal(err)
	}
	if servers[0].Endpoint.BaseURL == fmt.Sprintf("http://127.0.0.1:%d/v1", occupiedPort) {
		t.Fatalf("occupied endpoint was not replaced: %+v", servers[0].Endpoint)
	}
	if len(system.started) != 1 || slices.Contains(system.started[0].args, strconv.Itoa(occupiedPort)) {
		t.Fatalf("started on occupied port: %+v", system.started)
	}
	second := []ModelServer{{
		Model: "mlx-community/small",
		Endpoint: topology.Endpoint{
			ID:      "small",
			Kind:    topology.KindOpenAICompatible,
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d/v1", occupiedPort),
		},
	}}
	if err := manager.EnsureMLXServers(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if len(system.started) != 1 || second[0].Endpoint != servers[0].Endpoint {
		t.Fatalf("resolved server was not reused: starts=%+v first=%+v second=%+v", system.started, servers, second)
	}
}

func TestEnsureMLXServersRejectsRemoteOrConflictingEndpoints(t *testing.T) {
	manager := New(&fakeSystem{paths: map[string]string{"mlx_lm.server": "/tools/mlx_lm.server"}}, fakeDiscoverer{}, t.TempDir())
	for _, servers := range [][]ModelServer{
		{{Model: "model", Endpoint: topology.Endpoint{Kind: topology.KindOpenAICompatible, BaseURL: "http://192.168.1.20:8080/v1"}}},
		{
			{Model: "one", Endpoint: topology.Endpoint{Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"}},
			{Model: "two", Endpoint: topology.Endpoint{Kind: topology.KindOpenAICompatible, BaseURL: "http://127.0.0.1:8080/v1"}},
		},
	} {
		if err := manager.EnsureMLXServers(context.Background(), servers); err == nil {
			t.Fatalf("unsafe MLX servers accepted: %+v", servers)
		}
	}
}

type endpointReadiness struct {
	running map[string]bool
}

func (d *endpointReadiness) Discover(_ context.Context, endpoints []topology.Endpoint) ([]discovery.Result, error) {
	result := discovery.Result{Endpoint: endpoints[0]}
	if !d.running[endpoints[0].BaseURL] {
		result.Err = errors.New("connection refused")
	}
	return []discovery.Result{result}, nil
}

func (d *endpointReadiness) markStarted(call commandCall) {
	for _, value := range call.env {
		if strings.HasPrefix(value, "OLLAMA_HOST=") {
			d.running["http://"+strings.TrimPrefix(value, "OLLAMA_HOST=")] = true
		}
	}
}

type mlxEndpointReadiness struct {
	loaded map[string]string
}

func (d *mlxEndpointReadiness) Discover(_ context.Context, endpoints []topology.Endpoint) ([]discovery.Result, error) {
	result := discovery.Result{Endpoint: endpoints[0]}
	model := d.loaded[endpoints[0].BaseURL]
	if model == "" {
		result.Err = errors.New("connection refused")
	} else {
		result.Models = []discovery.Model{{ID: model}}
	}
	return []discovery.Result{result}, nil
}

func (d *mlxEndpointReadiness) markStarted(call commandCall) {
	var model, port string
	for index := range call.args {
		if call.args[index] == "--model" && index+1 < len(call.args) {
			model = call.args[index+1]
		}
		if call.args[index] == "--port" && index+1 < len(call.args) {
			port = call.args[index+1]
		}
	}
	if model != "" && port != "" {
		d.loaded["http://127.0.0.1:"+port+"/v1"] = model
	}
}

func TestStartRejectsMissingInstallAndUnknownOrUnsafeModels(t *testing.T) {
	cache := createCachedMLXModel(t, "safe", "model")
	manager := New(&fakeSystem{paths: map[string]string{"mlx_lm.server": "/bin/mlx_lm.server"}, outputs: map[string][]byte{}}, fakeDiscoverer{}, cache)
	for _, item := range []struct {
		kind  Kind
		model string
	}{
		{KindOllama, ""},
		{Kind("unsupported"), ""},
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

func TestMLXCacheReportsInstalledSnapshotSize(t *testing.T) {
	root := createCachedMLXModel(t, "mlx-community", "Qwen-4bit")
	models, err := scanMLXCache(root)
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	if models[0].SizeBytes != int64(len(`{}`)+len("weights")) {
		t.Fatalf("size=%d", models[0].SizeBytes)
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
	createCachedMLXModelAt(t, root, owner, name)
	return root
}

func createCachedMLXModelAt(t *testing.T, root, owner, name string) {
	t.Helper()
	snapshot := filepath.Join(root, "models--"+owner+"--"+name, "snapshots", "abc")
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	for filename, contents := range map[string]string{"config.json": `{}`, "model.safetensors": "weights"} {
		if err := os.WriteFile(filepath.Join(snapshot, filename), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
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
