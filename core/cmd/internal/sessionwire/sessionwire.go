// Package sessionwire builds the session dependencies and timeouts the
// CLI and the MCP server share, so both composition roots arbitrate
// devices through the same lease directory and identity rules.
package sessionwire

import (
	"fmt"
	"os"
	"strconv"

	"dev.atomyx/internal/clock"
	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/lease"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/session"
)

const (
	// McpIdleEnvVar overrides session.mcpIdleTimeoutMs.
	McpIdleEnvVar = "ATOMYX_MCP_IDLE_TIMEOUT_MS"
	// DeviceAcquireEnvVar overrides session.deviceAcquireTimeoutMs.
	DeviceAcquireEnvVar = "ATOMYX_DEVICE_ACQUIRE_TIMEOUT_MS"
)

// Deps returns the session dependencies for a process that runs command
// in role, such as "atomyx-mcp" in "mcp". An empty command lets each CLI
// command name itself, for example "atomyx run". It fails when the lease
// directory cannot be created safely, because a host that cannot
// arbitrate devices must not drive them.
func Deps(runner process.Runner, clk clock.Clock, logger logging.Logger, command, role string) (session.Deps, error) {
	leases, err := lease.NewManager(lease.DefaultEnvironment(), clk)
	if err != nil {
		return session.Deps{}, fmt.Errorf("device lease directory: %w", err)
	}
	return session.Deps{
		Identity: device.NewIdentityResolver(runner),
		Owners:   device.NewOwnerResolver(process.SystemInspector{}),
		Leases:   leases,
		Sockets:  leases,
		Clock:    clk,
		Logger:   logger,
		Command:  command,
		Role:     role,
	}, nil
}

// ResolveTimeouts returns the session timeouts for cfg, which may be
// nil. An environment variable beats the config file, which beats the
// default. The open-run idle timeout has no environment variable. A
// malformed or non-positive environment value is reported through
// logger and ignored.
func ResolveTimeouts(cfg *config.AtomyxConfig, getenv func(string) string, logger logging.Logger) session.Timeouts {
	t := session.DefaultTimeouts()
	if cfg != nil && cfg.Session != nil {
		fromConfig(&t.McpIdleMs, cfg.Session.McpIdleTimeoutMs)
		fromConfig(&t.OpenRunIdleMs, cfg.Session.OpenRunIdleTimeoutMs)
		fromConfig(&t.DeviceAcquireMs, cfg.Session.DeviceAcquireTimeoutMs)
		fromConfig(&t.DebugPauseMs, cfg.Session.DebugPauseTimeoutMs)
	}
	fromEnv(&t.McpIdleMs, McpIdleEnvVar, getenv, logger)
	fromEnv(&t.DeviceAcquireMs, DeviceAcquireEnvVar, getenv, logger)
	return t
}

func fromConfig(dst *int64, v *int64) {
	if v != nil {
		*dst = *v
	}
}

func fromEnv(dst *int64, name string, getenv func(string) string, logger logging.Logger) {
	raw := getenv(name)
	if raw == "" {
		return
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		logger.Warn("ignoring an invalid timeout environment variable", logging.LogContext{"name": name})
		return
	}
	*dst = n
}

// LoadConfigBestEffort reads the project config from the working
// directory for the settings a host applies without a command line. A
// missing config is normal,
// and an unreadable one is logged and ignored, because a host must
// start with its defaults rather than fail on a config it was never
// asked to read.
func LoadConfigBestEffort(logger logging.Logger) *config.AtomyxConfig {
	loaded := LoadProjectBestEffort(logger)
	if loaded == nil {
		return nil
	}
	return &loaded.Config
}

// LoadProjectBestEffort is LoadConfigBestEffort keeping the path the
// config was read from, for a caller that resolves paths the config
// names.
func LoadProjectBestEffort(logger logging.Logger) *config.LoadedConfig {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	loaded, err := config.Load(config.LoadOptions{Cwd: cwd})
	if err != nil {
		logger.Warn("ignoring the unreadable project config", logging.LogContext{"error": err.Error()})
		return nil
	}
	return loaded
}
