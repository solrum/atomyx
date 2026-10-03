// Command check-listen-loopback fails when production Go code could
// listen on an address that is not loopback.
//
// The inspector and any later local surface expose device control, so a
// listener on all interfaces would hand that control to the network.
// The checker parses each production file and requires the address of
// every listen call to be provably loopback from the source alone.
//
// A call is checked when it is one of:
//
//   - Listen or ListenPacket, whose address is the last argument. A
//     call naming a unix network is skipped, because a socket path is
//     not a network address.
//     A call through the package named attach is skipped too, because
//     that package binds only unix sockets.
//   - ListenAndServe or ListenAndServeTLS, whose address is the first
//     argument.
//   - ListenTCP, ListenUDP, ListenIP or ListenMulticastUDP, which take
//     a computed address and are forbidden outright, so a caller uses
//     Listen with a loopback literal.
//   - A composite literal with an Addr field, such as http.Server.
//
// An address is accepted when it resolves to a string, through
// literals, constants, variables with a literal initializer and
// concatenation, whose host is localhost or a loopback IP. An empty
// host, a wildcard, any other host, and an address the checker cannot
// resolve are all violations. A string literal that is a wildcard
// address outside any listen call is flagged too, because it is one
// edit away from a listener.
//
// Usage: check-listen-loopback [root]. The root defaults to the
// current directory and must be the Go module root.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// selfDir is this checker, which has to spell the wildcard addresses
// to look for them.
const selfDir = "scripts/check-listen-loopback/"

type finding struct {
	line   int
	reason string
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	violations, err := scan(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-listen-loopback: %v\n", err)
		os.Exit(2)
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Println(v)
		}
		fmt.Println()
		fmt.Println("Listen on a loopback literal such as 127.0.0.1:0. A listener reachable from")
		fmt.Println("the network would expose device control to other machines.")
		os.Exit(1)
	}
	fmt.Println("PASS: every listen address in production Go is loopback")
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
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, selfDir) {
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
			out = append(out, fmt.Sprintf("FORBIDDEN: %s:%d: %s", rel, f.line, f.reason))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// check parses one file and returns every listen address it cannot
// prove loopback.
func check(filename string, src []byte) ([]finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	r := newResolver(file)
	var found []finding
	report := func(n ast.Node, reason string) {
		found = append(found, finding{line: fset.Position(n.Pos()).Line, reason: reason})
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			checkCall(x, r, report)
		case *ast.KeyValueExpr:
			if key, ok := x.Key.(*ast.Ident); ok && key.Name == "Addr" {
				checkAddress(x.Value, r, report)
			}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || r.consumed[lit] || r.skipped[lit] {
			return true
		}
		if value, err := strconv.Unquote(lit.Value); err == nil && isWildcardAddress(value) {
			report(lit, fmt.Sprintf("wildcard listen address %q", value))
		}
		return true
	})
	return found, nil
}

func checkCall(call *ast.CallExpr, r *resolver, report func(ast.Node, string)) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	switch sel.Sel.Name {
	case "Listen", "ListenPacket":
		if len(call.Args) == 0 || isAttachPackage(sel.X) {
			return
		}
		for _, a := range call.Args[:len(call.Args)-1] {
			if network, ok := r.resolve(a); ok && strings.HasPrefix(network, "unix") {
				return
			}
		}
		checkAddress(call.Args[len(call.Args)-1], r, report)
	case "ListenAndServe", "ListenAndServeTLS":
		if len(call.Args) > 0 {
			checkAddress(call.Args[0], r, report)
		}
	case "ListenTCP", "ListenUDP", "ListenIP", "ListenMulticastUDP":
		report(call, sel.Sel.Name+" takes a computed address, use Listen with a loopback literal")
	}
}

func checkAddress(expr ast.Expr, r *resolver, report func(ast.Node, string)) {
	addr, ok := r.resolve(expr)
	if !ok {
		report(expr, "listen address is not a constant the checker can resolve")
		return
	}
	if !isLoopbackAddress(addr) {
		report(expr, fmt.Sprintf("listen address %q is not loopback", addr))
	}
}

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isWildcardAddress(value string) bool {
	for _, wildcard := range []string{"0.0.0.0", "[::]"} {
		if value == wildcard || strings.HasPrefix(value, wildcard+":") {
			return true
		}
	}
	return false
}

// resolver evaluates string expressions from one file's own
// declarations.
type resolver struct {
	decls map[string]ast.Expr
	// consumed holds the literals a listen call's address was built
	// from, so the stray-wildcard scan does not report them twice.
	consumed map[*ast.BasicLit]bool
	// skipped holds import paths and struct tags, which are not
	// addresses.
	skipped map[*ast.BasicLit]bool
}

func newResolver(file *ast.File) *resolver {
	r := &resolver{decls: map[string]ast.Expr{}, consumed: map[*ast.BasicLit]bool{}, skipped: map[*ast.BasicLit]bool{}}
	ambiguous := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			r.skipped[x.Path] = true
		case *ast.Field:
			if x.Tag != nil {
				r.skipped[x.Tag] = true
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if _, seen := r.decls[name.Name]; seen || i >= len(x.Values) {
					ambiguous[name.Name] = true
					continue
				}
				r.decls[name.Name] = x.Values[i]
			}
		}
		return true
	})
	for name := range ambiguous {
		delete(r.decls, name)
	}
	return r
}

func (r *resolver) resolve(expr ast.Expr) (string, bool) {
	return r.resolveDepth(expr, 0)
}

const maxResolveDepth = 8

func (r *resolver) resolveDepth(expr ast.Expr, depth int) (string, bool) {
	if depth > maxResolveDepth {
		return "", false
	}
	switch x := expr.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(x.Value)
		if err != nil {
			return "", false
		}
		r.consumed[x] = true
		return value, true
	case *ast.ParenExpr:
		return r.resolveDepth(x.X, depth+1)
	case *ast.Ident:
		if value, ok := r.decls[x.Name]; ok {
			return r.resolveDepth(value, depth+1)
		}
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		left, lok := r.resolveDepth(x.X, depth+1)
		right, rok := r.resolveDepth(x.Y, depth+1)
		return left + right, lok && rok
	}
	return "", false
}

// isAttachPackage reports whether x names the attach package, whose
// Listen binds a unix socket and takes no network address.
func isAttachPackage(x ast.Expr) bool {
	pkg, ok := x.(*ast.Ident)
	return ok && pkg.Name == "attach"
}
