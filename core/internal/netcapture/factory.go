package netcapture

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"dev.atomyx/internal/runner"
)

// Config selects and configures a runner.NetworkCapture adapter. Type
// dispatches through the adapter registry, and the built-in types are
// "none" (no capture), "file" (read Path as a JSON-lines capture
// file), and "mitmproxy" (also Path, semantically distinguishing
// that a mitmproxy addon is expected to be writing it, served by the
// same FileCapture). Host and Port are reserved for a future adapter
// type that manages its own proxy listener, so the built-in types
// ignore them.
type Config struct {
	Type string
	Path string
	Host string
	Port int
}

// Factory constructs a runner.NetworkCapture from a Config already
// known to carry this factory's Type.
type Factory func(Config) (runner.NetworkCapture, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{
		"none": func(Config) (runner.NetworkCapture, error) {
			return NullCapture{}, nil
		},
		"file": func(cfg Config) (runner.NetworkCapture, error) {
			if cfg.Path == "" {
				return nil, fmt.Errorf(`capture config type "file" requires a "path" to the JSON-lines capture file`)
			}
			return NewFileCapture(cfg.Path), nil
		},
		"mitmproxy": func(cfg Config) (runner.NetworkCapture, error) {
			if cfg.Path == "" {
				return nil, fmt.Errorf(`capture config type "mitmproxy" requires a "path" to the mitmproxy addon's JSON-lines output file`)
			}
			return NewFileCapture(cfg.Path), nil
		},
	}
)

// Register adds or replaces the factory for an adapter type. Call it
// before constructing a ScriptRunner if a host needs a proxy adapter
// type beyond the built-ins.
func Register(adapterType string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[adapterType] = factory
}

// New constructs a runner.NetworkCapture from config, dispatching on
// config.Type through the adapter registry. A nil config, or a
// config with Type "none", returns NullCapture.
func New(config *Config) (runner.NetworkCapture, error) {
	if config == nil || config.Type == "none" {
		return NullCapture{}, nil
	}

	registryMu.RLock()
	factory, ok := registry[config.Type]
	registryMu.RUnlock()
	if !ok {
		registryMu.RLock()
		types := make([]string, 0, len(registry))
		for t := range registry {
			types = append(types, t)
		}
		registryMu.RUnlock()
		sort.Strings(types)
		return nil, fmt.Errorf(
			"unknown capture adapter type %q; available: %s",
			config.Type, strings.Join(types, ", "))
	}
	return factory(*config)
}
