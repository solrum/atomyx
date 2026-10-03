package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/inspector"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/shutdown"
)

const inspectCommandName = "atomyx inspect"

// BrowserOpener opens a URL in the operator's default browser.
type BrowserOpener interface {
	Open(url string) error
}

// Inspect serves the browser inspector of the device named by flags
// (--platform, --device) until the context ends, then releases the
// device. It prints the bootstrap URL to stdout, one line per URL,
// and opens it unless --no-open is set. Each Enter read from
// deps.Stdin prints and opens a fresh URL, and the end of stdin
// stops that without stopping the server. The device is released
// after the idle timeout without an operator action and taken again
// only when the page asks. A device the session loses is dropped the
// same way and the page offers the same reconnect, so unlike run, suite
// and doctor, which cancel their work when the device is lost and end,
// the inspector keeps serving until it is stopped.
//
// Exit codes: 0 when the context ended with no stop signal, 1 when
// the device could not be reached or held or the server failed, 2
// for an unknown platform, and the signal's conventional code
// (130 for SIGINT, 143 for SIGTERM) when a stop signal ended it.
func Inspect(ctx context.Context, deps Deps, flags map[string]FlagValue) int {
	stdout, stderr := deps.stdout(), deps.stderr()
	platform := resolvePlatform(flags, nil)
	if platform != "android" && platform != "ios" {
		_, _ = fmt.Fprintf(stderr, "error: Unknown platform %q. Use --platform android or --platform ios.\n", platform)
		return 2
	}
	desc, err := resolveDescriptor(ctx, deps.Probe, deps.Runners, platform, resolveDeviceID(flags, nil))
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	var refresher inspector.DeferredRefresher
	opener := func(ctx context.Context) (inspector.Target, error) {
		held, err := acquireSession(ctx, deps, inspectCommandName, desc, &refresher)
		if err != nil {
			return nil, err
		}
		return held, nil
	}
	held, err := opener(ctx)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}

	logger := logging.NewStructuredLogger(stderr, logging.LevelInfo)
	host, err := inspector.NewHost(inspector.HostConfig{ //nolint:contextcheck // the idle timer outlives the command's call context
		Target: held, Open: opener, Clock: deps.clock(), IdleMs: deps.Timeouts.WithDefaults().McpIdleMs, Logger: logger,
	})
	if err != nil {
		_ = held.Close()
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	refresher.Bind(host)
	defer func() { _ = host.Close() }()
	srv, err := inspector.New(inspector.Config{ //nolint:contextcheck // the listener is not bound to a call context
		Source: host,
		Logger: logger,
		Clock:  deps.clock(),
		Groups: []inspector.RouteGroup{inspector.PageRoutes, host.Routes(inspector.ReadRoutes, inspector.SelectorRoutes)},
	})
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	defer func() { _ = srv.Close() }()

	noOpen := flagBool(flags, "--no-open")
	announce := func() error {
		url, err := srv.NewBootstrapURL()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, url)
		if noOpen || deps.Browser == nil {
			return nil
		}
		if err := deps.Browser.Open(url); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: the browser could not be opened (%s). Open the URL above yourself.\n", err)
		}
		return nil
	}

	served := make(chan error, 1)
	go func() { served <- srv.Serve() }()
	if err := announce(); err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	_, _ = fmt.Fprintln(stderr, "Press Enter for a fresh URL. Ctrl-C releases the device and stops the inspector.")

	enter, stopReading := readEnterKeys(deps.Stdin)
	defer stopReading()
	for {
		select {
		case <-ctx.Done():
			if code, ok := shutdown.InterruptExitCode(ctx); ok {
				_, _ = fmt.Fprintln(stderr, "interrupted")
				return code
			}
			return 0
		case err := <-served:
			if err == nil {
				return 0
			}
			return reportUncaught(ctx, stderr, deps.Debug, err)
		case <-enter:
			if err := announce(); err != nil {
				return reportUncaught(ctx, stderr, deps.Debug, err)
			}
		}
	}
}

// readEnterKeys delivers one value per line read from in. The end of
// in stops the delivery and nothing else. The returned stop ends the
// delivery, though a read already blocked on in returns only when in
// does. A nil in delivers nothing.
func readEnterKeys(in io.Reader) (<-chan struct{}, func()) {
	keys := make(chan struct{})
	done := make(chan struct{})
	if in == nil {
		return keys, func() {}
	}
	go func() {
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			select {
			case keys <- struct{}{}:
			case <-done:
				return
			}
		}
	}()
	return keys, func() { close(done) }
}
