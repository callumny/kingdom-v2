package setup

import "runtime"

// Platform records only the host facts that affect provider support. Keeping
// this value explicit makes capability checks deterministic and testable.
type Platform struct {
	OS   string
	Arch string
}

func CurrentPlatform() Platform { return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH} }

func (p Platform) SupportsOllama() bool { return p.OS == "darwin" || p.OS == "linux" }

func (p Platform) SupportsMLX() bool { return p.OS == "darwin" && p.Arch == "arm64" }
