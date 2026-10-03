package device

import (
	"context"
	"fmt"
	"sync"

	"dev.atomyx/internal/logging"
)

// Probe scans for devices reachable from this host. Scan never
// returns an error: a probe that cannot reach its tooling (adb
// missing, xcrun missing) reports an empty list instead, so a caller
// or a CompositeProbe never has to distinguish "found nothing" from
// "the scanner is broken."
type Probe interface {
	Scan(ctx context.Context) []Descriptor
}

// CompositeProbe aggregates several probes. A failure inside one
// probe's own tooling must not hide devices visible to the others,
// so each probe runs independently and a panic in one is isolated
// from the rest.
type CompositeProbe struct {
	probes []Probe
	logger logging.Logger
}

// NewCompositeProbe constructs a CompositeProbe over probes, logging
// through logger whenever a constituent probe panics. A nil logger
// defaults to logging.NoopLogger{}.
func NewCompositeProbe(logger logging.Logger, probes ...Probe) *CompositeProbe {
	if logger == nil {
		logger = logging.NoopLogger{}
	}
	return &CompositeProbe{probes: probes, logger: logger}
}

var _ Probe = (*CompositeProbe)(nil)

// Scan implements Probe by running every constituent probe
// concurrently and concatenating their results. Result order across
// probes is not guaranteed.
func (c *CompositeProbe) Scan(ctx context.Context) []Descriptor {
	return c.scan(ctx, c.probes)
}

// PlatformScoped is implemented by a probe that only ever reports
// devices of one platform, so a caller that needs one platform can skip
// the probes of the others.
type PlatformScoped interface {
	// ProbePlatform names the only platform the probe reports.
	ProbePlatform() Platform
}

// platformScanner scans only the probes that can report one platform.
type platformScanner interface {
	ScanPlatform(ctx context.Context, platform Platform) []Descriptor
}

// ScanPlatform scans the constituent probes that can report platform:
// every probe that is not PlatformScoped, and each PlatformScoped probe
// whose platform matches. A probe of another platform is never called,
// so resolving an Android device does not spend an iOS tooling scan.
func (c *CompositeProbe) ScanPlatform(ctx context.Context, platform Platform) []Descriptor {
	selected := make([]Probe, 0, len(c.probes))
	for _, p := range c.probes {
		if scoped, ok := p.(PlatformScoped); ok && scoped.ProbePlatform() != platform {
			continue
		}
		selected = append(selected, p)
	}
	return c.scan(ctx, selected)
}

var _ platformScanner = (*CompositeProbe)(nil)

// scanPlatform scans probe for platform, narrowing a probe that can
// scope its scan.
func scanPlatform(ctx context.Context, probe Probe, platform Platform) []Descriptor {
	if s, ok := probe.(platformScanner); ok {
		return s.ScanPlatform(ctx, platform)
	}
	return probe.Scan(ctx)
}

func (c *CompositeProbe) scan(ctx context.Context, probes []Probe) []Descriptor {
	results := make([][]Descriptor, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Add(1)
		go func(i int, p Probe) {
			defer wg.Done()
			// A probe that panics instead of returning an empty list is a
			// bug in that probe's own tooling integration, not a "no
			// devices" result, so it is logged here rather than silently
			// folded into an empty scan.
			defer func() {
				if rec := recover(); rec != nil {
					c.logger.Error("device probe panicked", nil, logging.LogContext{
						"probe": fmt.Sprintf("%T", p),
						"panic": fmt.Sprintf("%v", rec),
					})
				}
			}()
			results[i] = p.Scan(ctx)
		}(i, p)
	}
	wg.Wait()

	var out []Descriptor
	for _, r := range results {
		out = append(out, r...)
	}
	return out
}

// StaticProbe returns a fixed, scripted sequence of device lists, which is
// the device-free seam a test uses in place of a real adb/xcrun
// scan. Each call to Scan returns the next queued snapshot; once the
// queue is exhausted, Scan returns an empty list.
//
// StaticProbe is safe for concurrent use.
type StaticProbe struct {
	mu        sync.Mutex
	snapshots [][]Descriptor
}

// NewStaticProbe constructs a StaticProbe that returns each of
// snapshots in order, one per Scan call.
func NewStaticProbe(snapshots ...[]Descriptor) *StaticProbe {
	return &StaticProbe{snapshots: append([][]Descriptor(nil), snapshots...)}
}

var _ Probe = (*StaticProbe)(nil)

// Scan implements Probe.
func (p *StaticProbe) Scan(context.Context) []Descriptor {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.snapshots) == 0 {
		return nil
	}
	next := p.snapshots[0]
	p.snapshots = p.snapshots[1:]
	return next
}
