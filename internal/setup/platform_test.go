package setup

import "testing"

func TestPlatformProviderSupport(t *testing.T) {
	tests := []struct {
		name        string
		platform    Platform
		ollama, mlx bool
	}{
		{name: "Apple silicon", platform: Platform{OS: "darwin", Arch: "arm64"}, ollama: true, mlx: true},
		{name: "Intel Mac", platform: Platform{OS: "darwin", Arch: "amd64"}, ollama: true, mlx: false},
		{name: "Linux ARM", platform: Platform{OS: "linux", Arch: "arm64"}, ollama: true, mlx: false},
		{name: "Linux x86", platform: Platform{OS: "linux", Arch: "amd64"}, ollama: true, mlx: false},
		{name: "Windows deferred", platform: Platform{OS: "windows", Arch: "amd64"}, ollama: false, mlx: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.platform.SupportsOllama(); got != test.ollama {
				t.Fatalf("SupportsOllama()=%v, want %v", got, test.ollama)
			}
			if got := test.platform.SupportsMLX(); got != test.mlx {
				t.Fatalf("SupportsMLX()=%v, want %v", got, test.mlx)
			}
		})
	}
}
