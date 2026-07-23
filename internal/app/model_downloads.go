package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/callumny/kingdom/internal/localmodels"
	"github.com/callumny/kingdom/internal/setup"
)

func (m Model) beginModelDownloads() (tea.Model, tea.Cmd) {
	pending := m.workflow.Draft.PendingDownloads()
	if len(pending) == 0 {
		return m.advanceFromModels()
	}
	if m.modelDownloader == nil {
		m.workflow.Err = fmt.Errorf("model downloader is unavailable")
		return m, nil
	}
	m.modelDownloadGen++
	m.modelDownloadActive = true
	m.modelDownloadError = ""
	firstKind, _ := localKindForEndpoint(pending[0].Ref.EndpointID)
	m.modelDownloadProgress = localmodels.DownloadProgress{
		Provider:   firstKind,
		Model:      pending[0].Ref.ModelID,
		Status:     "Preparing model download",
		TotalBytes: pending[0].SizeBytes,
	}
	m.modelDownloadPosition = 1
	m.modelDownloadCount = len(pending)
	generation := m.modelDownloadGen
	ctx, cancel := context.WithCancel(context.Background())
	m.modelDownloadCancel = cancel
	channel := make(chan modelDownloadEvent, 64)
	m.modelDownloadCh = channel
	downloader := m.modelDownloader
	go func() {
		defer close(channel)
		emit := func(event modelDownloadEvent) bool {
			select {
			case channel <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for index, option := range pending {
			kind, ok := localKindForEndpoint(option.Ref.EndpointID)
			if !ok {
				emit(modelDownloadEvent{done: true, err: fmt.Errorf("unsupported model provider %q", option.Ref.EndpointID)})
				return
			}
			request := localmodels.DownloadRequest{Kind: kind, Model: option.Ref.ModelID, BaseURL: option.Endpoint.BaseURL, SizeBytes: option.SizeBytes}
			err := downloader.Download(ctx, request, func(progress localmodels.DownloadProgress) {
				value := progress
				value.Provider = kind
				emit(modelDownloadEvent{progress: &value, position: index + 1, count: len(pending)})
			})
			if err != nil {
				emit(modelDownloadEvent{done: true, err: fmt.Errorf("download %s: %w", option.Ref.ModelID, err)})
				return
			}
			ref := option.Ref
			if !emit(modelDownloadEvent{installed: &ref}) {
				return
			}
		}
		emit(modelDownloadEvent{done: true})
	}()
	return m, m.nextModelDownloadEventWithGeneration(generation)
}

func (m Model) nextModelDownloadEvent() tea.Cmd {
	return m.nextModelDownloadEventWithGeneration(m.modelDownloadGen)
}

func (m Model) nextModelDownloadEventWithGeneration(generation uint64) tea.Cmd {
	channel := m.modelDownloadCh
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			event = modelDownloadEvent{done: true}
		}
		return modelDownloadEventMsg{generation: generation, event: event}
	}
}

func localKindForEndpoint(endpointID string) (localmodels.Kind, bool) {
	switch endpointID {
	case setup.OllamaEndpointID:
		return localmodels.KindOllama, true
	case setup.MLXEndpointID:
		return localmodels.KindMLX, true
	default:
		return "", false
	}
}
