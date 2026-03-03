package main

import (
	"context"
	"os"

	"github.com/tinytelemetry/lotus/internal/logsource"
)

// NamedLogSource aliases the shared source abstraction to keep app-layer APIs explicit.
type NamedLogSource = logsource.LogSource

// InputSourcePlugin is a small plugin primitive for wiring log inputs.
type InputSourcePlugin interface {
	Name() string
	Enabled() bool
	Build(ctx context.Context) (NamedLogSource, error)
}

func buildInputPlugins() []InputSourcePlugin {
	return []InputSourcePlugin{stdinInputPlugin{}}
}

type stdinInputPlugin struct{}

func (p stdinInputPlugin) Name() string { return "stdin" }

func (p stdinInputPlugin) Enabled() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func (p stdinInputPlugin) Build(ctx context.Context) (NamedLogSource, error) {
	return logsource.NewStdinSource(ctx), nil
}
