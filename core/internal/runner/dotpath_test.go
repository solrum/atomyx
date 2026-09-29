package runner

import "testing"

func TestResolveDotPathSingleIndex(t *testing.T) {
	obj := map[string]any{
		"items": []any{"first", "second"},
	}
	got := resolveDotPath(obj, "$.items[0]")
	if got != "first" {
		t.Fatalf("resolveDotPath() = %v, want %q", got, "first")
	}
}

func TestResolveDotPathNoIndex(t *testing.T) {
	obj := map[string]any{"name": "atomyx"}
	got := resolveDotPath(obj, "$.name")
	if got != "atomyx" {
		t.Fatalf("resolveDotPath() = %v, want %q", got, "atomyx")
	}
}

func TestResolveDotPathNestedIndexResolvesIntoNestedArray(t *testing.T) {
	obj := map[string]any{
		"matrix": []any{
			[]any{"a0-0", "a0-1"},
			[]any{"a1-0", "a1-1"},
		},
	}
	got := resolveDotPath(obj, "$.matrix[0][1]")
	if got != "a0-1" {
		t.Fatalf("resolveDotPath() = %v, want %q", got, "a0-1")
	}

	got = resolveDotPath(obj, "$.matrix[1][0]")
	if got != "a1-0" {
		t.Fatalf("resolveDotPath() = %v, want %q", got, "a1-0")
	}
}

func TestResolveDotPathNestedIndexContinuesThroughTrailingSegments(t *testing.T) {
	obj := map[string]any{
		"rows": []any{
			[]any{
				map[string]any{"name": "cell"},
			},
		},
	}
	got := resolveDotPath(obj, "$.rows[0][0].name")
	if got != "cell" {
		t.Fatalf("resolveDotPath() = %v, want %q", got, "cell")
	}
}

func TestResolveDotPathOutOfRangeIndexYieldsUndefined(t *testing.T) {
	obj := map[string]any{
		"items": []any{"only"},
	}
	if got := resolveDotPath(obj, "$.items[5]"); !isJSUndefined(got) {
		t.Fatalf("resolveDotPath() = %v, want jsUndefined", got)
	}
}

func TestResolveDotPathNestedOutOfRangeIndexYieldsUndefined(t *testing.T) {
	obj := map[string]any{
		"matrix": []any{
			[]any{"only"},
		},
	}
	if got := resolveDotPath(obj, "$.matrix[0][5]"); !isJSUndefined(got) {
		t.Fatalf("resolveDotPath() = %v, want jsUndefined", got)
	}
}
