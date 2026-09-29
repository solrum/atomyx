package androiddriver

import (
	"encoding/json"
	"reflect"
	"testing"

	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/wire"
)

func boolPtr(b bool) *bool { return &b }

func decodeRaw(t *testing.T, jsonSrc string) transport.AndroidTreeNode {
	t.Helper()
	var raw transport.AndroidTreeNode
	if err := json.Unmarshal([]byte(jsonSrc), &raw); err != nil {
		t.Fatalf("decode raw sample: %v", err)
	}
	return raw
}

func TestNormalizeAndroidTree_MapsAttributesAndState(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "btn1",
		"className": "android.widget.Button",
		"resourceId": "com.example:id/save",
		"text": "Save",
		"contentDesc": "Save changes",
		"hintText": "",
		"bounds": {"left": 10, "top": 20, "right": 110, "bottom": 60},
		"clickable": true,
		"enabled": true,
		"focused": false,
		"selected": false,
		"checkable": false,
		"checked": false,
		"visible": true,
		"isIme": false,
		"children": []
	}`)

	got := NormalizeAndroidTree(raw)

	want := wire.TreeNodeWire{
		Attributes: map[string]string{
			"id":     "com.example:id/save",
			"label":  "Save changes",
			"text":   "Save",
			"class":  "android.widget.Button",
			"role":   "button",
			"bounds": "10,20,110,60",
		},
		Children:  []wire.TreeNodeWire{},
		Clickable: boolPtr(true),
		Enabled:   boolPtr(true),
		Focused:   boolPtr(false),
		Selected:  boolPtr(false),
		Visible:   boolPtr(true),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAndroidTree() = %+v, want %+v", got, want)
	}
}

func TestNormalizeAndroidTree_HintTextMapsToHintAttribute(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "e1",
		"className": "android.widget.EditText",
		"hintText": "Enter your email",
		"clickable": false,
		"enabled": true,
		"focused": true,
		"selected": false,
		"checkable": false,
		"checked": false,
		"visible": true,
		"isIme": false
	}`)

	got := NormalizeAndroidTree(raw)
	if got.Attributes["hint"] != "Enter your email" {
		t.Fatalf("Attributes[hint] = %q, want %q", got.Attributes["hint"], "Enter your email")
	}
	if got.Attributes["role"] != "text-field" {
		t.Fatalf("Attributes[role] = %q, want text-field", got.Attributes["role"])
	}
}

func TestNormalizeAndroidTree_IsImeSetsExtensionAttribute(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "kb1",
		"className": "android.inputmethodservice.KeyboardView",
		"isIme": true,
		"clickable": false,
		"enabled": true,
		"focused": false,
		"selected": false,
		"checkable": false,
		"checked": false,
		"visible": true
	}`)

	got := NormalizeAndroidTree(raw)
	if got.Attributes["ext:isIme"] != "true" {
		t.Fatalf("Attributes[ext:isIme] = %q, want %q", got.Attributes["ext:isIme"], "true")
	}
}

func TestNormalizeAndroidTree_CheckableTrueCarriesChecked(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "cb1",
		"className": "android.widget.CheckBox",
		"clickable": true,
		"enabled": true,
		"focused": false,
		"selected": false,
		"checkable": true,
		"checked": true,
		"visible": true
	}`)

	got := NormalizeAndroidTree(raw)
	if got.Checked == nil || !*got.Checked {
		t.Fatalf("Checked = %v, want a true pointer", got.Checked)
	}
	if got.Attributes["role"] != "checkbox" {
		t.Fatalf("Attributes[role] = %q, want checkbox", got.Attributes["role"])
	}
}

func TestNormalizeAndroidTree_NotCheckableLeavesCheckedNil(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "v1",
		"className": "android.widget.TextView",
		"text": "hello",
		"clickable": false,
		"enabled": true,
		"focused": false,
		"selected": false,
		"checkable": false,
		"checked": true,
		"visible": true
	}`)

	got := NormalizeAndroidTree(raw)
	if got.Checked != nil {
		t.Fatalf("Checked = %v, want nil for a non-checkable node", *got.Checked)
	}
}

func TestNormalizeAndroidTree_RoleFromClassName(t *testing.T) {
	cases := []struct {
		className string
		wantRole  string
	}{
		{"android.widget.RadioButton", "radio-button"},
		{"android.widget.CheckBox", "checkbox"},
		{"android.widget.EditText", "text-field"},
		{"androidx.appcompat.widget.SearchView", "search-field"},
		{"android.widget.ImageButton", "button"},
		{"android.widget.Button", "button"},
		{"android.widget.ImageView", "image"},
		{"android.widget.TextView", "text"},
		{"android.widget.Switch", "switch"},
		{"android.widget.SeekBar", "slider"},
		{"androidx.recyclerview.widget.RecyclerView", "list"},
		{"android.webkit.WebView", "other"},
		{"android.widget.FrameLayout", "container"},
		{"android.widget.LinearLayout", "container"},
		{"com.example.CustomBox", "container"},
		{"android.widget.SomethingUnknown", "other"},
	}
	for _, tc := range cases {
		t.Run(tc.className, func(t *testing.T) {
			raw := transport.AndroidTreeNode{ClassName: tc.className}
			got := NormalizeAndroidTree(raw)
			if got.Attributes["role"] != tc.wantRole {
				t.Fatalf("role for %s = %q, want %q", tc.className, got.Attributes["role"], tc.wantRole)
			}
		})
	}
}

func TestNormalizeAndroidTree_BareViewDerivesRoleFromSignals(t *testing.T) {
	cases := []struct {
		name     string
		raw      transport.AndroidTreeNode
		wantRole string
	}{
		{
			name:     "clickable with text becomes button",
			raw:      transport.AndroidTreeNode{ClassName: "android.view.View", Clickable: boolPtr(true), Text: "Tap me"},
			wantRole: "button",
		},
		{
			name:     "clickable with content description becomes button",
			raw:      transport.AndroidTreeNode{ClassName: "android.view.View", Clickable: boolPtr(true), ContentDesc: "Tap me"},
			wantRole: "button",
		},
		{
			name:     "text without clickable becomes text",
			raw:      transport.AndroidTreeNode{ClassName: "android.view.View", Clickable: boolPtr(false), Text: "Just a label"},
			wantRole: "text",
		},
		{
			name:     "no signal falls back to other",
			raw:      transport.AndroidTreeNode{ClassName: "android.view.View"},
			wantRole: "other",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeAndroidTree(tc.raw)
			if got.Attributes["role"] != tc.wantRole {
				t.Fatalf("role = %q, want %q", got.Attributes["role"], tc.wantRole)
			}
		})
	}
}

func TestNormalizeAndroidTree_RecursesIntoChildrenPreservingOrder(t *testing.T) {
	raw := decodeRaw(t, `{
		"elementId": "root",
		"className": "android.widget.FrameLayout",
		"clickable": false,
		"enabled": true,
		"focused": false,
		"selected": false,
		"checkable": false,
		"checked": false,
		"visible": true,
		"children": [
			{"elementId": "a", "className": "android.widget.TextView", "text": "first"},
			{"elementId": "b", "className": "android.widget.TextView", "text": "second"}
		]
	}`)

	got := NormalizeAndroidTree(raw)
	if len(got.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(got.Children))
	}
	if got.Children[0].Attributes["text"] != "first" || got.Children[1].Attributes["text"] != "second" {
		t.Fatalf("children order not preserved: %+v", got.Children)
	}
}

func TestNormalizeAndroidTree_EmptyStringFieldsOmittedFromAttributes(t *testing.T) {
	raw := transport.AndroidTreeNode{ClassName: ""}
	got := NormalizeAndroidTree(raw)
	for _, key := range []string{"id", "label", "text", "hint", "class", "bounds", "ext:isIme"} {
		if _, ok := got.Attributes[key]; ok {
			t.Fatalf("Attributes[%q] present, want absent for an empty raw field", key)
		}
	}
	if got.Attributes["role"] != "other" {
		t.Fatalf(`Attributes["role"] = %q, want "other"`, got.Attributes["role"])
	}
}
