package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/driver/mock"
)

func typeInto(t *testing.T, d *mock.Driver, sel Selector) error {
	t.Helper()
	o, _ := newTestOrchestra(d)
	_, err := o.InputText(context.Background(), sel, "secret", InputTextOptions{})
	return err
}

func TestInputText_FocusFollowsResolvedNodeWhenLabelChanges(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(node("", "Email", "0,200,300,250")))
	d.StageHierarchy(rootWithChildren(focusedNode("", "", "0,200,300,250")))

	if err := typeInto(t, d, Selector{Text: lit("Email")}); err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	assertDriverTyped(t, d, "secret")
}

func TestInputText_FocusFollowsResolvedNodeThroughLayoutShift(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(
		node("", "Email", "0,400,300,450"),
		node("", "Password", "0,500,300,550"),
	))
	d.StageHierarchy(rootWithChildren(
		node("", "Email", "0,250,300,300"),
		focusedNode("", "Password", "0,350,300,400"),
	))

	if err := typeInto(t, d, Selector{Text: lit("Password")}); err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
	assertDriverTyped(t, d, "secret")
}

func TestInputText_FocusConfirmedByStableIDBeyondShiftTolerance(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(node("pw", "", "0,1500,300,1550")))
	d.StageHierarchy(rootWithChildren(focusedNode("pw", "", "0,100,300,150"), node("other", "", "0,1500,300,1550")))

	// Neither the pre-tap frame nor the shift tolerance reaches the new
	// frame, so only the id can confirm it. The id node is the focused
	// node itself.
	if err := typeInto(t, d, Selector{ID: lit("pw")}); err != nil {
		t.Fatalf("InputText returned error: %v", err)
	}
}

func TestInputText_RefusesWhenPreviouslyFocusedFieldKeepsFocus(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(
		focusedNode("", "Email", "0,400,300,450"),
		node("", "Password", "0,500,300,550"),
	))

	err := typeInto(t, d, Selector{Text: lit("Password")})
	var focusErr *InputFocusError
	if !errors.As(err, &focusErr) {
		t.Fatalf("got error %v (%T), want *InputFocusError", err, err)
	}
	if hasDriverCall(d.Calls, "InputText") {
		t.Fatal("text must not be dispatched when a different field holds focus")
	}
}

func TestInputText_RefusesWhenAnotherFieldShiftsOntoAnotherNodesFrame(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(rootWithChildren(
		node("", "Name", "0,300,300,350"),
		node("", "Email", "0,400,300,450"),
		node("", "Password", "0,500,300,550"),
	))
	// The layout moves down by one row and the name field takes focus,
	// landing exactly on the email field's pre-tap frame. A frame that
	// another node already occupied means a different field, so it is
	// not the shifted target.
	d.StageHierarchy(rootWithChildren(
		focusedNode("", "Name", "0,400,300,450"),
		node("", "Email", "0,500,300,550"),
		node("", "Password", "0,600,300,650"),
	))

	err := typeInto(t, d, Selector{Text: lit("Password")})
	var focusErr *InputFocusError
	if !errors.As(err, &focusErr) {
		t.Fatalf("got error %v (%T), want *InputFocusError", err, err)
	}
}
