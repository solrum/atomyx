package diag

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCodesListsEveryDeclaredCode(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "diag.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "Code") || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, _ := strconv.Unquote(lit.Value)
			declared = append(declared, value)
		}
		return true
	})
	if len(declared) == 0 {
		t.Fatal("found no declared codes")
	}
	listed := Codes()
	for _, code := range declared {
		if !slices.Contains(listed, code) {
			t.Errorf("Codes() is missing %s", code)
		}
	}
	if len(listed) != len(declared) {
		t.Errorf("Codes() lists %d codes, diag.go declares %d", len(listed), len(declared))
	}
}
