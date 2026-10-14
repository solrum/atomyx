// Command check-runfolder-layout fails when production Go code outside
// internal/runfolder spells a run folder layout name.
//
// The names are runs, v1, artifacts, report, screenshots, index.json,
// junit.xml, and the video file name prefix. They are
// the on-disk contract of a run folder, so one package owns them and
// every other package asks it for a path.
//
// The checker parses each production file and inspects every string
// literal, interpreted or raw, after unquoting it. Struct tags and
// import paths are skipped. A literal is flagged when:
//
//   - its whole value is runs, artifacts, or screenshots
//   - it is a path of slash-separated segments in which runs, artifacts,
//     screenshots, v1, or report is a whole segment, including link
//     formats such as screenshots/%s
//   - it is exactly v1 or report and is a direct argument of a path or
//     file call such as filepath.Join, path.Join, or os.MkdirAll
//
// The file names index.json and junit.xml are flagged as a whole literal
// or as a whole segment of a slashed path. The video name prefix is
// flagged when a literal, or a segment of a slashed path, begins with
// it and holds no whitespace.
//
// A two-segment literal that ends in v1, such as atomyx/v1, is a format
// identifier and is not flagged. The bare words v1 and report are
// common in config keys and message text, so outside a slashed path
// they are only flagged as an argument of a path or file call. A
// literal beginning with /api/ is an HTTP route of a local server. A
// literal containing whitespace or a URL scheme is prose or an
// address, not a directory, and is never treated as a path.
//
// Usage: check-runfolder-layout [root]. The root defaults to the
// current directory and must be the Go module root.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	ownDir = "internal/runfolder/"
	// selfDir is this checker, which has to name the layout names to
	// look for them.
	selfDir = "scripts/check-runfolder-layout/"
	// moduleRouterFile registers the top-level CLI modules by name. One of
	// them is called runs, which only coincides with the run folder
	// directory name, so that one key of the module map is not a layout
	// name. Every other literal in the file is still checked.
	moduleRouterFile = "internal/cli/router.go"
	// moduleMapVar is the package variable in moduleRouterFile that holds
	// the module map.
	moduleMapVar = "Modules"
	// moduleRunsKey is the module name that spells the run folder
	// directory name.
	moduleRunsKey = "runs"
)

var alwaysNames = map[string]bool{
	"runs": true, "artifacts": true, "screenshots": true,
}

// fileNames are run folder files that only internal/runfolder may name.
var fileNames = map[string]bool{
	"index.json": true, "junit.xml": true,
}

// videoPrefix begins the name of every recorded video file.
const videoPrefix = "video-"

var pathOnlyNames = map[string]bool{
	"v1": true, "report": true,
}

var pathPackages = map[string]bool{
	"filepath": true, "path": true, "os": true,
}

var pathFuncs = map[string]bool{
	"Join": true, "MkdirAll": true, "Mkdir": true, "RemoveAll": true,
	"Remove": true, "ReadDir": true, "Stat": true, "Lstat": true,
	"Open": true, "OpenFile": true, "Create": true, "ReadFile": true,
	"WriteFile": true, "Rename": true,
}

type finding struct {
	line  int
	value string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	violations, err := scan(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-runfolder-layout: %v\n", err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Println(v)
		}
		fmt.Println()
		fmt.Println("Use the path helpers in core/internal/runfolder instead of spelling runs, v1,")
		fmt.Println("artifacts, report, screenshots, index.json, junit.xml, or the video name prefix in another package.")
		os.Exit(1)
	}
	fmt.Println("PASS: run folder layout names are spelled only in core/" + ownDir)
}

func scan(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "bin", "dist", "testdata", "vendor", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, ownDir) || strings.HasPrefix(rel, selfDir) {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		found, parseErr := check(rel, src)
		if parseErr != nil {
			return parseErr
		}
		for _, f := range found {
			out = append(out, fmt.Sprintf("FORBIDDEN: %s:%d: run folder layout name spelled outside internal/runfolder: %q", rel, f.line, f.value))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// skipModuleRunsKey marks the "runs" key of the module map literal in spec
// as exempt, and nothing else in it.
func skipModuleRunsKey(spec *ast.ValueSpec, skip map[*ast.BasicLit]bool) {
	if len(spec.Names) != 1 || spec.Names[0].Name != moduleMapVar || len(spec.Values) != 1 {
		return
	}
	lit, ok := spec.Values[0].(*ast.CompositeLit)
	if !ok {
		return
	}
	if _, isMap := lit.Type.(*ast.MapType); !isMap {
		return
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING && key.Value == strconv.Quote(moduleRunsKey) {
			skip[key] = true
		}
	}
}

// check parses one file and returns every literal that spells a layout
// name. filename is the slash-separated path relative to the module root.
func check(filename string, src []byte) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	skip := map[*ast.BasicLit]bool{}
	pathArg := map[*ast.BasicLit]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			skip[x.Path] = true
		case *ast.Field:
			if x.Tag != nil {
				skip[x.Tag] = true
			}
		case *ast.ValueSpec:
			if filename == moduleRouterFile {
				skipModuleRunsKey(x, skip)
			}
		case *ast.CallExpr:
			if isPathCall(x.Fun) {
				for _, a := range x.Args {
					if lit, ok := a.(*ast.BasicLit); ok {
						pathArg[lit] = true
					}
				}
			}
		}
		return true
	})

	var found []finding
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || skip[lit] {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if spellsLayoutName(value, pathArg[lit]) {
			found = append(found, finding{line: fset.Position(lit.Pos()).Line, value: value})
		}
		return true
	})
	return found, nil
}

func isPathCall(fun ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		pkg, ok := f.X.(*ast.Ident)
		return ok && pathPackages[pkg.Name] && pathFuncs[f.Sel.Name]
	case *ast.Ident:
		return f.Name == "join" || f.Name == "Join"
	}
	return false
}

func spellsLayoutName(value string, pathCallArg bool) bool {
	if alwaysNames[value] {
		return true
	}
	if fileNames[value] || spellsVideoPrefix(value) {
		return true
	}
	if pathCallArg && pathOnlyNames[value] {
		return true
	}
	if strings.HasPrefix(value, "/api/") {
		return false
	}
	if !strings.Contains(value, "/") || strings.ContainsAny(value, " \t\r\n") || strings.Contains(value, "://") {
		return false
	}
	segments := strings.Split(value, "/")
	for i, seg := range segments {
		if alwaysNames[seg] {
			return true
		}
		if fileNames[seg] || spellsVideoPrefix(seg) {
			return true
		}
		if !pathOnlyNames[seg] {
			continue
		}
		formatIdentifier := seg == "v1" && len(segments) == 2 && i == 1
		if !formatIdentifier {
			return true
		}
	}
	return false
}

func spellsVideoPrefix(value string) bool {
	return strings.HasPrefix(value, videoPrefix) && !strings.ContainsAny(value, " \t\r\n")
}
