package process

import (
	"context"
	"sync"
)

// Invocation records one call made to a Fake's Run method, in
// dispatch order.
type Invocation struct {
	Name string
	Args []string
	// Env is the additional environment the call's context carried
	// through WithEnv, or nil when it carried none.
	Env []string
}

// Matcher reports whether an invocation matches a stubbed response.
type Matcher func(name string, args []string) bool

// ExactMatch returns a Matcher that matches only the exact command
// name and argument list, in order.
func ExactMatch(name string, args ...string) Matcher {
	wantArgs := append([]string(nil), args...)
	return func(gotName string, gotArgs []string) bool {
		if gotName != name || len(gotArgs) != len(wantArgs) {
			return false
		}
		for i, a := range wantArgs {
			if gotArgs[i] != a {
				return false
			}
		}
		return true
	}
}

type stub struct {
	match  Matcher
	result Result
	err    error
}

// Fake is a Runner test double. A test stages canned responses with
// Stub, keyed by a Matcher, and inspects Invocations afterward to
// assert which commands were dispatched and in what order. A call
// matching no stub returns Default and DefaultErr.
//
// Fake is safe for concurrent use.
type Fake struct {
	mu          sync.Mutex
	stubs       []stub
	invocations []Invocation

	// Default and DefaultErr are returned for an invocation matching
	// no registered stub.
	Default    Result
	DefaultErr error
}

// NewFake constructs an empty Fake with no stubs.
func NewFake() *Fake {
	return &Fake{}
}

var _ Runner = (*Fake)(nil)

// Stub registers a canned response for every future invocation
// matching match. Stubs are checked in registration order; the first
// match wins.
func (f *Fake) Stub(match Matcher, result Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stubs = append(f.stubs, stub{match: match, result: result, err: err})
}

// Invocations returns every call made to Run so far, in dispatch
// order.
func (f *Fake) Invocations() []Invocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Invocation(nil), f.invocations...)
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.invocations = append(f.invocations, Invocation{Name: name, Args: append([]string(nil), args...), Env: append([]string(nil), EnvFrom(ctx)...)})
	for _, s := range f.stubs {
		if s.match(name, args) {
			return s.result, s.err
		}
	}
	return f.Default, f.DefaultErr
}
