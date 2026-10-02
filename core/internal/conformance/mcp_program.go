package conformance

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

// mcpDefaultHierarchyRepeat is the fallback repeat count for
// McpDriverProgram.Hierarchy when a case does not set Repeat: high
// enough that no MCP tool call in the differential corpus can ever
// exhaust the queue and hit mock.Driver's "no tree staged" error,
// regardless of how many times the tool happens to re-read the
// hierarchy internally.
const mcpDefaultHierarchyRepeat = 200

// McpHierarchyStage is one entry of McpDriverProgram.HierarchySequence:
// tree answers the next Repeat Hierarchy reads (or
// mcpDefaultHierarchyRepeat when Repeat is zero) before the sequence
// advances to its next stage, or becomes sticky if it is the last one.
type McpHierarchyStage struct {
	Hierarchy wire.TreeNodeWire `json:"hierarchy"`
	Repeat    int               `json:"repeat,omitempty"`
}

// McpDriverProgram is the declarative MockDriver program an MCP
// corpus case stages before replaying its steps.
//
// Most cases set Hierarchy (+ optional Repeat): one static screen,
// repeated Repeat times (or mcpDefaultHierarchyRepeat when zero), so
// every Hierarchy read during the case sees the identical tree. The
// differential corpus picks screens where every step's target is
// already resolvable from that one screen, which sidesteps needing
// the golden and the Go server to read the hierarchy the same number
// of times internally.
//
// A case whose classification depends on something appearing that
// was not present before (tap_and_wait_transition's overlay-based
// timeout classifications) instead sets HierarchySequence: an ordered
// list of stages, each answering its own Repeat count of Hierarchy
// reads before the next stage takes over, with the last stage sticky
// once its own queue is exhausted. This keeps the "before" and
// "after" reads deterministic without pinning the Go server to a
// specific number of internal Hierarchy calls: as long as a case's
// first stage covers every read up to and including the tap's own
// resolution, which stage answers which read past that point cannot
// change the reads that matter for classification (the very first
// read, and every read once the sequence has gone sticky). When
// HierarchySequence is set, Hierarchy and Repeat are ignored.
type McpDriverProgram struct {
	Hierarchy         wire.TreeNodeWire           `json:"hierarchy"`
	Repeat            int                         `json:"repeat,omitempty"`
	HierarchySequence []McpHierarchyStage         `json:"hierarchySequence,omitempty"`
	Capabilities      *RunnerCapabilitiesOverride `json:"capabilities,omitempty"`
	Screen            *wire.Size                  `json:"screen,omitempty"`

	// Apps lists the app ids the Go-side mock driver considers
	// installed. It is read only by the Go factory below: the
	// recorded list_apps golden always answers empty regardless of
	// input, so a case that also compares list_apps output must not
	// set Apps, or the Go side stops matching that golden. A case
	// exercising launch_app past that comparison sets it so the
	// launch reaches the outcome the golden expects.
	Apps []string `json:"apps,omitempty"`
}

// mcpScriptedMockFactory implements device.Factory for the MCP
// differential: every Build call ignores the requested Descriptor and
// returns a fresh mock.Driver staged from the same program, so a case
// can register the identical factory under both the "ios" and
// "android" DeviceSession keys and still get deterministic, matching
// behavior regardless of which platform the corpus case targets.
type mcpScriptedMockFactory struct {
	program *McpDriverProgram
	wrap    device.DriverWrap
}

var _ device.WrappingFactory = (*mcpScriptedMockFactory)(nil)

// WithDriverWrap implements device.WrappingFactory. The wrap sits
// between the Orchestra and the mock, so the Orchestra's captures
// reach whatever the wrap observes.
func (f *mcpScriptedMockFactory) WithDriverWrap(wrap device.DriverWrap) device.Factory {
	wrapped := *f
	previous := f.wrap
	wrapped.wrap = func(d driver.Driver) driver.Driver {
		if previous != nil {
			d = previous(d)
		}
		return wrap(d)
	}
	return &wrapped
}

func (f *mcpScriptedMockFactory) Build(ctx context.Context, _ device.Descriptor) (device.Built, error) {
	drv := mock.New()
	if err := drv.Connect(ctx); err != nil {
		return device.Built{}, err
	}
	if f.program != nil {
		if f.program.Capabilities != nil {
			applyCapabilitiesOverride(drv, f.program.Capabilities)
		}
		if f.program.Screen != nil {
			drv.Screen = *f.program.Screen
		}
		if len(f.program.Apps) > 0 {
			apps := make([]driver.InstalledApp, len(f.program.Apps))
			for i, id := range f.program.Apps {
				apps[i] = driver.InstalledApp{BundleID: id}
			}
			drv.Apps = apps
		}
		if len(f.program.HierarchySequence) > 0 {
			for _, stage := range f.program.HierarchySequence {
				repeat := stage.Repeat
				if repeat <= 0 {
					repeat = mcpDefaultHierarchyRepeat
				}
				drv.StageHierarchyRepeated(stage.Hierarchy, repeat)
			}
		} else {
			repeat := f.program.Repeat
			if repeat <= 0 {
				repeat = mcpDefaultHierarchyRepeat
			}
			drv.StageHierarchyRepeated(f.program.Hierarchy, repeat)
		}
	}
	var built driver.Driver = drv
	if f.wrap != nil {
		built = f.wrap(drv)
	}
	return device.Built{
		Driver:    built,
		Orchestra: orchestra.New(orchestra.OrchestraDeps{Driver: built, Clock: orchestra.SystemClock{}}),
	}, nil
}
