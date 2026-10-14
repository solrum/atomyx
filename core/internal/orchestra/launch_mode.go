package orchestra

import (
	"context"
	"fmt"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

// LaunchMode selects how LaunchAppWithMode starts an app.
type LaunchMode string

const (
	// LaunchModeDefault starts the app without promising anything about
	// process state. A repeat launch of the same app in one flow is
	// skipped.
	LaunchModeDefault LaunchMode = "default"
	// LaunchModeFresh force-stops the app and launches it again. The
	// app's data is kept. It bypasses the repeat-launch skip.
	LaunchModeFresh LaunchMode = "fresh"
	// LaunchModeAttach reuses a running instance of the app. A repeat
	// launch of the same app in one flow is skipped.
	LaunchModeAttach LaunchMode = "attach"
)

// LaunchModeFromNoReset maps a script-level noReset value (nil for
// unset) to its launch mode.
func LaunchModeFromNoReset(noReset *bool) LaunchMode {
	switch {
	case noReset == nil:
		return LaunchModeDefault
	case *noReset:
		return LaunchModeAttach
	default:
		return LaunchModeFresh
	}
}

// LaunchAppWithMode launches appID using mode and the dedup rule
// documented on LaunchApp. An unknown mode is rejected before any device
// call with an error coded diag.CodeInvalidArgument.
func (o *Orchestra) LaunchAppWithMode(ctx context.Context, appID string, mode LaunchMode) error {
	var args driver.LaunchArgs
	switch mode {
	case LaunchModeDefault:
	case LaunchModeFresh:
		fresh := false
		args.NoReset = &fresh
	case LaunchModeAttach:
		attach := true
		args.NoReset = &attach
	default:
		return diag.New(
			diag.CodeInvalidArgument,
			fmt.Sprintf("unknown launch mode %q", mode),
			"use one of default, fresh, or attach",
			nil,
		)
	}
	return o.LaunchApp(ctx, appID, args)
}
