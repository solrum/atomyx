package orchestra

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver/mock"
	"dev.atomyx/internal/wire"
)

func roleNode(role, id, bounds string, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	attrs := map[string]string{AttrRole: role, AttrBounds: bounds}
	if id != "" {
		attrs[AttrID] = id
	}
	return wire.TreeNodeWire{Attributes: attrs, Children: children}
}

// signInScreen nests two text fields under containers of role other,
// the shape a Flutter Semantics tree takes on Android.
func signInScreen() wire.TreeNodeWire {
	email := roleNode("text-field", "email", "0,100,100,150")
	password := roleNode("text-field", "password", "0,200,100,250")
	return roleNode("other", "", "0,0,100,300",
		roleNode("other", "", "0,0,100,300",
			roleNode("container", "", "0,100,100,150", email),
			roleNode("container", "", "0,200,100,250", password),
		),
	)
}

func TestFind_RoleOnlySelectorReturnsEveryMatchInDocumentOrder(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(signInScreen())
	o, _ := newTestOrchestra(d)

	got, err := o.Find(context.Background(), Selector{Role: strPtr("text-field")})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 2 || idOf(got[0]) != "email" || idOf(got[1]) != "password" {
		t.Fatalf("got %d matches, want email then password", len(got))
	}
}

func TestFind_RoleWithNthSelectsByDocumentPosition(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(signInScreen())
	o, _ := newTestOrchestra(d)

	for nth, want := range map[int]string{0: "email", 1: "password"} {
		got, err := o.FindOne(context.Background(), Selector{Role: strPtr("text-field"), Nth: intPtr(nth)})
		if err != nil {
			t.Fatalf("FindOne nth=%d: %v", nth, err)
		}
		if got == nil || idOf(got) != want {
			t.Fatalf("nth=%d resolved to %v, want %s", nth, got, want)
		}
	}
}

func TestResolvePoint_RoleWithNthTargetsTheNthField(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(signInScreen())
	o, _ := newTestOrchestra(d)

	point, err := o.ResolvePoint(context.Background(), Selector{Role: strPtr("text-field"), Nth: intPtr(1)})
	if err != nil {
		t.Fatalf("ResolvePoint: %v", err)
	}
	if point.X != 50 || point.Y != 225 {
		t.Fatalf("point = %+v, want the password field center (50,225)", point)
	}
}

func TestEmptySelectorIsRejectedInsteadOfMatchingTheRoot(t *testing.T) {
	d := mock.New()
	d.StageHierarchy(signInScreen())
	o, _ := newTestOrchestra(d)
	ctx := context.Background()
	empty := Selector{Enabled: boolPtr(false)}

	calls := map[string]func() error{
		"Find":    func() error { _, err := o.Find(ctx, empty); return err },
		"FindOne": func() error { _, err := o.FindOne(ctx, empty); return err },
		"WaitFor": func() error { _, err := o.WaitFor(ctx, empty, WaitOptions{TimeoutMs: 10}); return err },
		"ResolvePoint": func() error {
			_, err := o.ResolvePoint(ctx, empty)
			return err
		},
		"InputText": func() error {
			_, err := o.InputText(ctx, empty, "x", InputTextOptions{})
			return err
		},
	}
	for name, call := range calls {
		var emptyErr *EmptySelectorError
		err := call()
		if !errors.As(err, &emptyErr) {
			t.Fatalf("%s error = %v, want *EmptySelectorError", name, err)
		}
		if emptyErr.Code() != diag.CodeInvalidArgument {
			t.Fatalf("%s code = %q, want %q", name, emptyErr.Code(), diag.CodeInvalidArgument)
		}
	}
}

func TestSelectorIsEmpty(t *testing.T) {
	if !(Selector{}).IsEmpty() {
		t.Fatal("zero selector must be empty")
	}
	for name, s := range map[string]Selector{
		"role":      {Role: strPtr("button")},
		"nth":       {Nth: intPtr(0)},
		"text":      {Text: lit("a")},
		"enabled":   {Enabled: boolPtr(true)},
		"clickable": {Clickable: boolPtr(true)},
		"focused":   {Focused: boolPtr(true)},
	} {
		if s.IsEmpty() {
			t.Fatalf("%s selector must not be empty", name)
		}
	}
}
