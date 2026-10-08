package debugrun

import (
	"context"
	"fmt"
	"time"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/script"
)

// checkBudget bounds the look for the next step's target, so a resume
// never waits long on a device that is slow to answer.
const checkBudget = 3 * time.Second

// Finder looks for one element on the device. *orchestra.Orchestra
// satisfies it.
type Finder interface {
	FindOne(ctx context.Context, selector orchestra.Selector) (*orchestra.TreeCursor, error)
}

// missReason runs the advisory check of the step about to run. It
// returns an empty string when the step is not an immediate action with
// a selector, when no Finder is set, or when the target is on screen.
func (a *Adapter) missReason(ctx context.Context, step runner.GateStep) string {
	selector := immediateTarget(step.Step)
	finder := a.finder()
	if selector == nil || finder == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, checkBudget)
	defer cancel()
	match, err := finder.FindOne(ctx, runner.SelectorFor(*selector))
	if err != nil || match != nil {
		return ""
	}
	return fmt.Sprintf("No element on screen matches the selector of the next %s step", step.Command)
}

// immediateTarget returns the selector of a step that acts at once
// instead of waiting for its target: tap, type, longPress and read.
func immediateTarget(step script.ScriptStep) *script.ScriptSelector {
	switch s := step.(type) {
	case *script.TapStep:
		return &s.Selector
	case *script.TypeStep:
		return s.Into
	case *script.ReadStep:
		return &s.Selector
	case *script.LongPressStep:
		if s.At != nil {
			return s.At.Selector
		}
	}
	return nil
}

func (a *Adapter) finder() Finder {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opts.Finder
}
