package localmodels

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const maxCommandOutput = 2 << 20

type OSSystem struct{}

func (OSSystem) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (OSSystem) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	buffer := &boundedBuffer{remaining: maxCommandOutput}
	command.Stdout = buffer
	command.Stderr = buffer
	if err := command.Run(); err != nil {
		return buffer.Bytes(), fmt.Errorf("%s: %w", name, err)
	}
	return buffer.Bytes(), nil
}

func (OSSystem) Start(name string, args, environment []string) error {
	command := exec.Command(name, args...)
	command.Env = mergedEnvironment(os.Environ(), environment)
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = detachedSysProcAttr()
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	remaining int
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if len(value) > b.remaining {
		value = value[:b.remaining]
	}
	_, _ = b.buffer.Write(value)
	b.remaining -= len(value)
	return original, nil
}

func (b *boundedBuffer) Bytes() []byte { return append([]byte(nil), b.buffer.Bytes()...) }

func mergedEnvironment(base, overrides []string) []string {
	keys := make(map[string]bool, len(overrides))
	for _, value := range overrides {
		if index := strings.IndexByte(value, '='); index > 0 {
			keys[value[:index]] = true
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, value := range base {
		index := strings.IndexByte(value, '=')
		if index > 0 && keys[value[:index]] {
			continue
		}
		result = append(result, value)
	}
	return append(result, overrides...)
}
