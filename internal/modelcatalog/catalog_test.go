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

func TestMergeUsesProviderIdentityAndLimitsEachProvider(t *testing.T) {
	remote := []Model{{Provider: Ollama, ID: "same"}, {Provider: MLX, ID: "same"}, {Provider: Ollama, ID: "something"}}
	got := MergeAndFilter(nil, remote, "s", 1)
	if len(got) != 2 || got[0].Provider == got[1].Provider {
		t.Fatalf("models=%+v", got)
	}
}

func TestMergeKeepsEveryInstalledMatchAndLimitsRemoteMatchesPerProvider(t *testing.T) {
	installed := []Model{
		{Provider: Ollama, ID: "qwen-installed-1", Installed: true},
		{Provider: Ollama, ID: "qwen-installed-2", Installed: true},
		{Provider: MLX, ID: "qwen-installed-3", Installed: true},
	}
	remote := []Model{
		{Provider: Ollama, ID: "qwen-remote-1"},
		{Provider: MLX, ID: "qwen-remote-2"},
		{Provider: Ollama, ID: "qwen-remote-3"},
	}

	got := MergeAndFilter(installed, remote, "qwen", 2)
	if len(got) != 6 {
		t.Fatalf("models=%+v, want 3 installed plus 3 provider-limited remote", got)
	}
	for index := 0; index < 3; index++ {
		if !got[index].Installed {
			t.Fatalf("installed model ranked after remote result: %+v", got)
		}
	}
}
