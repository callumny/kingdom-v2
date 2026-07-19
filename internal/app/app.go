package app

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/config"
	"github.com/callumny/kingdom/internal/discovery"
	"github.com/callumny/kingdom/internal/memory"
	"github.com/callumny/kingdom/internal/orchestration"
	"github.com/callumny/kingdom/internal/setup"
	"github.com/callumny/kingdom/internal/skills"
	"github.com/callumny/kingdom/internal/tools"
	"github.com/callumny/kingdom/internal/topology"
	"github.com/callumny/kingdom/internal/ui"
)

type Model struct {
	width, height int
	config        config.Config
	setup         bool
	workflow      *setup.Workflow
	defaults      []topology.Endpoint
	discover      func(context.Context, uint64, []topology.Endpoint) tea.Cmd
	save          func(config.Config) error
	screen        setup.WorkflowState
	role          int
	modelIndex    int
	gate          *setup.GenerationGate
	form          ui.CustomEndpointForm
	formActive    bool
	chat          ui.ChatInput
	history       []string
	progress      string
	chatError     string
	run           RunFunc
	runCancel     context.CancelFunc
	runCh         <-chan orchestration.Event
	runGen        uint64
	running       bool
	approval      *orchestration.ApprovalRequest
	skills        skillState
	memory        memoryState
	perfFocus     int
	scanning      bool
	saveGen       uint64
	saving        bool
}
type DiscoverFunc func(context.Context, uint64, []topology.Endpoint) tea.Cmd
type RunFunc func(context.Context, config.Config, string, []skills.Skill) <-chan orchestration.Event

type Services struct {
	Defaults []topology.Endpoint
	Discover DiscoverFunc
	Save     func(config.Config) error
	Run      RunFunc
	Skills   SkillLibrary
	Memory   MemoryBrowser
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
	model := Model{
		config:   c,
		setup:    c.RequiresSetup(),
		defaults: services.Defaults,
		discover: services.Discover,
		save:     services.Save,
		run:      services.Run,
		skills:   skillState{library: services.Skills},
		memory:   memoryState{store: services.Memory},
		workflow: w,
		screen:   w.State,
		gate:     &setup.GenerationGate{},
		chat:     ui.NewChatInput(),
	}
	// An incomplete config starts in discovery; show scanning immediately when
	// an automatic discovery dependency is available (before Init runs).
	if model.setup && model.screen == setup.StateDiscovery && services.Discover != nil {
		model.scanning = true
	}
	return model
}
func (m Model) RequiresSetup() bool       { return m.setup }
func (m Model) Workflow() *setup.Workflow { return m.workflow }
func (m Model) Init() tea.Cmd {
	if m.setup && m.screen == setup.StateDiscovery {
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
	m.modelIndex = 0
	cands := setup.MergeCandidates(m.defaults, m.workflow.Draft.Config.Topology.Endpoints)
	return m, m.discover(ctx, gen, cands)
}

func (m Model) startSetup() Model {
	m.gate.Cancel()
	m.saveGen++
	m.setup = true
	m.workflow = setup.Start(m.config, m.defaults)
	m.workflow.State = setup.StateDiscovery
	m.screen = setup.StateDiscovery
	m.modelIndex, m.role, m.perfFocus = 0, 0, 0
	m.form = ui.CustomEndpointForm{}
	m.formActive, m.saving = false, false
	m.scanning = m.discover != nil
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = x.Width, x.Height
	case DiscoveryMsg:
		if m.workflow != nil && m.screen == setup.StateDiscovery && m.gate.Accept(x.Generation) {
			m.workflow.Draft.ApplyResults(x.Results)
			if n := m.modelCount(); n == 0 {
				m.modelIndex = 0
			} else if m.modelIndex >= n {
				m.modelIndex = n - 1
			}
			m.scanning = false
		}
	case SaveMsg:
		if !m.saving || m.screen != setup.StateReview || x.Generation != m.saveGen {
			return m, nil
		}
		m.saving = false
		if x.Err == nil {
			m.config = x.Config
			m.setup = false
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
		return m, m.loadMemorySessions()
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
			if m.running && m.runCancel != nil {
				m.runCancel()
				m.running = false
				m.progress = "Cancelled"
				m.history = append(m.history, "Cancelled")
				m.approval = nil
			}
			return m, tea.Quit
		}
		if m.skills.open {
			return m.handleSkillsKey(key), nil
		}
		if m.memory.open {
			return m.handleMemoryKey(key)
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
		if m.setup && key == "q" {
			return m, tea.Quit
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
				m.history = append(m.history, "You: "+p)
				m.chat.SetValue("")
				m.chatError = ""
				if m.run != nil {
					ctx, cancel := context.WithCancel(context.Background())
					m.runCancel = cancel
					m.running = true
					m.runGen++
					active := append([]skills.Skill(nil), m.skills.active...)
					m.runCh = m.run(ctx, m.config, p, active)
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
		if key == "r" && m.screen == setup.StateDiscovery {
			return m.beginDiscovery()
		}
		if key == "a" && m.screen == setup.StateDiscovery {
			m.form = ui.NewCustomEndpointForm()
			m.formActive = true
			return m, nil
		}
		if key == "esc" {
			m.gate.Cancel()
			m.scanning = false
			if m.screen == setup.StateReview || m.screen == setup.StateRoles || m.screen == setup.StatePerformance {
				m.workflow.Back()
				m.screen = m.workflow.State
			}
			return m, nil
		}
		switch m.screen {
		case setup.StateDiscovery:
			if key == "enter" {
				if m.scanning {
					return m, nil
				}
				if err := m.workflow.Continue(); err == nil {
					m.gate.Cancel()
					m.screen = m.workflow.State
				}
			}
		case setup.StateRoles:
			if key == "0" {
				m.workflow.Draft.UseKingForCouncil(true)
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
			if key == "n" && m.workflow.Draft.Config.Topology.Roles.King.Complete() && m.workflow.Draft.Config.Topology.Roles.Worker.Complete() {
				if err := m.workflow.Continue(); err == nil {
					m.screen = m.workflow.State
				}
			}
		case setup.StatePerformance:
			if key == "up" || key == "k" {
				m.perfFocus = (m.perfFocus + 1) % 2
			}
			if key == "down" || key == "j" {
				m.perfFocus = (m.perfFocus + 1) % 2
			}
			if key == "left" || key == "h" {
				m.adjustPerformance(-1)
			}
			if key == "right" || key == "l" {
				m.adjustPerformance(1)
			}
			if key == "enter" {
				_ = m.workflow.Continue()
				m.screen = m.workflow.State
			}
		case setup.StateReview:
			if key == "enter" {
				cfg := m.workflow.Draft.Config
				cfg.Topology.Endpoints = m.workflow.Draft.PersistenceEndpoints(m.config.Topology.Endpoints)
				if m.save == nil {
					m.config = cfg
					m.setup = false
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
	n := 0
	if m.workflow == nil {
		return n
	}
	for _, r := range m.workflow.Draft.Results {
		n += len(r.Models)
	}
	return n
}

func (m *Model) adjustPerformance(delta int) {
	if m.perfFocus == 0 {
		m.workflow.Draft.Config.CouncilSize = max(1, min(9, m.workflow.Draft.Config.CouncilSize+delta))
	} else {
		m.workflow.Draft.Config.WorkerConcurrency = max(1, min(32, m.workflow.Draft.Config.WorkerConcurrency+delta))
	}
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
	var chosen topology.Assignment
	idx := m.modelIndex
	for _, r := range m.workflow.Draft.Results {
		for _, model := range r.Models {
			if idx == 0 {
				chosen = topology.Assignment{EndpointID: r.Endpoint.ID, Model: model.ID}
				break
			}
			idx--
		}
		if chosen.Complete() {
			break
		}
	}
	if !chosen.Complete() {
		return
	}
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
	if m.memory.open {
		return m.memoryView()
	}
	if m.skills.open {
		return m.skillsView()
	}
	if !m.setup {
		return ui.ChatView(m.width, m.height, m.history, m.progress, m.chatError, m.chat, m.running)
	}
	return ui.ViewWithPresentation(m.width, m.height, m.setup, m.workflow, ui.Presentation{ModelIndex: m.modelIndex, Role: m.role, PerfFocus: m.perfFocus, Form: &m.form, PreviousEndpoints: m.config.Topology.Endpoints, FormActive: m.formActive, Scanning: m.scanning, Saving: m.saving})
}
