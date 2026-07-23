package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/modelapi"
	"github.com/callumny/kingdom/internal/modelcatalog"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
	"github.com/callumny/kingdom/internal/wizard"
)

type Model struct {
	width, height           int
	config                  config.Config
	setup                   bool
	workflow                *setup.Workflow
	defaults                []topology.Endpoint
	discover                func(context.Context, uint64, []topology.Endpoint) tea.Cmd
	save                    func(config.Config) error
	screen                  setup.WorkflowState
	role                    int
	modelIndex              int
	gate                    *setup.GenerationGate
	form                    ui.CustomEndpointForm
	formActive              bool
	chat                    ui.ChatInput
	history                 []string
	progress                string
	chatError               string
	run                     RunFunc
	sessionID               string
	newSessionID            func() (string, error)
	compact                 SessionCompactor
	compacting              bool
	compactGeneration       uint64
	compactCancel           context.CancelFunc
	prepareRun              PrepareRunFunc
	warmRun                 PrepareRunFunc
	runtimeWarm             <-chan runtimeWarmResult
	runtimeWarmSignature    string
	wizardWarming           bool
	wizardWarmCancel        context.CancelFunc
	wizardWarmGeneration    uint64
	runCancel               context.CancelFunc
	runCh                   <-chan orchestration.Event
	runGen                  uint64
	running                 bool
	approval                *orchestration.ApprovalRequest
	skills                  skillState
	memory                  memoryState
	localModels             localModelState
	installer               ProviderInstaller
	modelSearch             ModelSearcher
	providerCursor          int
	providerConfirming      bool
	providerInstalling      bool
	providerNotice          string
	providerProgress        localmodels.InstallProgress
	providerInstallCh       <-chan providerInstallEvent
	providerInstallGen      uint64
	modelCursor             int
	modelInventoryGen       uint64
	modelInventoryLoading   bool
	installedModels         []setup.ModelOption
	modelQuery              string
	modelSearchActive       bool
	modelSearching          bool
	modelSearchWarning      string
	modelSearchGen          uint64
	modelSearchCancel       context.CancelFunc
	modelDownloadConfirming bool
	modelDownloader         ModelDownloader
	modelDownloadActive     bool
	modelDownloadProgress   localmodels.DownloadProgress
	modelDownloadPosition   int
	modelDownloadCount      int
	modelDownloadError      string
	modelDownloadCh         <-chan modelDownloadEvent
	modelDownloadGen        uint64
	modelDownloadCancel     context.CancelFunc
	modelRemover            ModelRemover
	modelRemoveConfirming   bool
	modelRemoveActive       bool
	modelRemoveTarget       setup.ModelOption
	modelRemoveNotice       string
	modelRemoveGen          uint64
	modelRemoveCancel       context.CancelFunc
	modelsReturnToReady     bool
	modelMetrics            map[string]modelMetric
	perfFocus               int
	prepareWizard           WizardPrepareFunc
	wizardClient            modelapi.ChatClient
	wizardEngine            *wizard.Engine
	wizardSession           *wizard.Session
	wizardModel             setup.ModelOption
	wizardInput             ui.ChatInput
	wizardMessages          []string
	wizardReady             bool
	wizardBusy              bool
	wizardApplying          bool
	wizardPreparing         bool
	wizardReturnToReady     bool
	wizardManual            bool
	wizardCancel            context.CancelFunc
	wizardGeneration        uint64
	scanning                bool
	saveGen                 uint64
	saving                  bool
}

type modelMetric struct {
	completionTokens   int
	generationDuration time.Duration
}
type DiscoverFunc func(context.Context, uint64, []topology.Endpoint) tea.Cmd
type RunFunc func(context.Context, config.Config, string, string, []skills.Skill) <-chan orchestration.Event
type PrepareRunFunc func(context.Context, config.Config) (config.Config, error)
type WizardPrepareFunc func(context.Context, config.Config, setup.ModelOption) (setup.ModelOption, error)
type SessionCompactor func(context.Context, config.Config, memory.Context) (string, memory.Usage, error)

type Services struct {
	Defaults      []topology.Endpoint
	Discover      DiscoverFunc
	Save          func(config.Config) error
	Run           RunFunc
	NewSessionID  func() (string, error)
	Compact       SessionCompactor
	PrepareRun    PrepareRunFunc
	WarmRun       PrepareRunFunc
	Skills        SkillLibrary
	Memory        MemoryBrowser
	LocalModels   LocalModelManager
	Installer     ProviderInstaller
	ModelSearch   ModelSearcher
	ModelDownload ModelDownloader
	ModelRemove   ModelRemover
	PrepareWizard WizardPrepareFunc
	WizardClient  modelapi.ChatClient
}

type DiscoveryMsg struct {
	Generation uint64
	Results    []setup.EndpointResult
}
type SaveMsg struct {
	Generation uint64
	Config     config.Config
	Err        error
}

type ProviderInstaller interface {
	InstallWithProgress(context.Context, localmodels.Kind, string, string, localmodels.ProgressReporter) error
}

type ModelSearcher interface {
	Search(context.Context, modelcatalog.Provider, string, int) ([]modelcatalog.Model, error)
}

type ModelDownloader interface {
	Download(context.Context, localmodels.DownloadRequest, localmodels.DownloadReporter) error
}

type ModelRemover interface {
	Remove(context.Context, localmodels.RemoveRequest) error
}

type providerInstallEvent struct {
	progress *localmodels.InstallProgress
	done     bool
	err      error
}

type providerInstallEventMsg struct {
	generation uint64
	kind       localmodels.Kind
	event      providerInstallEvent
}

type providerRuntimesMsg struct {
	runtimes []localmodels.Runtime
}

type modelInventoryMsg struct {
	generation uint64
	runtimes   []localmodels.Runtime
}

type modelSearchMsg struct {
	generation uint64
	models     []modelcatalog.Model
	warnings   []string
}

type modelDownloadEvent struct {
	progress  *localmodels.DownloadProgress
	position  int
	count     int
	installed *setup.ModelRef
	done      bool
	err       error
}

type modelDownloadEventMsg struct {
	generation uint64
	event      modelDownloadEvent
}

type modelRemoveMsg struct {
	generation uint64
	ref        setup.ModelRef
	err        error
}

func New(c config.Config) Model {
	return NewWithServices(c, Services{Defaults: discovery.DefaultEndpoints()})
}
func NewWithDeps(c config.Config, defaults []topology.Endpoint, discover DiscoverFunc) Model {
	return NewWithServices(c, Services{Defaults: defaults, Discover: discover})
}
func NewWithDepsAndSave(c config.Config, defaults []topology.Endpoint, discover DiscoverFunc, save func(config.Config) error) Model {
	return NewWithServices(c, Services{Defaults: defaults, Discover: discover, Save: save})
}
func NewWithServices(c config.Config, services Services) Model {
	w := setup.Start(c, services.Defaults)
	newSessionID := services.NewSessionID
	if newSessionID == nil {
		newSessionID = memory.NewSessionID
	}
	sessionID, sessionErr := newSessionID()
	model := Model{
		config:          c,
		setup:           c.RequiresSetup(),
		defaults:        services.Defaults,
		discover:        services.Discover,
		save:            services.Save,
		run:             services.Run,
		sessionID:       sessionID,
		newSessionID:    newSessionID,
		compact:         services.Compact,
		prepareRun:      services.PrepareRun,
		warmRun:         services.WarmRun,
		skills:          skillState{library: services.Skills},
		memory:          memoryState{store: services.Memory},
		localModels:     localModelState{manager: services.LocalModels},
		installer:       services.Installer,
		modelSearch:     services.ModelSearch,
		modelDownloader: services.ModelDownload,
		modelRemover:    services.ModelRemove,
		prepareWizard:   services.PrepareWizard,
		wizardClient:    services.WizardClient,
		workflow:        w,
		screen:          w.State,
		gate:            &setup.GenerationGate{},
		chat:            ui.NewChatInput(),
		modelMetrics:    make(map[string]modelMetric),
		wizardInput:     ui.NewChatInput(),
	}
	if sessionErr != nil {
		model.chatError = "start session: " + sessionErr.Error()
	}
	if model.setup && model.screen == setup.StateProviders && services.Discover != nil {
		model.scanning = true
	}
	return model
}
func (m Model) RequiresSetup() bool       { return m.setup }
func (m Model) Workflow() *setup.Workflow { return m.workflow }
func (m Model) Init() tea.Cmd {
	if m.setup && m.screen == setup.StateProviders {
		_, cmd := m.beginDiscovery()
		return cmd
	}
	return nil
}

func (m Model) beginDiscovery() (Model, tea.Cmd) {
	if m.discover == nil {
		return m, nil
	}
	gen, ctx := m.gate.Begin(context.Background())
	m.scanning = true
	m.workflow.Draft.Results = nil
	m.workflow.Err = nil
	m.modelIndex, m.modelCursor = 0, 0
	cands := setup.MergeCandidates(m.defaults, m.workflow.Draft.Config.Topology.Endpoints)
	cands = setup.ApplyProviderPorts(cands, m.workflow.Draft.Config.Providers)
	return m, m.discover(ctx, gen, cands)
}

func (m Model) startSetup() Model {
	m.gate.Cancel()
	m.cancelWizardWarmup()
	m.saveGen++
	m.setup = true
	m.workflow = setup.Start(m.config, m.defaults)
	m.workflow.State = setup.StateProviders
	m.screen = setup.StateProviders
	m.modelIndex, m.modelCursor, m.role, m.providerCursor, m.perfFocus = 0, 0, 0, 0, 0
	m.form = ui.CustomEndpointForm{}
	m.formActive, m.saving = false, false
	if m.wizardCancel != nil {
		m.wizardCancel()
	}
	m.wizardGeneration++
	m.wizardEngine, m.wizardSession = nil, nil
	m.wizardMessages = nil
	m.wizardReady, m.wizardBusy, m.wizardApplying, m.wizardPreparing = false, false, false, false
	m.wizardReturnToReady = false
	if m.modelRemoveCancel != nil {
		m.modelRemoveCancel()
	}
	m.modelRemoveGen++
	m.modelRemoveConfirming, m.modelRemoveActive = false, false
	m.modelRemoveTarget = setup.ModelOption{}
	m.modelRemoveNotice = ""
	m.modelsReturnToReady = false
	m.scanning = m.discover != nil
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = x.Width, x.Height
	case DiscoveryMsg:
		if m.workflow != nil && (m.screen == setup.StateProviders || m.screen == setup.StateModels) && m.gate.Accept(x.Generation) {
			m.workflow.Draft.ApplyResults(x.Results)
			removed := m.workflow.Draft.ReconcileModelSelection()
			if len(removed) > 0 {
				m.workflow.Err = fmt.Errorf("%d selected model(s) are no longer available", len(removed))
			}
			if len(x.Results) == 0 {
				m.providerCursor = 0
			} else if m.providerCursor >= len(x.Results) {
				m.providerCursor = len(x.Results) - 1
			}
			if n := len(m.workflow.Draft.Catalog()); n == 0 {
				m.modelCursor = 0
			} else if m.modelCursor >= n {
				m.modelCursor = n - 1
			}
			if n := m.modelCount(); n == 0 {
				m.modelIndex = 0
			} else if m.modelIndex >= n {
				m.modelIndex = n - 1
			}
			m.scanning = false
			m.focusPreferredModel()
			if m.screen == setup.StateProviders && m.localModels.manager != nil {
				return m, m.inspectProviderRuntimes()
			}
		}
	case providerRuntimesMsg:
		if m.workflow == nil || m.screen != setup.StateProviders {
			return m, nil
		}
		for _, runtime := range x.runtimes {
			switch runtime.Kind {
			case localmodels.KindOllama:
				m.workflow.Draft.SetProviderReady(setup.OllamaEndpointID, runtime.Installed && runtime.Running)
			case localmodels.KindMLX:
				m.workflow.Draft.SetProviderReady(setup.MLXEndpointID, runtime.Installed)
			}
		}
	case modelInventoryMsg:
		if m.workflow == nil || m.screen != setup.StateModels || x.generation != m.modelInventoryGen {
			return m, nil
		}
		m.modelInventoryLoading = false
		m.installedModels = m.installedModelOptions(x.runtimes)
		m.replaceVisibleModels(nil)
		m.workflow.Draft.ReconcileModelSelection()
		if count := len(m.workflow.Draft.Catalog()); count == 0 {
			m.modelCursor = 0
		} else if m.modelCursor >= count {
			m.modelCursor = count - 1
		}
	case modelSearchMsg:
		if m.workflow == nil || m.screen != setup.StateModels || x.generation != m.modelSearchGen {
			return m, nil
		}
		m.modelSearching = false
		m.modelSearchCancel = nil
		m.modelSearchWarning = strings.Join(x.warnings, "; ")
		m.replaceVisibleModels(x.models)
		if count := len(m.workflow.Draft.Catalog()); count == 0 {
			m.modelCursor = 0
		} else if m.modelCursor >= count {
			m.modelCursor = count - 1
		}
	case modelDownloadEventMsg:
		if !m.modelDownloadActive || x.generation != m.modelDownloadGen {
			return m, nil
		}
		if x.event.progress != nil {
			m.modelDownloadProgress = *x.event.progress
			m.modelDownloadPosition = x.event.position
			m.modelDownloadCount = x.event.count
			return m, m.nextModelDownloadEvent()
		}
		if x.event.installed != nil {
			m.workflow.Draft.MarkModelInstalled(*x.event.installed)
			return m, m.nextModelDownloadEvent()
		}
		if !x.event.done {
			return m, m.nextModelDownloadEvent()
		}
		m.modelDownloadActive = false
		m.modelDownloadCancel = nil
		m.modelDownloadCh = nil
		if x.event.err != nil {
			m.modelDownloadError = x.event.err.Error()
			return m, nil
		}
		return m.advanceFromModels()
	case modelRemoveMsg:
		if !m.modelRemoveActive || x.generation != m.modelRemoveGen || m.screen != setup.StateModels {
			return m, nil
		}
		m.modelRemoveActive = false
		m.modelRemoveCancel = nil
		m.modelRemoveTarget = setup.ModelOption{}
		if x.err != nil {
			m.workflow.Err = fmt.Errorf("uninstall %s: %w", x.ref.ModelID, x.err)
			return m, nil
		}
		m.workflow.Err = nil
		m.modelRemoveNotice = "Model uninstalled: " + x.ref.ModelID
		return m.beginModelInventory()
	case wizardPreparedMsg:
		if x.generation != m.wizardGeneration || m.screen != setup.StateWizard {
			return m, nil
		}
		m.wizardPreparing = false
		m.wizardCancel = nil
		if x.err != nil {
			m.workflow.Err = fmt.Errorf("Wizard conversation is unavailable: %w. You can still apply the proposed defaults", x.err)
			return m.beginWizardWarmup()
		}
		m.wizardModel = x.model
		m.wizardEngine = wizard.NewEngine(m.wizardClient, m.wizardModel, m.wizardSession)
		m.workflow.Err = nil
		return m.beginWizardWarmup()
	case wizardReplyMsg:
		if x.generation != m.wizardGeneration || m.screen != setup.StateWizard {
			return m, nil
		}
		m.wizardBusy = false
		if x.err != nil {
			m.workflow.Err = x.err
			return m, nil
		}
		m.workflow.Err = nil
		m.wizardMessages = append(m.wizardMessages, "Wizard: "+x.reply.Content)
		m.wizardReady = x.reply.Ready
		if x.reply.Changed {
			return m.beginWizardWarmup()
		}
	case wizardWarmMsg:
		if x.generation != m.wizardWarmGeneration {
			return m, nil
		}
		m.wizardWarming = false
		m.wizardWarmCancel = nil
		if x.result.Err != nil && x.result.Err != context.Canceled {
			m.workflow.Err = fmt.Errorf("background model preparation failed: %w; the first prompt will retry", x.result.Err)
		}
	case wizardApplyMsg:
		if x.generation != m.wizardGeneration || m.screen != setup.StateWizard {
			return m, nil
		}
		m.wizardApplying = false
		if x.err != nil {
			m.workflow.Err = x.err
			return m, nil
		}
		cfg := x.config
		cfg.Topology.Endpoints = m.workflow.Draft.PersistenceEndpoints(m.config.Topology.Endpoints)
		if m.wizardCancel != nil {
			m.wizardCancel()
			m.wizardCancel = nil
		}
		m.wizardGeneration++
		m.wizardPreparing = false
		m.config = cfg
		m.setup = false
		m.screen = setup.StateReady
		m.workflow.State = setup.StateReady
		m.workflow.Err = nil
	case providerInstallEventMsg:
		if !m.providerInstalling || x.generation != m.providerInstallGen {
			return m, nil
		}
		if x.event.progress != nil {
			m.providerProgress = *x.event.progress
			return m, m.nextProviderInstallEvent(x.kind)
		}
		if !x.event.done {
			return m, m.nextProviderInstallEvent(x.kind)
		}
		m.providerInstalling = false
		m.providerInstallCh = nil
		if x.event.err != nil {
			m.workflow.Err = x.event.err
			m.providerNotice = ""
			return m, nil
		}
		platform := setup.CurrentPlatform()
		endpointID := setup.OllamaEndpointID
		if x.kind == localmodels.KindMLX {
			endpointID = setup.MLXEndpointID
		}
		m.workflow.Draft.SetProviderReady(endpointID, true)
		if err := m.workflow.Draft.SetProviderEnabled(endpointID, true, platform); err != nil {
			m.workflow.Err = err
			return m, nil
		}
		m.workflow.Err = nil
		m.providerNotice = "Provider installed. Kingdom is checking it now."
		return m.beginDiscovery()
	case localModelsMsg:
		if !m.localModels.open || x.generation != m.localModels.generation {
			return m, nil
		}
		m.localModels.loading = false
		m.localModels.runtimes = append([]localmodels.Runtime(nil), x.runtimes...)
		if len(m.localModels.runtimes) == 0 {
			m.localModels.runtimeCursor = 0
			m.localModels.modelCursor = 0
		} else if m.localModels.runtimeCursor >= len(m.localModels.runtimes) {
			m.localModels.runtimeCursor = len(m.localModels.runtimes) - 1
			m.localModels.modelCursor = 0
		}
	case localModelStartedMsg:
		if !m.localModels.open || x.generation != m.localModels.generation {
			return m, nil
		}
		m.localModels.starting = false
		if x.err != nil {
			m.localModels.err = x.err.Error()
			return m, nil
		}
		return m, m.inspectLocalModels()
	case SaveMsg:
		if !m.saving || m.screen != setup.StateReview || x.Generation != m.saveGen {
			return m, nil
		}
		m.saving = false
		if x.Err == nil {
			m.config = x.Config
			m.setup = false
			m.wizardManual = false
			m.screen = setup.StateReady
			m.workflow.State = setup.StateReady
			m.workflow.Err = nil
		} else if m.workflow != nil {
			m.workflow.Err = x.Err
		}
	case memorySessionsMsg:
		if !m.memory.open || x.generation != m.memory.generation {
			return m, nil
		}
		m.memory.loading = false
		m.memory.sessions = append([]memory.Session(nil), x.sessions...)
		m.memory.err = ""
		if x.err != nil {
			m.memory.err = x.err.Error()
			m.memory.sessions = nil
			return m, nil
		}
		if len(m.memory.sessions) == 0 {
			m.memory.cursor = 0
			m.memory.exchanges = nil
			return m, nil
		}
		if m.memory.cursor >= len(m.memory.sessions) {
			m.memory.cursor = len(m.memory.sessions) - 1
		}
		return m, m.loadSelectedMemory()
	case memoryExchangesMsg:
		if !m.memory.open || x.generation != m.memory.generation || m.memory.cursor >= len(m.memory.sessions) || m.memory.sessions[m.memory.cursor].ID != x.sessionID {
			return m, nil
		}
		m.memory.loading = false
		m.memory.err = ""
		m.memory.exchanges = append([]memory.Exchange(nil), x.exchanges...)
		if x.err != nil {
			m.memory.err = x.err.Error()
			m.memory.exchanges = nil
		}
	case memoryDeletedMsg:
		if !m.memory.open || x.generation != m.memory.generation {
			return m, nil
		}
		m.memory.loading = false
		if x.err != nil {
			m.memory.err = x.err.Error()
			return m, nil
		}
		if !x.deleted {
			m.memory.err = "session no longer exists"
			return m, nil
		}
		if x.sessionID == m.sessionID {
			sessionID, err := m.newSessionID()
			if err != nil {
				m.memory.err = "start replacement session: " + err.Error()
				return m, nil
			}
			m.sessionID = sessionID
			m.history = nil
			m.progress = "New session started"
		}
		return m, m.loadMemorySessions()
	case sessionCompactedMsg:
		if x.generation != m.compactGeneration {
			return m, nil
		}
		m.compacting = false
		m.compactCancel = nil
		if x.err != nil {
			m.chatError = x.err.Error()
			m.progress = ""
			return m, nil
		}
		m.chatError = ""
		m.progress = "Session compacted"
		if m.memory.open {
			return m, m.loadMemorySessions()
		}
	case chatEventMsg:
		if x.Generation != m.runGen || !m.running {
			return m, nil
		}
		if x.Event.Type == orchestration.EventCompleted {
			if x.Event.Result == nil {
				m.chatError = "orchestration completed without a result"
			} else {
				m.history = append(m.history, "King: "+x.Event.Result.Content)
			}
			m.running = false
			m.progress = ""
			m.runCancel = nil
			m.approval = nil
			return m, m.nextEvent()
		}
		if x.Event.Type == orchestration.EventFailed {
			m.chatError = x.Event.Message
			if m.chatError == "" && x.Event.Result != nil {
				m.chatError = x.Event.Result.Error
			}
			if m.chatError == "" {
				m.chatError = "orchestration failed"
			}
			m.running = false
			m.progress = ""
			m.runCancel = nil
			m.approval = nil
			return m, m.nextEvent()
		}
		switch x.Event.Type {
		case orchestration.EventModelActivity:
			if activity := x.Event.ModelActivity; activity != nil && activity.CompletionTokens > 0 && activity.GenerationDuration > 0 {
				key := modelMetricKey(activity.EndpointKind, activity.Model)
				metric := m.modelMetrics[key]
				metric.completionTokens += activity.CompletionTokens
				metric.generationDuration += activity.GenerationDuration
				m.modelMetrics[key] = metric
			}
		case orchestration.EventRuntimePreparing:
			m.progress = "Starting local model servers…"
		case orchestration.EventStarted:
			m.progress = "Started…"
		case orchestration.EventKingThinking:
			m.progress = "King is thinking…"
		case orchestration.EventWorkersRunning:
			m.progress = "Workers running…"
		case orchestration.EventCouncilReviewing:
			m.progress = "Council reviewing…"
		case orchestration.EventToolRunning:
			if x.Event.ToolCall != nil {
				m.progress = "King requested " + x.Event.ToolCall.Name + "…"
			}
		case orchestration.EventToolApproval:
			if x.Event.Approval == nil {
				m.chatError = "invalid tool approval request"
				if m.runCancel != nil {
					m.runCancel()
				}
				m.running = false
				return m, nil
			}
			m.approval = x.Event.Approval
			approval := x.Event.Approval.Approval()
			m.history = append(m.history, formatApproval(approval))
			m.progress = "Approval required: y approve, n deny, Esc cancel"
			return m, nil
		case orchestration.EventToolCompleted:
			if x.Event.ToolResult != nil {
				m.history = append(m.history, formatToolResult(*x.Event.ToolResult))
			}
			m.progress = "King is thinking…"
		case orchestration.EventMemoryRecall:
			m.progress = x.Event.Message
		case orchestration.EventMemoryWarning:
			m.history = append(m.history, "Memory warning: "+x.Event.Message)
		}
		return m, m.nextEvent()
	case tea.KeyPressMsg:
		if m.saving {
			return m, nil
		}
		key := x.String()
		if key == "ctrl+c" {
			if m.compactCancel != nil {
				m.compactCancel()
			}
			if m.localModels.cancel != nil {
				m.localModels.cancel()
			}
			if m.modelDownloadCancel != nil {
				m.modelDownloadCancel()
			}
			if m.modelRemoveCancel != nil {
				m.modelRemoveCancel()
			}
			if m.running && m.runCancel != nil {
				m.runCancel()
				m.running = false
				m.progress = "Cancelled"
				m.history = append(m.history, "Cancelled")
				m.approval = nil
			}
			if m.wizardCancel != nil {
				m.wizardCancel()
			}
			m.cancelWizardWarmup()
			return m, tea.Quit
		}
		if m.compacting {
			if key == "esc" && m.compactCancel != nil {
				m.compactCancel()
				m.compactGeneration++
				m.compacting = false
				m.compactCancel = nil
				m.progress = "Compaction cancelled"
			}
			return m, nil
		}
		if m.skills.open {
			return m.handleSkillsKey(key), nil
		}
		if m.memory.open {
			return m.handleMemoryKey(key)
		}
		if m.localModels.open {
			return m.handleLocalModelsKey(key)
		}
		if m.approval != nil {
			switch key {
			case "y", "n":
				approved := key == "y"
				if m.approval.Resolve(approved) {
					decision := "denied"
					if approved {
						decision = "approved"
					}
					m.history = append(m.history, "Tool "+decision)
				}
				m.approval = nil
				m.progress = "Running tool…"
				return m, m.nextEvent()
			case "esc":
				if m.runCancel != nil {
					m.runCancel()
				}
				m.running = false
				m.approval = nil
				m.progress = "Cancelled"
				m.history = append(m.history, "Cancelled")
				return m, nil
			default:
				return m, nil
			}
		}
		if m.formActive {
			if key == "q" {
				var cmd tea.Cmd
				m.form, cmd = m.form.Update(msg)
				return m, cmd
			}
			if key == "esc" {
				m.formActive = false
				return m, nil
			}
			if key == "enter" {
				ep, err := m.form.Endpoint()
				if err == nil {
					m.workflow.Draft.Config.Topology.Endpoints = setup.MergeCandidates(m.workflow.Draft.Config.Topology.Endpoints, []topology.Endpoint{ep})
					m.formActive = false
					return m.beginDiscovery()
				}
				m.form.Err = err
				return m, nil
			}
			var cmd tea.Cmd
			m.form, cmd = m.form.Update(msg)
			return m, cmd
		}
		if m.setup && m.screen == setup.StateModels && (m.modelSearchActive || m.modelDownloadConfirming || m.modelRemoveConfirming || m.modelRemoveActive) {
			return m.handleModelsKey(key)
		}
		if m.setup && m.screen == setup.StateWizard {
			return m.handleWizardKey(x, key)
		}
		if m.setup && key == "q" {
			return m, tea.Quit
		}
		if key == "ctrl+r" && !m.setup && !m.running && m.localModels.manager != nil {
			return m, m.openLocalModels()
		}
		if !m.setup && key == "ctrl+s" {
			if m.running {
				return m, nil
			}
			m = m.startSetup()
			return m.beginDiscovery()
		}
		if !m.setup {
			if key == "ctrl+m" {
				if !m.running && m.memory.store != nil {
					return m, m.openMemory()
				}
				return m, nil
			}
			if key == "ctrl+k" {
				if !m.running && m.skills.library != nil {
					m.openSkills()
				}
				return m, nil
			}
			if key == "esc" {
				if m.running && m.runCancel != nil {
					m.runCancel()
					m.running = false
					m.progress = "Cancelled"
					m.history = append(m.history, "Cancelled")
					m.approval = nil
				}
				return m, nil
			}
			if key == "ctrl+enter" {
				if m.running {
					return m, nil
				}
				p := strings.TrimSpace(m.chat.Value())
				if p == "" {
					return m, nil
				}
				switch strings.ToLower(p) {
				case "/setup", "/wizard":
					m.chat.SetValue("")
					return m.reopenWizard()
				case "/models":
					m.chat.SetValue("")
					return m.reopenModels()
				case "/sessions", "/memory":
					m.chat.SetValue("")
					if m.memory.store == nil {
						m.chatError = "memory is unavailable"
						return m, nil
					}
					return m, m.openMemory()
				case "/new":
					m.chat.SetValue("")
					return m.startNewSession()
				case "/compact":
					m.chat.SetValue("")
					return m.startSessionCompaction(m.sessionID)
				case "/skills":
					m.chat.SetValue("")
					if m.skills.library == nil {
						m.chatError = "skills are unavailable"
						return m, nil
					}
					m.openSkills()
					return m, nil
				}
				m.history = append(m.history, "You: "+p)
				m.chat.SetValue("")
				m.chatError = ""
				if m.run != nil {
					ctx, cancel := context.WithCancel(context.Background())
					m.runCancel = cancel
					m.running = true
					m.runGen++
					active := append([]skills.Skill(nil), m.skills.active...)
					m.runCh = m.startRunStream(ctx, m.config, m.sessionID, p, active)
					if m.runCh == nil {
						m.running = false
						m.runCancel = nil
						m.chat.SetValue(p)
						m.chatError = "orchestration stream unavailable"
						return m, nil
					}
					return m, m.nextEvent()
				}
				m.chatError = "orchestration unavailable"
				m.chat.SetValue(p)
				return m, nil
			}
			var cmd tea.Cmd
			m.chat, cmd = m.chat.Update(msg)
			return m, cmd
		}
		if key == "r" && m.screen == setup.StateProviders {
			m.localModels.preferred = nil
			return m.beginDiscovery()
		}
		if key == "r" && m.screen == setup.StateModels {
			m.modelRemoveNotice = ""
			return m.beginModelInventory()
		}
		if key == "a" && m.screen == setup.StateProviders {
			m.form = ui.NewCustomEndpointForm()
			m.formActive = true
			return m, nil
		}
		if key == "esc" {
			m.gate.Cancel()
			m.modelInventoryGen++
			m.modelInventoryLoading = false
			m.localModels.preferred = nil
			m.scanning = false
			if m.screen == setup.StateModels && m.modelsReturnToReady {
				m.cancelModelSearch()
				m.modelsReturnToReady = false
				m.setup = false
				m.screen = setup.StateReady
				m.workflow.State = setup.StateReady
				return m, nil
			}
			if m.screen == setup.StateRoles && m.wizardManual {
				return m.returnToWizardFromManual()
			}
			if m.screen == setup.StateReview || m.screen == setup.StateRoles || m.screen == setup.StatePerformance || m.screen == setup.StateModels || m.screen == setup.StateProviders {
				m.workflow.Back()
				m.screen = m.workflow.State
			}
			return m, nil
		}
		switch m.screen {
		case setup.StateProviders:
			return m.handleProvidersKey(key)
		case setup.StateModels:
			return m.handleModelsKey(key)
		case setup.StateRoles:
			if key == "x" {
				m.workflow.Err = m.workflow.Draft.SwapRoles("king", "worker")
			}
			if key == "0" {
				m.workflow.Draft.SetCouncilEnabled(false)
			}
			if key == "up" || key == "k" {
				if m.modelIndex > 0 {
					m.modelIndex--
				}
			}
			if key == "down" || key == "j" {
				m.modelIndex++
				if n := m.modelCount(); n > 0 && m.modelIndex >= n {
					m.modelIndex = n - 1
				}
			}
			if key == "1" {
				m.role = 0
			}
			if key == "2" {
				m.role = 1
			}
			if key == "3" {
				m.role = 2
			}
			if key == "enter" {
				m.assignCurrent()
			}
			if key == "n" {
				if err := m.workflow.Continue(); err == nil {
					m.screen = m.workflow.State
					m.workflow.Err = nil
				} else {
					m.workflow.Err = err
				}
			}
		case setup.StatePerformance:
			if key == "up" || key == "k" {
				m.perfFocus = (m.perfFocus - 1 + m.performanceControlCount()) % m.performanceControlCount()
			}
			if key == "down" || key == "j" {
				m.perfFocus = (m.perfFocus + 1) % m.performanceControlCount()
			}
			if key == "left" || key == "h" {
				m.adjustPerformance(-1)
			}
			if key == "right" || key == "l" {
				m.adjustPerformance(1)
			}
			if (key == " " || key == "space") && m.perfFocus == 2 && config.UsesManagedOllama(m.workflow.Draft.Config) {
				if m.workflow.Draft.Config.Providers.Ollama.PortMode == config.OllamaDedicatedPorts {
					m.workflow.Draft.Config.Providers.Ollama.PortMode = config.OllamaSharedPort
				} else {
					m.workflow.Draft.Config.Providers.Ollama.PortMode = config.OllamaDedicatedPorts
				}
				m.workflow.Err = nil
			}
			if key == "enter" {
				if err := m.workflow.Continue(); err != nil {
					m.workflow.Err = err
				} else {
					m.workflow.Err = nil
					m.screen = m.workflow.State
					if m.wizardManual && m.screen == setup.StateReview {
						return m.beginWizardWarmup()
					}
				}
			}
		case setup.StateReview:
			if key == "enter" {
				if m.modelDownloadActive {
					m.workflow.Err = fmt.Errorf("model downloads are still in progress")
					break
				}
				if m.modelDownloadError != "" {
					m.workflow.Err = fmt.Errorf("model download failed: %s", m.modelDownloadError)
					break
				}
				cfg := m.workflow.Draft.Config
				cfg.Topology.Endpoints = m.workflow.Draft.PersistenceEndpoints(m.config.Topology.Endpoints)
				if m.save == nil {
					m.config = cfg
					m.setup = false
					m.wizardManual = false
					m.screen = setup.StateReady
					m.workflow.State = setup.StateReady
					break
				}
				m.saveGen++
				m.saving = true
				gen, snap := m.saveGen, cfg
				return m, func() tea.Msg { return SaveMsg{Generation: gen, Config: snap, Err: m.save(snap)} }
			}
		}
	}
	return m, nil
}

func (m Model) inspectProviderRuntimes() tea.Cmd {
	manager := m.localModels.manager
	return func() tea.Msg {
		return providerRuntimesMsg{runtimes: manager.Inspect(context.Background())}
	}
}

func formatApproval(approval tools.Approval) string {
	return fmt.Sprintf("Tool request: %s | target: %s | risk: %s | args: %s", approval.Call.Name, approval.Summary, approval.Risk, string(approval.Call.Arguments))
}

func formatToolResult(result tools.Result) string {
	status := result.Output
	if result.Error != "" {
		status = "error: " + result.Error
	}
	return fmt.Sprintf("Tool result: %s | %s", result.Name, status)
}

func (m *Model) modelCount() int {
	if m.workflow == nil {
		return 0
	}
	return len(m.workflow.Draft.SelectedModels())
}

func (m *Model) adjustPerformance(delta int) {
	if m.perfFocus == 0 {
		m.workflow.Draft.Config.CouncilSize = max(1, min(9, m.workflow.Draft.Config.CouncilSize+delta))
	} else if m.perfFocus == 1 {
		m.workflow.Draft.Config.WorkerConcurrency = max(1, min(32, m.workflow.Draft.Config.WorkerConcurrency+delta))
	}
}

func (m Model) performanceControlCount() int {
	if m.workflow != nil && config.UsesManagedOllama(m.workflow.Draft.Config) {
		return 3
	}
	return 2
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Model) assignCurrent() {
	if m.workflow == nil {
		return
	}
	selected := m.workflow.Draft.SelectedModels()
	if m.modelIndex < 0 || m.modelIndex >= len(selected) {
		return
	}
	chosen := selected[m.modelIndex].Ref.Assignment()
	switch m.role {
	case 0:
		m.workflow.Draft.AssignKing(chosen)
	case 1:
		m.workflow.Draft.AssignWorker(chosen)
	default:
		m.workflow.Draft.AssignCouncil(chosen)
	}
}

func (m Model) View() tea.View {
	if m.localModels.open {
		return m.localModelsView()
	}
	if m.memory.open {
		return m.memoryView()
	}
	if m.skills.open {
		return m.skillsView()
	}
	if !m.setup {
		return ui.ChatViewWithPresentation(m.width, m.height, ui.ChatPresentation{
			History:  m.history,
			Progress: m.progress,
			Error:    m.chatError,
			Input:    m.chat,
			Running:  m.running,
			Models:   m.chatModelActivity(),
		})
	}
	return ui.ViewWithPresentation(m.width, m.height, m.setup, m.workflow, m.presentation())
}

func (m Model) presentation() ui.Presentation {
	return ui.Presentation{
		ModelIndex:              m.modelIndex,
		ModelCursor:             m.modelCursor,
		Role:                    m.role,
		ProviderCursor:          m.providerCursor,
		PerfFocus:               m.perfFocus,
		Form:                    &m.form,
		PreviousEndpoints:       m.config.Topology.Endpoints,
		FormActive:              m.formActive,
		Scanning:                m.scanning,
		ModelInventoryLoading:   m.modelInventoryLoading,
		ModelQuery:              m.modelQuery,
		ModelSearchActive:       m.modelSearchActive,
		ModelSearching:          m.modelSearching,
		ModelSearchWarning:      m.modelSearchWarning,
		ModelDownloadConfirming: m.modelDownloadConfirming,
		ModelDownloadActive:     m.modelDownloadActive,
		ModelDownloadProgress:   m.modelDownloadProgress,
		ModelDownloadPosition:   m.modelDownloadPosition,
		ModelDownloadCount:      m.modelDownloadCount,
		ModelDownloadError:      m.modelDownloadError,
		ModelRemoveConfirming:   m.modelRemoveConfirming,
		ModelRemoveActive:       m.modelRemoveActive,
		ModelRemoveTarget:       m.modelRemoveTarget,
		ModelRemoveNotice:       m.modelRemoveNotice,
		Saving:                  m.saving,
		ProviderConfirming:      m.providerConfirming,
		ProviderInstalling:      m.providerInstalling,
		ProviderNotice:          m.providerNotice,
		ProviderProgress:        m.providerProgress,
		WizardModel:             wizardModelLabel(m.wizardModel),
		WizardMessages:          append([]string(nil), m.wizardMessages...),
		WizardInput:             m.wizardInput.Value(),
		WizardBusy:              m.wizardBusy,
		WizardReady:             m.wizardReady,
		WizardApplying:          m.wizardApplying,
		WizardPreparing:         m.wizardPreparing,
		WizardWarming:           m.wizardWarming,
		ManualSetup:             m.wizardManual,
	}
}
