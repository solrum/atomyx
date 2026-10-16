package androiddriver

import (
	"context"
	"fmt"
	"strings"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

const viewIntentAction = "android.intent.action.VIEW"

// OpenLink asks the device to start a view intent for link through the
// activity manager, restricted to opts.AppID when it is set.
//
// The activity manager exits 0 even when it could not start an
// activity, and reports that on its output with a leading "Error:". The
// method reads that line, so a link no app handles fails with
// diag.CodeLinkNotOpened instead of passing silently.
func (d *AndroidDriver) OpenLink(ctx context.Context, link string, opts driver.OpenLinkOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := driver.ValidateLink(link); err != nil {
		return err
	}
	if opts.AppID != "" && !androidPackageNamePattern.MatchString(opts.AppID) {
		return diag.New(diag.CodeInvalidArgument,
			fmt.Sprintf("%q is not a valid app id", opts.AppID),
			diag.InvalidArgumentRemedy, nil)
	}
	res, err := d.proc.Run(ctx, "adb", openLinkArguments(d.opts.Serial, link, opts.AppID)...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errCode, _, _ := diag.Describe(err); errCode == diag.CodeToolTimeout {
			return err
		}
		reason := firstOutputLine(res.Stderr, res.Stdout)
		if adbDeviceUnreachable(reason) {
			return d.unreachable(err)
		}
		return diag.New(diag.CodeLinkNotOpened, linkNotOpenedMessage(reason),
			"Check that the device is connected and an installed app handles this link, then try again.", err)
	}
	if line := activityManagerError(res.Stdout + "\n" + res.Stderr); line != "" {
		return diag.New(diag.CodeLinkNotOpened, linkNotOpenedMessage(line),
			"Check that an installed app handles this link, or name the app that should open it.", nil)
	}
	return nil
}

// openLinkArguments builds the adb command line that starts a view
// intent. adb joins every word after "shell" into one string for the
// device shell, so the link and the package name are single-quoted to
// reach the activity manager as one word each.
func openLinkArguments(serial, link, appID string) []string {
	args := []string{"-s", serial, "shell", "am", "start", "-a", viewIntentAction, "-d", shellQuote(link)}
	if appID != "" {
		args = append(args, "-p", shellQuote(appID))
	}
	return args
}

// shellQuote wraps value in single quotes for a POSIX shell. A single
// quote inside the value closes the quoted run, adds an escaped quote,
// and opens a new run, which is the only form that keeps every other
// character literal.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// activityManagerError returns the first output line the activity
// manager prefixes with "Error", or an empty string when it started an
// activity.
func activityManagerError(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Error") {
			return line
		}
	}
	return ""
}

// firstOutputLine returns the first non-empty line of the first output
// that has one.
func firstOutputLine(outputs ...string) string {
	for _, output := range outputs {
		for _, line := range strings.Split(output, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return ""
}

// linkNotOpenedMessage describes a failed open without echoing the
// device's text when that text embeds the link, because a link may
// carry a token the caller does not want in a log.
func linkNotOpenedMessage(deviceLine string) string {
	if strings.Contains(deviceLine, "unable to resolve Intent") {
		return "no installed app handles the link"
	}
	if deviceLine == "" || strings.Contains(deviceLine, "Intent {") {
		return "the device did not start an app for the link"
	}
	return "the device did not start an app for the link: " + deviceLine
}
