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
// A device the inspector acquired is also given to another Atomyx
// process that asks for it while no capture or action is running. The
// page then watches that process and offers the reconnect once it ends.
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
	var yielder inspector.DeferredYielder
	opener := inspectOpener(deps, desc, &refresher, &yielder)
	held, err := opener(ctx)
	if err != nil {
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	watched, watching := held.(*session.RemoteHandle)
	if watching {
		_, _ = fmt.Fprintf(stderr, "The device is held by %s. The inspector watches it and does not drive it.\n", watched.Info().Holder.Describe()) //nolint:contextcheck // the handle reads its status on its own connection context
	}

	logger := logging.NewStructuredLogger(stderr, logging.LevelInfo)
	host, err := inspector.NewHost(inspector.HostConfig{ //nolint:contextcheck // the idle timer outlives the command's call context
		Target: held, Open: inspectReconnectOpener(deps, desc, &refresher, &yielder, opener), Follow: inspectFollower(deps, desc), Clock: deps.clock(), IdleMs: deps.Timeouts.WithDefaults().McpIdleMs, Logger: logger,
	})
	if err != nil {
		_ = held.Close()
		return reportUncaught(ctx, stderr, deps.Debug, err)
	}
	refresher.Bind(host)
	yielder.Bind(host)
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
	_, _ = fmt.Fprintln(stderr, "Press Enter for a fresh URL. "+stopHint(watching))

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

// stopHint tells what Ctrl-C does: a viewer has no device to release.
func stopHint(watching bool) string {
	if watching {
		return "Ctrl-C stops watching the device and stops the inspector."
	}
	return "Ctrl-C releases the device and stops the inspector."
}

// inspectOpenAttempts bounds how often the opener retries after a device
// that looked free was taken by another process before it could be
// acquired, or one that looked held was released before it could be
// attached. The attempts share one wait budget, inspectAttachBudgetMs.
const inspectOpenAttempts = 3

// inspectOpener acquires the device, or attaches to the process that
// holds it. It attaches first, because a held device needs no runner
// build. A device that is free is acquired with socketRefresher as the
// answer to a viewer's fresh capture request. When the device is taken
// between the probe and the acquisition, it tries again, and it returns
// the device's own coded error when the attempts run out. Without a lease
// manager that can be probed, it only acquires.
func inspectOpener(deps Deps, desc device.Descriptor, socketRefresher session.Refresher, yielder session.Yielder) inspector.Opener {
	attach, canProbe := inspectAttachDeps(deps)
	return func(ctx context.Context) (inspector.Target, error) {
		var held error
		budget := newAttachBudget(deps.Session.Clock)
		for attempt := 0; attempt < inspectOpenAttempts; attempt++ {
			if canProbe {
				if held != nil && budget.remainingMs() <= 0 {
					return nil, held
				}
				attachCtx, stop := ctx, func() {}
				if held != nil {
					attachCtx, stop = budget.bound(ctx)
				}
				result, err := session.Attach(attachCtx, attach, desc)
				cut := cutOff(ctx, attachCtx)
				stop()
				if cut {
					return nil, held
				}
				if err != nil {
					return nil, err
				}
				if result.Handle != nil {
					return result.Handle, nil
				}
			}
			opened, err := acquireSession(ctx, deps, inspectCommandName, desc, socketRefresher, yielder)
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

// inspectReconnectOpener is the opener of the operator's reconnect. It
// takes the device when it is free or held by an idle process that can
// yield it. When the device stays held, it falls back to watch, which
// attaches to the holder or reports the device's own coded error. Every
// other failure is returned as it is, so a failed launch is not repeated.
// Only an explicit reconnect borrows, because the inspector's own paths
// must never take a device from another process unasked.
func inspectReconnectOpener(deps Deps, desc device.Descriptor, socketRefresher session.Refresher, yielder session.Yielder, watch inspector.Opener) inspector.Opener {
	return func(ctx context.Context) (inspector.Target, error) {
		opened, err := borrowSession(ctx, deps, inspectCommandName, inspectCommandName, session.Request{Descriptor: desc, SocketRefresher: socketRefresher, Yielder: yielder})
		if err == nil {
			return opened, nil
		}
		if _, _, isHeld := heldHolder(err); !isHeld {
			return nil, err
		}
		return watch(ctx)
	}
}

// inspectAttachDeps names the viewer the inspector attaches as, and reports
// whether the lease manager can be probed at all.
func inspectAttachDeps(deps Deps) (session.AttachDeps, bool) {
	prober, canProbe := deps.Session.Leases.(session.LeaseProber)
	return session.AttachDeps{
		Leases: prober, Identity: deps.Session.Identity, Clock: deps.Session.Clock, Client: inspectCommandName,
	}, canProbe
}

// inspectFollower attaches to the process that takes the device right after
// the inspector yielded it, waiting a bounded time for it to appear.
func inspectFollower(deps Deps, desc device.Descriptor) inspector.Opener {
	attach, canProbe := inspectAttachDeps(deps)
	return func(ctx context.Context) (inspector.Target, error) {
		if !canProbe {
			return nil, session.ErrNoBorrower
		}
		handle, err := session.Follow(ctx, attach, desc)
		if err != nil {
			return nil, err
		}
		return handle, nil
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
