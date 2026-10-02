package orchestra

import (
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/wire"
)

func suggestNode(attrs map[string]string, children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return wire.TreeNodeWire{Attributes: attrs, Children: children}
}

func withState(n wire.TreeNodeWire, enabled, clickable, focused *bool) wire.TreeNodeWire {
	n.Enabled, n.Clickable, n.Focused = enabled, clickable, focused
	return n
}

func suggestRoot(children ...wire.TreeNodeWire) wire.TreeNodeWire {
	return suggestNode(map[string]string{AttrRole: RoleContainer}, children...)
}

func patternValue(t *testing.T, p *Pattern) string {
	t.Helper()
	if p == nil {
		return ""
	}
	b, err := p.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Trim(string(b), `"`)
}

func TestSuggestSelectors_IDWinsAndRanksFirst(t *testing.T) {
	root := suggestRoot(suggestNode(map[string]string{
		AttrID: "submit", AttrLabel: "Submit", AttrText: "Submit", AttrRole: "button",
	}))
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want id, label, text candidates, got %d: %+v", len(got), got)
	}
	if patternValue(t, got[0].Selector.ID) != "submit" || got[0].Selector.Role != nil {
		t.Fatalf("first candidate must be the bare id: %+v", got[0])
	}
	for i, c := range got {
		if !c.Unique || c.MatchCount != 1 {
			t.Fatalf("candidate %d must be unique with one match: %+v", i, c)
		}
	}
	if got[1].Selector.Label == nil || got[2].Selector.Text == nil {
		t.Fatalf("order must follow id, label, text: %+v", got)
	}
}

func TestSuggestSelectors_RoleDisambiguates(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrLabel: "Save", AttrRole: "image"}),
		suggestNode(map[string]string{AttrLabel: "Save", AttrRole: "button"}),
	)
	got, err := SuggestSelectors(root, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Unique {
		t.Fatalf("want one unique candidate: %+v", got)
	}
	s := got[0].Selector
	if s.Role == nil || *s.Role != "button" || s.Enabled != nil || s.Clickable != nil || s.Nth != nil {
		t.Fatalf("only the role may be added: %+v", s)
	}
	if got[0].MatchCount != 1 {
		t.Fatalf("match count must count the constrained set: %+v", got[0])
	}
}

func TestSuggestSelectors_StateDisambiguatesWhenRoleCannot(t *testing.T) {
	root := suggestRoot(
		withState(suggestNode(map[string]string{AttrText: "Pay", AttrRole: "button"}), boolPtr(false), nil, nil),
		withState(suggestNode(map[string]string{AttrText: "Pay", AttrRole: "button"}), boolPtr(true), nil, nil),
	)
	got, err := SuggestSelectors(root, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	s := got[0].Selector
	if !got[0].Unique || s.Enabled == nil || !*s.Enabled || s.Role != nil {
		t.Fatalf("the smallest rescuing constraint set is enabled alone: %+v", got[0])
	}
}

func TestSuggestSelectors_RankingDoesNotCountAsUnique(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrText: "Go", AttrRole: "text"}),
		withState(suggestNode(map[string]string{AttrText: "Go", AttrRole: "text"}), nil, boolPtr(true), nil),
	)
	got, err := SuggestSelectors(root, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Selector.Clickable == nil || !got[0].Unique || got[0].MatchCount != 1 {
		t.Fatalf("a candidate that only wins by ranking must be constrained: %+v", got[0])
	}
}

func TestSuggestSelectors_NthIsLastResort(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrText: "Row", AttrRole: "cell"}),
		suggestNode(map[string]string{AttrText: "Row", AttrRole: "cell"}),
		suggestNode(map[string]string{AttrText: "Row", AttrRole: "cell"}),
	)
	for index := 0; index < 3; index++ {
		got, err := SuggestSelectors(root, []int{index})
		if err != nil {
			t.Fatal(err)
		}
		first := got[0]
		if !first.Unique || first.Selector.Nth == nil || *first.Selector.Nth != index {
			t.Fatalf("index %d: want a positional pin: %+v", index, first)
		}
		if first.Selector.Text == nil || first.Selector.Role == nil || first.MatchCount != 3 {
			t.Fatalf("index %d: nth must combine the best field and the role: %+v", index, first)
		}
		picked, ok := FindOne(CompileSelector(first.Selector), root)
		if !ok || picked.ChildIndex != index {
			t.Fatalf("index %d: the selector must resolve to the target", index)
		}
	}
}

func TestSuggestSelectors_NthFollowsDocumentOrderNotRank(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrText: "Row"}),
		withState(suggestNode(map[string]string{AttrText: "Row"}), nil, boolPtr(true), nil),
		withState(suggestNode(map[string]string{AttrText: "Row"}), nil, boolPtr(true), nil),
	)
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Unique || got[0].Selector.Nth == nil || *got[0].Selector.Nth != 0 {
		t.Fatalf("the first node in document order is nth 0: %+v", got[0])
	}
}

func TestSuggestSelectors_SecureNodeNeverUsesTextOrValue(t *testing.T) {
	const secret = "hunter2"
	root := suggestRoot(
		suggestNode(map[string]string{
			AttrRole: RoleTextField, AttrSecure: "true", AttrText: secret, AttrValue: secret, AttrHint: "Password",
		}),
	)
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Selector.Hint == nil || !got[0].Unique {
		t.Fatalf("the hint must carry the suggestion: %+v", got)
	}
	for _, c := range got {
		if c.Selector.Text != nil || c.Selector.Value != nil {
			t.Fatalf("secure text and value must never be used: %+v", c)
		}
	}

	onlySecret := suggestRoot(suggestNode(map[string]string{
		AttrRole: RoleTextField, AttrSecure: "true", AttrText: secret, AttrValue: secret,
	}))
	got, err = SuggestSelectors(onlySecret, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Selector.Text != nil || c.Selector.Value != nil {
			t.Fatalf("secure text and value must never be used: %+v", c)
		}
	}
	if len(got) != 1 || got[0].Selector.Role == nil || !got[0].Unique {
		t.Fatalf("a role alone identifies the only secure field: %+v", got)
	}
}

func TestSuggestSelectors_SkipsUnusableValues(t *testing.T) {
	tooLong := strings.Repeat("x", MaxSuggestedValueLength+1)
	atLimit := strings.Repeat("y", MaxSuggestedValueLength)
	root := suggestRoot(suggestNode(map[string]string{
		AttrID: "", AttrLabel: "   \n", AttrText: tooLong, AttrValue: atLimit, AttrHint: "ok",
	}))
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Selector.Value == nil || got[1].Selector.Hint == nil {
		t.Fatalf("only the value at the bound and the hint remain: %+v", got)
	}
}

func TestSuggestSelectors_NoUsableSignalYieldsNothing(t *testing.T) {
	root := suggestRoot(suggestNode(map[string]string{}))
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want no candidates: %+v", got)
	}
}

func TestSuggestSelectors_AmbiguousCandidateIsReportedNotUnique(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrLabel: "Same"}),
		suggestNode(map[string]string{AttrLabel: "Same"}),
	)
	got, err := SuggestSelectors(root, []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Unique || got[0].Selector.Nth == nil || got[1].Unique || got[1].MatchCount != 2 {
		t.Fatalf("nth must pin the target and the bare label must stay listed as ambiguous: %+v", got)
	}
}

func TestSuggestSelectors_InvalidPath(t *testing.T) {
	root := suggestRoot(suggestNode(map[string]string{AttrID: "a"}))
	for _, path := range [][]int{{1}, {-1}, {0, 0}} {
		if _, err := SuggestSelectors(root, path); err == nil {
			t.Fatalf("path %v must be rejected", path)
		}
	}
}

func TestSuggestSelectors_RootIsAddressable(t *testing.T) {
	root := suggestNode(map[string]string{AttrID: "root"})
	got, err := SuggestSelectors(root, []int{})
	if err != nil || len(got) != 1 || !got[0].Unique {
		t.Fatalf("root must be suggestible: %+v, %v", got, err)
	}
}

func TestSuggestSelectors_DeterministicAndDoesNotMutate(t *testing.T) {
	root := suggestRoot(
		suggestNode(map[string]string{AttrLabel: "A", AttrRole: "button"}),
		suggestNode(map[string]string{AttrLabel: "A", AttrRole: "button"}),
	)
	first, err := SuggestSelectors(root, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := SuggestSelectors(root, []int{1})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("suggestions must be deterministic:\n%+v\n%+v", first, second)
	}
}
