package modelcatalog

import (
	"sort"
	"strings"
)

type Provider string

const (
	Ollama Provider = "ollama"
	MLX    Provider = "mlx"
)

type Identity struct {
	Provider Provider
	ID       string
}

type Model struct {
	Provider       Provider
	ID             string
	Installed      bool
	PopularityRank int
	Downloads      int64
	ParameterSize  string
	SizeBytes      int64
}

func (m Model) Identity() Identity { return Identity{Provider: m.Provider, ID: m.ID} }

func MergeAndFilter(installed, remote []Model, query string, limit int) []Model {
	query = strings.ToLower(strings.TrimSpace(query))
	byIdentity := make(map[Identity]Model, len(installed)+len(remote))
	for _, group := range [][]Model{remote, installed} {
		for _, model := range group {
			model.ID = strings.TrimSpace(model.ID)
			if model.ID == "" || (model.Provider != Ollama && model.Provider != MLX) || !fuzzyMatch(strings.ToLower(model.ID), query) {
				continue
			}
			identity := model.Identity()
			if current, exists := byIdentity[identity]; !exists || model.Installed || !current.Installed {
				byIdentity[identity] = model
			}
		}
	}
	models := make([]Model, 0, len(byIdentity))
	for _, model := range byIdentity {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Installed != models[j].Installed {
			return models[i].Installed
		}
		if !models[i].Installed {
			leftPopular := models[i].PopularityRank > 0
			rightPopular := models[j].PopularityRank > 0
			if leftPopular != rightPopular {
				return leftPopular
			}
			if leftPopular && rightPopular {
				if models[i].Provider != models[j].Provider {
					return models[i].Provider == Ollama
				}
				if models[i].PopularityRank != models[j].PopularityRank {
					return models[i].PopularityRank < models[j].PopularityRank
				}
			}
		}
		left, right := strings.ToLower(models[i].ID), strings.ToLower(models[j].ID)
		leftPrefix, rightPrefix := strings.HasPrefix(left, query), strings.HasPrefix(right, query)
		if leftPrefix != rightPrefix {
			return leftPrefix
		}
		if left != right {
			return left < right
		}
		return models[i].Provider < models[j].Provider
	})
	if limit > 0 {
		limited := make([]Model, 0, len(models))
		remoteCount := make(map[Provider]int, 2)
		for _, model := range models {
			if !model.Installed {
				if remoteCount[model.Provider] >= limit {
					continue
				}
				remoteCount[model.Provider]++
			}
			limited = append(limited, model)
		}
		models = limited
	}
	return models
}

func fuzzyMatch(value, query string) bool {
	if query == "" {
		return true
	}
	index := 0
	for _, character := range value {
		if character == rune(query[index]) {
			index++
			if index == len(query) {
				return true
			}
		}
	}
	return false
}
