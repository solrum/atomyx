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

// LaunchOptions is everything a caller chooses about one launch beyond
// the app id.
type LaunchOptions struct {
	// Mode selects how the app starts. The zero value means
	// LaunchModeDefault.
	Mode LaunchMode
	// Arguments are named values handed to the app at launch. A launch
	// that carries arguments is never skipped as a repeat, because the
	// values have to reach a new start of the app.
	Arguments map[string]string
	// Permissions are granted or revoked before the app starts, so the
	// app sees them from its first moment. A launch that carries
	// permissions is never skipped as a repeat, because a running
	// instance may keep its earlier view of them. A device that cannot
	// set permissions fails the launch with diag.CodePermissionsUnsupported.
	Permissions map[driver.Permission]driver.PermissionState
	// Link, when not empty, starts the app by opening this link in it,
	// so the app lands on the link's destination. It cannot be combined
	// with a mode other than the default or with Arguments, because a
	// link launch has no separate start step to apply them to.
	Link string
}

// LaunchAppWithMode launches appID using mode and the dedup rule
// documented on LaunchApp. An unknown mode is rejected before any device
// call with an error coded diag.CodeInvalidArgument.
func (o *Orchestra) LaunchAppWithMode(ctx context.Context, appID string, mode LaunchMode) error {
	return o.LaunchAppWithOptions(ctx, appID, LaunchOptions{Mode: mode})
}

// LaunchAppWithOptions launches appID using opts.Mode and passes
// opts.Arguments to the app. The dedup rule documented on LaunchApp
// applies, except that a launch carrying arguments always reaches the
// driver. An unknown mode or an invalid arguments map is rejected
// before any device call with an error coded diag.CodeInvalidArgument.
//
// With opts.Permissions set, they are applied after the other options
// are validated and before the app starts, and the launch always
// reaches the driver.
//
// With opts.Link set, the app is started by OpenLink instead, and the
// dedup rule does not apply.
func (o *Orchestra) LaunchAppWithOptions(ctx context.Context, appID string, opts LaunchOptions) error {
	if opts.Link != "" {
		return o.launchAppWithLink(ctx, appID, opts)
	}
	if err := driver.ValidateLaunchArguments(opts.Arguments); err != nil {
		return err
	}
	args := driver.LaunchArgs{Arguments: opts.Arguments}
	mode := opts.Mode
	if mode == "" {
		mode = LaunchModeDefault
	}
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
	if len(opts.Permissions) > 0 {
		if err := o.SetPermissions(ctx, appID, opts.Permissions); err != nil {
			return err
		}
	}
	return o.launchApp(ctx, appID, args, len(opts.Permissions) > 0)
}
