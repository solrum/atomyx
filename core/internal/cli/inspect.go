package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/inspector"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/shutdown"
)

const inspectCommandName = "atomyx inspect"

// BrowserOpener opens a URL in the operator's default browser.
type BrowserOpener interface {
	Open(url string) error
}

// Inspect serves the browser inspector of the device named by flags
// (--platform, --device) until the context ends, then releases the
// device. It prints the bootstrap URL to stdout, one line per URL, and
// opens it unless --no-open is set. Each Enter read from deps.Stdin
// prints and opens a fresh URL, and the end of stdin stops that without
// stopping the server.
//
// A device another process holds is watched instead of acquired. The
// page shows what that holder captured, its state and its step lines,
// and asks it for a fresh capture only while it is idle. When that
// process exits, the page offers the reconnect, which acquires the
// device, or watches its next holder. The inspector never takes a
// device by itself.
//
// A device the inspector acquired is released after the idle timeout
// without an operator action and taken again only when the page asks. A
// device the session loses is dropped the same way and the page offers
// the same reconnect, so unlike run, suite and doctor, which cancel
// their work when the device is lost and end, the inspector keeps
// serving until it is stopped.
//
// Exit codes:
//
//   - 0 when the context ended with no stop signal.
//   - 1 when the device could not be reached or held, when its holder
//     serves no socket, is still starting or cannot be reached, or when
//     the server failed.
//   - 2 for an unknown platform.
//   - The signal's conventional code (130 for SIGINT, 143 for SIGTERM)
//     when a stop signal ended it.
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
	opener := inspectOpener(deps, desc, &refresher)
	held, err := opener(ctx)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	if watched, ok := held.(*session.RemoteHandle); ok {
		_, _ = fmt.Fprintf(stderr, "The device is held by %s. The inspector watches it and does not drive it.\n", watched.Info().Holder.Describe()) //nolint:contextcheck // the handle reads its status on its own connection context
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

// inspectOpenAttempts bounds how often the opener retries after a device
// that looked free was taken by another process before it could be
// acquired, or one that looked held was released before it could be
// attached.
const inspectOpenAttempts = 3

// inspectOpener acquires the device, or attaches to the process that
// holds it. It attaches first, because a held device needs no runner
// build. A device that is free is acquired with socketRefresher as the
// answer to a viewer's fresh capture request. When the device is taken
// between the probe and the acquisition, it tries again, and it returns
// the device's own coded error when the attempts run out. Without a lease
// manager that can be probed, it only acquires.
func inspectOpener(deps Deps, desc device.Descriptor, socketRefresher session.Refresher) inspector.Opener {
	prober, canProbe := deps.Session.Leases.(session.LeaseProber)
	attach := session.AttachDeps{
		Leases: prober, Identity: deps.Session.Identity, Clock: deps.Session.Clock, Client: inspectCommandName,
	}
	return func(ctx context.Context) (inspector.Target, error) {
		var held error
		for attempt := 0; attempt < inspectOpenAttempts; attempt++ {
			if canProbe {
				result, err := session.Attach(ctx, attach, desc)
				if err != nil {
					return nil, err
				}
				if result.Handle != nil {
					return result.Handle, nil
				}
			}
			opened, err := acquireSession(ctx, deps, inspectCommandName, desc, socketRefresher)
			if err == nil {
				return opened, nil
			}
			if _, _, isHeld := heldHolder(err); !isHeld || !canProbe {
				return nil, err
			}
			held = err
		}
		return nil, held
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
