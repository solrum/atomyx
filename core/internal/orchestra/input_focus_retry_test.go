package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
)

// flakyInputDriver fails the first typeFailures InputText calls with
// typeErr, delegating every other call to the embedded mock.
type flakyInputDriver struct {
	*mock.Driver
	typeErr      error
	typeFailures int
	typeAttempts int
}

func (f *flakyInputDriver) InputText(ctx context.Context, text string) error {
	f.typeAttempts++
	if f.typeAttempts <= f.typeFailures {
		return f.typeErr
	}
	return f.Driver.InputText(ctx, text)
}

func notFocusedErr() error {
	return diag.New(diag.CodeInputNotFocused, "no input field has focus", "Tap the field.", nil)
}

func countCalls(d *mock.Driver, method string) int {
	n := 0
	for _, c := range d.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func newFlakyInputOrchestra(typeErr error, failures int) (*Orchestra, *flakyInputDriver) {
	m := mock.New()
	m.StageHierarchy(rootWithChildren(focusedNode("email", "", "0,200,300,250")))
	f := &flakyInputDriver{Driver: m, typeErr: typeErr, typeFailures: failures}
	return New(OrchestraDeps{Driver: f, Clock: NewFakeClock(0)}), f
}

func TestInputText_RefocusesAndRetriesOnceWhenFocusIsLostBeforeDispatch(t *testing.T) {
	o, f := newFlakyInputOrchestra(notFocusedErr(), 1)

	if _, err := o.InputText(context.Background(), Selector{ID: lit("email")}, "secret", InputTextOptions{}); err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	if got := countCalls(f.Driver, "Tap"); got != 2 {
		t.Errorf("Tap calls = %d, want 2 (initial focus tap and one re-focus tap)", got)
	}
	if f.typeAttempts != 2 {
		t.Errorf("type attempts = %d, want 2", f.typeAttempts)
	}
	assertDriverTyped(t, f.Driver, "secret")
}

func TestInputText_FailsWithOriginalErrorWhenFocusIsLostTwice(t *testing.T) {
	first := notFocusedErr()
	o, f := newFlakyInputOrchestra(first, 2)

	_, err := o.InputText(context.Background(), Selector{ID: lit("email")}, "secret", InputTextOptions{})
	if !errors.Is(err, first) {
		t.Fatalf("error = %v, want the original focus error", err)
	}
	if f.typeAttempts != 2 {
		t.Errorf("type attempts = %d, want exactly 2", f.typeAttempts)
	}
	if got := countCalls(f.Driver, "Tap"); got != 2 {
		t.Errorf("Tap calls = %d, want 2", got)
	}
}

func TestInputText_DoesNotRetryUnrelatedErrors(t *testing.T) {
	boom := errors.New("transport down")
	o, f := newFlakyInputOrchestra(boom, 1)

	_, err := o.InputText(context.Background(), Selector{ID: lit("email")}, "secret", InputTextOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the unrelated error", err)
	}
	if f.typeAttempts != 1 {
		t.Errorf("type attempts = %d, want 1", f.typeAttempts)
	}
	if got := countCalls(f.Driver, "Tap"); got != 1 {
		t.Errorf("Tap calls = %d, want 1", got)
	}
}
