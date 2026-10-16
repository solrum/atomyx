package orchestra

import (
	"context"
	"errors"
	"fmt"

	"dev.atomyx/internal/diag"
)

// Scroll directions ScrollUntilVisible accepts.
const (
	ScrollDirectionUp   = "up"
	ScrollDirectionDown = "down"
)

// ScrollUntilVisibleOptions configures Orchestra.ScrollUntilVisible.
type ScrollUntilVisibleOptions struct {
	// Direction is the scroll direction searched first: "down" reveals
	// content below the screen, "up" reveals content above it. The other
	// direction is still searched afterwards. Empty keeps the scroll
	// controller's default order.
	Direction string
}

// ScrollUntilVisible scrolls until the element matching selector sits
// inside the interactive viewport, and reports success once it does.
// Unlike Tap and InputText it dispatches no action on the element and
// does not check whether another element covers it.
//
// It returns a failed ActionResult, not an error, when the element is
// never found or cannot be brought into view. A failure caused by the
// element never appearing carries diag.CodeElementNotFound. It returns
// an error for an unsupported direction, an empty selector, or an
// infrastructure failure.
func (o *Orchestra) ScrollUntilVisible(ctx context.Context, selector Selector, opts ScrollUntilVisibleOptions) (ActionResult, error) {
	switch opts.Direction {
	case "", ScrollDirectionUp, ScrollDirectionDown:
	default:
		return ActionResult{}, diag.New(
			diag.CodeInvalidArgument,
			fmt.Sprintf("unknown scroll direction %q", opts.Direction),
			"use up or down",
			nil,
		)
	}
	if err := RequireNonEmpty(selector); err != nil {
		return ActionResult{}, err
	}

	cursor, err := o.scroll.EnsureVisibleSearching(ctx, CompileSelector(selector), opts.Direction)
	if err != nil {
		var unreachable *ScrollUnreachableError
		if errors.As(err, &unreachable) {
			return unreachableResult(unreachable), nil
		}
		return ActionResult{}, err
	}
	return ActionOk(guessResolvedBy(selector, cursor.Node), "element is visible"), nil
}

// unreachableResult converts a scroll failure into the failed
// ActionResult every scroll-based action reports.
func unreachableResult(unreachable *ScrollUnreachableError) ActionResult {
	failed := ActionFail(fmt.Sprintf("could not scroll element into view: %s", unreachable.Message), nil)
	if unreachable.NotFound {
		failed.Code = diag.CodeElementNotFound
	}
	return failed
}
