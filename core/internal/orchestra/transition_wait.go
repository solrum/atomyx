package orchestra

import (
	"context"
	"errors"
)

// WaitForTransitionParams configures Orchestra.WaitForTransition. At
// least one of WaitForAbsent or WaitForAppear should be set; a nil
// condition is treated as already satisfied.
type WaitForTransitionParams struct {
	WaitForAbsent *Selector
	WaitForAppear *Selector

	// TimeoutMs is the wait's soft deadline: the poll gives up after
	// this many milliseconds unless a loading indicator or element
	// motion is observed, in which case the deadline extends by up
	// to another TimeoutMs, never past MaxTimeoutMs.
	TimeoutMs int
	// MaxTimeoutMs is the hard deadline no extension can exceed.
	MaxTimeoutMs int
	// IntervalMs is the delay between polls.
	IntervalMs int
	// LoadingKeywords supplements the built-in loading-role
	// vocabulary with caller-specified label substrings.
	LoadingKeywords []string

	// Selector is the tap target. On timeout, WaitForTransition
	// re-resolves it to diagnose whether the tap itself changed the
	// target's state.
	Selector Selector
	// TargetPre is Selector's resolved state captured before the tap
	// that triggered this wait.
	TargetPre ResolvedElementState
	// ResolvedBy identifies which selector priority field resolved
	// the tap target, carried into a successful result unchanged.
	ResolvedBy string
	// BeforeSnapshot is a compact snapshot of the hierarchy captured
	// immediately before the triggering tap. It anchors element-
	// appearance diffing and overlay detection against the pre-tap
	// screen.
	BeforeSnapshot []CompactElement
}

// WaitForTransitionResult is Orchestra.WaitForTransition's outcome.
// When Ok is false, the remaining fields diagnose why the transition
// never resolved: Classification is one of ClassifyFailure's
// classification values, and Hint is written for a downstream agent
// deciding what to do next.
type WaitForTransitionResult struct {
	Ok         bool
	WaitedMs   int64
	ResolvedBy string

	Classification          string
	Hint                    string
	DialogAppeared          []AppearedElement
	Appeared                []AppearedElement
	CurrentSnapshot         []string
	TargetStateChangeReason string
	Overlay                 *OverlayAnalysis
}

// transitionPoll is one hierarchy read taken while WaitForTransition
// polls: whether the caller's completion conditions are satisfied,
// and the loading/motion signals computed against the previous poll
// (or, for the first poll, against BeforeSnapshot).
type transitionPoll struct {
	absentOk bool
	appearOk bool
	loading  LoadingSignal
	motion   MotionResult
}

// WaitForTransition polls the hierarchy until params.WaitForAbsent no
// longer matches and params.WaitForAppear matches (whichever of the
// two the caller set), or the hard deadline elapses. Detecting a
// loading indicator or element motion at the soft deadline extends
// it, so a screen that is visibly still working gets more time before
// the wait gives up.
//
// On timeout, WaitForTransition takes a fresh hierarchy read and
// classifies why the transition never resolved via ClassifyFailure,
// comparing that fresh read against params.BeforeSnapshot rather than
// whatever the last poll happened to observe.
func (o *Orchestra) WaitForTransition(ctx context.Context, params WaitForTransitionParams) (WaitForTransitionResult, error) {
	startedAt := o.clock.NowMs()
	hardDeadlineMs := startedAt + int64(params.MaxTimeoutMs)

	prevCompact := params.BeforeSnapshot
	fetch := func(ctx context.Context) (transitionPoll, error) {
		tree, err := o.Hierarchy(ctx)
		if err != nil {
			return transitionPoll{}, err
		}
		compact := TreeNodeToCompactElements(tree)
		loading := DetectLoading(compact, params.LoadingKeywords)
		motion := DetectMotion(prevCompact, compact, DefaultMotionMinDeltaPx)
		prevCompact = compact

		absentOk := true
		if params.WaitForAbsent != nil {
			matches, err := o.Find(ctx, *params.WaitForAbsent)
			if err != nil {
				return transitionPoll{}, err
			}
			absentOk = len(matches) == 0
		}
		appearOk := true
		if params.WaitForAppear != nil {
			matches, err := o.Find(ctx, *params.WaitForAppear)
			if err != nil {
				return transitionPoll{}, err
			}
			appearOk = len(matches) > 0
		}
		return transitionPoll{absentOk: absentOk, appearOk: appearOk, loading: loading, motion: motion}, nil
	}
	resolved := func(p transitionPoll) bool { return p.absentOk && p.appearOk }

	for {
		now := o.clock.NowMs()
		remainingHardMs := hardDeadlineMs - now
		if remainingHardMs <= 0 {
			break
		}
		windowMs := int64(params.TimeoutMs)
		if windowMs > remainingHardMs {
			windowMs = remainingHardMs
		}

		poll, err := WaitUntil(ctx, o.clock, int(windowMs), params.IntervalMs, "waitForTransition", fetch, resolved)
		if err == nil {
			return WaitForTransitionResult{
				Ok:         true,
				WaitedMs:   o.clock.NowMs() - startedAt,
				ResolvedBy: params.ResolvedBy,
			}, nil
		}
		var timeoutErr *WaitTimeoutError
		if !errors.As(err, &timeoutErr) {
			return WaitForTransitionResult{}, err
		}
		if o.clock.NowMs() >= hardDeadlineMs || (!poll.loading.Detected && !poll.motion.Detected) {
			break
		}
	}

	return o.classifyTransitionTimeout(ctx, params, startedAt)
}

// classifyTransitionTimeout builds WaitForTransition's failure result
// after the poll gives up: a fresh hierarchy read, the tap target's
// post-wait state, and ClassifyFailure's diagnosis, all compared
// against params.BeforeSnapshot and params.TargetPre.
func (o *Orchestra) classifyTransitionTimeout(
	ctx context.Context,
	params WaitForTransitionParams,
	startedAt int64,
) (WaitForTransitionResult, error) {
	afterTree, err := o.Hierarchy(ctx)
	if err != nil {
		return WaitForTransitionResult{}, err
	}
	afterCompact := TreeNodeToCompactElements(afterTree)
	post, err := o.ResolveElementState(ctx, params.Selector)
	if err != nil {
		return WaitForTransitionResult{}, err
	}

	targetStateChange := DetectTargetStateChange(params.TargetPre, post)
	overlay := AnalyzeOverlay(params.BeforeSnapshot, afterCompact, DefaultOverlayMinCoverage)
	loadingSignal := DetectLoading(afterCompact, params.LoadingKeywords)
	motion := DetectMotion(params.BeforeSnapshot, afterCompact, DefaultMotionMinDeltaPx)

	absentOk := true
	if params.WaitForAbsent != nil {
		matches, err := o.Find(ctx, *params.WaitForAbsent)
		if err != nil {
			return WaitForTransitionResult{}, err
		}
		absentOk = len(matches) == 0
	}
	appearOk := false
	if params.WaitForAppear != nil {
		matches, err := o.Find(ctx, *params.WaitForAppear)
		if err != nil {
			return WaitForTransitionResult{}, err
		}
		appearOk = len(matches) > 0
	}

	diag := ClassifyFailure(params.BeforeSnapshot, afterCompact, absentOk, appearOk, ClassifyFailureOptions{
		LoadingSignal:     &loadingSignal,
		Motion:            &motion,
		LoadingKeywords:   params.LoadingKeywords,
		TargetStateChange: &targetStateChange,
		Overlay:           overlay,
	})

	return WaitForTransitionResult{
		Ok:                      false,
		WaitedMs:                o.clock.NowMs() - startedAt,
		Classification:          diag.Classification,
		Hint:                    diag.Hint,
		DialogAppeared:          diag.DialogAppeared,
		Appeared:                diag.Appeared,
		CurrentSnapshot:         diag.CurrentSnapshot,
		TargetStateChangeReason: targetStateChange.Reason,
		Overlay:                 overlay,
	}, nil
}
