package modelcatalog

import "testing"

func TestMergeRanksInstalledModelsBeforeFuzzyRemoteMatches(t *testing.T) {
	installed := []Model{{Provider: Ollama, ID: "qwen3:8b", Installed: true}, {Provider: MLX, ID: "mlx-community/Qwen3-4B", Installed: true}}
	remote := []Model{{Provider: Ollama, ID: "qwen3:14b"}, {Provider: MLX, ID: "mlx-community/Qwen3-8B"}, {Provider: Ollama, ID: "llama3"}, {Provider: Ollama, ID: "qwen3:8b"}}
	got := MergeAndFilter(installed, remote, "qwn3", 10)
	if len(got) != 4 {
		t.Fatalf("models=%+v", got)
	}
	if !got[0].Installed || !got[1].Installed {
		t.Fatalf("installed models were not first: %+v", got)
	}
	seen := map[Identity]bool{}
	for _, model := range got {
		identity := model.Identity()
		if seen[identity] {
			t.Fatalf("duplicate model: %+v", identity)
		}
		seen[identity] = true
	}
}

func TestMergeUsesProviderAsPartOfIdentityAndHonoursLimit(t *testing.T) {
	remote := []Model{{Provider: Ollama, ID: "same"}, {Provider: MLX, ID: "same"}, {Provider: Ollama, ID: "something"}}
	got := MergeAndFilter(nil, remote, "s", 2)
	if len(got) != 2 || got[0].Provider == got[1].Provider {
		t.Fatalf("models=%+v", got)
	}
}
