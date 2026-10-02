// Package browseropen opens a URL in the platform's default browser
// by running the platform's own opener command.
package browseropen

import (
	"context"
	"fmt"
	"strings"

	"dev.atomyx/internal/process"
)

// Opener implements cli.BrowserOpener over a process.Runner.
type Opener struct {
	runner process.Runner
	goos   string
}

// New returns an Opener that runs the opener command of goos through
// runner.
func New(runner process.Runner, goos string) *Opener {
	return &Opener{runner: runner, goos: goos}
}

// Open opens url, which must be an http URL, in the default browser.
// It fails when the platform has no known opener or the opener command
// fails.
func (o *Opener) Open(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("browseropen: %q is not an http URL", url)
	}
	name, args, ok := command(o.goos, url)
	if !ok {
		return fmt.Errorf("browseropen: no default browser opener is known for %s", o.goos)
	}
	if _, err := o.runner.Run(context.Background(), name, args...); err != nil {
		return fmt.Errorf("browseropen: %s: %w", name, err)
	}
	return nil
}

func command(goos, url string) (string, []string, bool) {
	switch goos {
	case "darwin":
		return "open", []string{url}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return "xdg-open", []string{url}, true
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, true
	}
	return "", nil, false
}
