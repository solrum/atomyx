package orchestra

import (
	"context"
	"fmt"
	"math"

	"dev.atomyx/internal/wire"
)

// Leaf-count threshold and fractional height threshold for
// ScrollController's viewport pre-check. A tree with fewer than this
// many leaves AND whose deepest leaf sits above this fraction of the
// screen height is treated as thin: its scroll-search budget is
// capped to avoid burning swipes on a non-scrollable or lazily-
// loading container.
const (
	thinTreeLeafThreshold  = 5
	thinTreeHeightFraction = 0.9
	thinTreeBudget         = 2
)

// ScrollDriver is the narrow driver.Driver subset ScrollController
// needs.
type ScrollDriver interface {
	HierarchyDriver
	Swipe(ctx context.Context, from, to wire.Point, durationMs int) error
	ScreenSize(ctx context.Context) (wire.Size, error)
}

// ViewportInsets are safe-area insets, in points, around the region
// ScrollController treats as "visible".
type ViewportInsets struct {
	Top, Bottom, Left, Right float64
}

// DefaultInsets is 60pt top (status bar and nav bar large-title
// zone) and 50pt bottom (home indicator and tab bar), with no side
// insets.
var DefaultInsets = ViewportInsets{Top: 60, Bottom: 50, Left: 0, Right: 0}

// ScrollControllerOptions configures a ScrollController. Use
// DefaultScrollControllerOptions and override individual fields
// rather than constructing the zero value, since a zero
// MaxScrollFraction or SwipeDurationMs disables scrolling entirely.
type ScrollControllerOptions struct {
	// MaxIterations caps positional-scroll iterations after the
	// target is found, preventing runaway loops on misconfigured
	// scrollables.
	MaxIterations int
	// MaxScrollFraction is how far each swipe travels, as a fraction
	// of the screen height. 0.6 means 60% of the screen per swipe.
	MaxScrollFraction float64
	// SwipeDurationMs is the swipe animation duration.
	SwipeDurationMs int
	// SettleWaitMs is how long to wait after each swipe for the
	// scroll view to settle.
	SettleWaitMs int
	// ScrollSearchBudget is the number of swipes in each direction
	// (up then down) tried when the target isn't in the current
	// snapshot at all. Total swipes = 2 * ScrollSearchBudget.
	ScrollSearchBudget int
	ViewportInsets     ViewportInsets
}

// DefaultScrollControllerOptions returns the defaults the
// ScrollController applies when a caller omits an option.
func DefaultScrollControllerOptions() ScrollControllerOptions {
	return ScrollControllerOptions{
		MaxIterations:      8,
		MaxScrollFraction:  0.6,
		SwipeDurationMs:    200,
		SettleWaitMs:       500,
		ScrollSearchBudget: 6,
		ViewportInsets:     DefaultInsets,
	}
}

// ScrollUnreachableError is returned by ScrollController.EnsureVisible
// when the target element cannot be found via scroll-search, or
// cannot be centered within the iteration budget. FinalBounds is the
// last known bounds of the element when available.
//
// NotFound is true when the element never appeared in the tree during
// the search, as opposed to appearing and then failing to reach the
// viewport.
type ScrollUnreachableError struct {
	Message     string
	FinalBounds *wire.Bounds
	NotFound    bool
}

func (e *ScrollUnreachableError) Error() string { return e.Message }

// ScrollController is the cross-platform scroll-into-view controller.
// It operates only on the canonical wire.TreeNodeWire shape plus the
// primitive Driver.Swipe gesture, so the same two-stage algorithm
// drives every platform adapter.
//
// Stage 1, scroll-search (virtualized list recovery): when the
// initial find returns zero matches, EnsureVisible assumes the
// target may be recycled off-screen by a virtualized list. It swipes
// the screen's center column up repeatedly (revealing items
// logically above), polling the filter after each swipe; if still
// empty after the up budget, it reverses direction and swipes down
// for another budget.
//
// Stage 2, positional scroll-into-view: once the target is present
// in the tree, EnsureVisible checks whether its midpoint sits in the
// safe-area-inset viewport. If not, it computes the direction
// (element above viewport midY scrolls content down, element below
// scrolls content up), executes one swipe sized to at most
// MaxScrollFraction of the screen height, re-resolves, and loops
// until the element is visible or its bounds stop moving.
type ScrollController struct {
	driver ScrollDriver
	clock  Clock
	finder *Finder
	opts   ScrollControllerOptions
}

// NewScrollController constructs a ScrollController over driverDep
// and clock, with the given options.
func NewScrollController(driverDep ScrollDriver, clock Clock, opts ScrollControllerOptions) *ScrollController {
	return &ScrollController{
		driver: driverDep,
		clock:  clock,
		finder: NewFinder(driverDep, clock),
		opts:   opts,
	}
}

// EnsureVisible brings the element matching filter into the
// interactive viewport, returning the cursor once the element's
// midpoint sits inside the safe-area-inset rect.
func (s *ScrollController) EnsureVisible(ctx context.Context, filter ElementFilter) (*TreeCursor, error) {
	return s.EnsureVisibleSearching(ctx, filter, "")
}

// EnsureVisibleSearching is EnsureVisible with the order of the
// scroll-search directions chosen by the caller. firstDirection is
// "down" to reveal content below the screen first, or "up" to reveal
// content above it first. Both directions are still searched, so the
// element is found wherever it sits, and only the cost of finding it
// changes. An empty or unknown value keeps the default order, up then
// down.
func (s *ScrollController) EnsureVisibleSearching(ctx context.Context, filter ElementFilter, firstDirection string) (*TreeCursor, error) {
	screen, err := s.driver.ScreenSize(ctx)
	if err != nil {
		return nil, err
	}

	initial, err := s.finder.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var match *TreeCursor
	if len(initial) > 0 {
		match = initial[0]
	} else {
		found, err := s.scrollSearch(ctx, filter, screen, searchOrder(firstDirection))
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, &ScrollUnreachableError{
				Message:  "element not found after scrolling",
				NotFound: true,
			}
		}
		match = found
	}

	var previousBounds *wire.Bounds
	for iteration := 0; iteration < s.opts.MaxIterations; iteration++ {
		bounds := s.boundsOf(match)
		if bounds == nil {
			return match, nil
		}
		if s.isInsideInsetViewport(*bounds, screen) {
			return match, nil
		}
		if previousBounds != nil && boundsEqual(*previousBounds, *bounds) {
			return nil, &ScrollUnreachableError{
				Message: fmt.Sprintf(
					"element at %s did not move after scroll iteration %d. Likely at scroll-region edge.",
					formatBounds(*bounds), iteration,
				),
				FinalBounds: bounds,
			}
		}
		previousBounds = bounds

		if err := s.scrollTowardElement(ctx, *bounds, screen); err != nil {
			return nil, err
		}
		if err := s.clock.Sleep(ctx, int64(s.opts.SettleWaitMs)); err != nil {
			return nil, err
		}

		next, err := s.finder.Find(ctx, filter)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			return nil, &ScrollUnreachableError{
				Message: fmt.Sprintf(
					"element lost after scroll attempt %d because filter no longer matches after container reload",
					iteration+1,
				),
				FinalBounds: bounds,
			}
		}
		match = next[0]
	}

	return nil, &ScrollUnreachableError{
		Message:     fmt.Sprintf("element remained off-screen after %d iterations", s.opts.MaxIterations),
		FinalBounds: s.boundsOf(match),
	}
}

// scrollSearch is the alternating-direction probe with tree-signature
// saturation detection: swipes up then down, polling filter after
// each swipe. It breaks early when two consecutive hierarchy
// snapshots hash identically, signalling either a scroll boundary or
// a non-scrollable container.
func (s *ScrollController) scrollSearch(ctx context.Context, filter ElementFilter, screen wire.Size, order [2]string) (*TreeCursor, error) {
	currentTree, err := s.driver.Hierarchy(ctx)
	if err != nil {
		return nil, err
	}

	leafCount := CountLeaves(currentTree)
	maxBottom := MaxLeafBoundsBottom(currentTree)
	effectiveBudget := s.opts.ScrollSearchBudget
	if leafCount < thinTreeLeafThreshold && maxBottom < screen.Height*thinTreeHeightFraction {
		effectiveBudget = thinTreeBudget
	}

	for _, direction := range order {
		prevSig := HashVisibleLeaves(currentTree)
		for i := 0; i < effectiveBudget; i++ {
			if err := s.swipeCenter(ctx, screen, direction); err != nil {
				return nil, err
			}
			if err := s.clock.Sleep(ctx, int64(s.opts.SettleWaitMs)); err != nil {
				return nil, err
			}
			currentTree, err = s.driver.Hierarchy(ctx)
			if err != nil {
				return nil, err
			}
			if matches := filter(FromTree(currentTree)); len(matches) > 0 {
				return matches[0], nil
			}
			newSig := HashVisibleLeaves(currentTree)
			if newSig == prevSig {
				break
			}
			prevSig = newSig
		}
	}
	return nil, nil
}

// searchOrder returns the two scroll-search directions in the order
// they are tried, given the one the caller wants first.
func searchOrder(firstDirection string) [2]string {
	if firstDirection == "down" {
		return [2]string{"down", "up"}
	}
	return [2]string{"up", "down"}
}

// scrollTowardElement dispatches one scroll gesture sized and
// directed to move the element toward the viewport center. The
// scroll amount is clamped to MaxScrollFraction of the screen height
// so each iteration makes a predictable step.
func (s *ScrollController) scrollTowardElement(ctx context.Context, bounds wire.Bounds, screen wire.Size) error {
	from, to := ComputeScrollTowardElementEndpoints(bounds, screen, s.opts.MaxScrollFraction)
	return s.driver.Swipe(ctx, from, to, s.opts.SwipeDurationMs)
}

// ComputeScrollTowardElementEndpoints computes the swipe endpoints
// ScrollController dispatches to move bounds toward the viewport's
// vertical center. The horizontal coordinate is clamped 40 logical
// points in from either screen edge so the touch point never lands
// under an edge-swipe gesture area; the vertical travel is capped at
// maxScrollFraction of the screen height so one iteration makes a
// predictable, bounded step. Exported so the endpoint math is
// testable independent of a live driver.
func ComputeScrollTowardElementEndpoints(bounds wire.Bounds, screen wire.Size, maxScrollFraction float64) (from, to wire.Point) {
	elemMidX := (bounds.Left + bounds.Right) / 2
	elemMidY := (bounds.Top + bounds.Bottom) / 2
	viewportMidY := screen.Height / 2

	swipeX := ClampF(elemMidX, 40, screen.Width-40)
	deltaY := elemMidY - viewportMidY
	scrollAmount := math.Min(math.Abs(deltaY), screen.Height*maxScrollFraction)

	// Element below viewport: finger drags bottom-to-top (content
	// moves up). Element above viewport: finger drags top-to-bottom
	// (content moves down).
	elementBelow := deltaY > 0
	var fromY, toY float64
	if elementBelow {
		fromY = viewportMidY + scrollAmount/2
		toY = viewportMidY - scrollAmount/2
	} else {
		fromY = viewportMidY - scrollAmount/2
		toY = viewportMidY + scrollAmount/2
	}

	return wire.Point{X: swipeX, Y: fromY}, wire.Point{X: swipeX, Y: toY}
}

// swipeCenter is the blind center-column swipe scrollSearch uses.
// Distance is fixed to MaxScrollFraction, not adaptive, since the
// target's position isn't known yet.
func (s *ScrollController) swipeCenter(ctx context.Context, screen wire.Size, direction string) error {
	from, to := ComputeSwipeCenterEndpoints(screen, direction, s.opts.MaxScrollFraction)
	return s.driver.Swipe(ctx, from, to, s.opts.SwipeDurationMs)
}

// ComputeSwipeCenterEndpoints computes the swipe endpoints
// scrollSearch's blind center-column probe uses. Distance is fixed
// to maxScrollFraction of the screen height, not adaptive, since the
// target's position isn't known yet. direction is "up" or "down";
// any other value is treated as "down". Exported so the endpoint
// math is testable independent of a live driver.
func ComputeSwipeCenterEndpoints(screen wire.Size, direction string, maxScrollFraction float64) (from, to wire.Point) {
	x := screen.Width / 2
	midY := screen.Height / 2
	delta := (screen.Height * maxScrollFraction) / 2

	// "up" reveals items logically above: finger drags down, content
	// scrolls down. "down" reveals items below: finger drags up,
	// content scrolls up.
	var fromY, toY float64
	if direction == "up" {
		fromY = midY - delta
		toY = midY + delta
	} else {
		fromY = midY + delta
		toY = midY - delta
	}

	return wire.Point{X: x, Y: fromY}, wire.Point{X: x, Y: toY}
}

func (s *ScrollController) boundsOf(cursor *TreeCursor) *wire.Bounds {
	if b, ok := ParseBounds(cursor.Node.Attributes[AttrBounds]); ok {
		return &b
	}
	return nil
}

func (s *ScrollController) isInsideInsetViewport(b wire.Bounds, screen wire.Size) bool {
	center := BoundsCenter(b)
	insets := s.opts.ViewportInsets
	return center.X >= insets.Left &&
		center.X < screen.Width-insets.Right &&
		center.Y >= insets.Top &&
		center.Y < screen.Height-insets.Bottom
}

// ClampF clamps v into [min, max]. Returns min when the range is
// inverted (max < min) rather than panicking, since a caller-
// supplied screen dimension narrower than the margin it clamps
// against is a degenerate but recoverable input.
func ClampF(v, min, max float64) float64 {
	if max < min {
		return min
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func boundsEqual(a, b wire.Bounds) bool {
	return a.Left == b.Left && a.Top == b.Top && a.Right == b.Right && a.Bottom == b.Bottom
}

func formatBounds(b wire.Bounds) string {
	return fmt.Sprintf("[%g,%g,%g,%g]", b.Left, b.Top, b.Right, b.Bottom)
}
