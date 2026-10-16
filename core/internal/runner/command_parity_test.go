package runner

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	stepsSourcePath    = "../script/steps.go"
	schemaPath         = "../script/atomyx-script.schema.json"
	scriptCorpusDir    = "../conformance/corpus/script"
	runnerCorpusDir    = "../conformance/corpus/runner"
	referenceDocPath   = "../../../docs/yml-script-reference.md"
	commandConstPrefix = "Command"
)

// TestCommandParity fails when a YAML step command is missing any of the
// four places that make it usable: a runner registration, an entry in the
// generated JSON Schema, a parse case and a run case in the conformance
// corpus, and a heading in the script reference. Each subtest names the
// command, so a failure points at the exact command and the exact gap.
func TestCommandParity(t *testing.T) {
	commands := declaredStepCommands(t)
	if len(commands) == 0 {
		t.Fatalf("no step command constants found in %s", stepsSourcePath)
	}

	registered := registeredCommands()
	schemaConsts := schemaCommandConsts(t)
	parseCorpus := corpusYAML(t, scriptCorpusDir)
	runCorpus := corpusYAML(t, runnerCorpusDir)
	headings := referenceHeadings(t)

	for _, name := range commands {
		t.Run(name, func(t *testing.T) {
			if !registered[name] {
				t.Errorf("command %q has no runner registration: add its CommandDefinition to DefaultCommands in commands.go", name)
			}
			if !schemaConsts[name] {
				t.Errorf("command %q has no JSON schema entry: add it to the step definitions in script/jsonschema_steps.go and regenerate with make gen-schema", name)
			}
			if !usesCommand(parseCorpus, name) {
				t.Errorf("command %q has no parse case: add a case under conformance/corpus/script whose yaml uses it", name)
			}
			if !usesCommand(runCorpus, name) {
				t.Errorf("command %q has no run case: add a case under conformance/corpus/runner whose yaml uses it", name)
			}
			if !headings[name] {
				t.Errorf("command %q has no heading in docs/yml-script-reference.md: add a %q heading that documents it", name, "### "+name)
			}
		})
	}
}

// declaredStepCommands returns the value of every string constant in the
// script package whose name starts with the command prefix and whose
// declaration sits in the steps source file.
func declaredStepCommands(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), stepsSourcePath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", stepsSourcePath, err)
	}
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if !strings.HasPrefix(id.Name, commandConstPrefix) || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("constant %s: %v", id.Name, err)
				}
				names = append(names, value)
			}
		}
	}
	return names
}

func registeredCommands() map[string]bool {
	out := map[string]bool{}
	for _, def := range DefaultCommands() {
		out[def.Command()] = true
	}
	return out
}

// schemaCommandConsts collects every "const" string in the generated
// schema. A step definition pins its command name with a const, so the
// set contains every command the schema accepts.
func schemaCommandConsts(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode %s: %v", schemaPath, err)
	}
	out := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			if s, ok := node["const"].(string); ok {
				out[s] = true
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(doc)
	return out
}

// corpusYAML returns the yaml field of every case file in dir.
func corpusYAML(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no corpus cases under %s (err %v)", dir, err)
	}
	var out []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		var c struct {
			YAML string `json:"yaml"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("decode %s: %v", f, err)
		}
		out = append(out, c.YAML)
	}
	return out
}

// usesCommand reports whether any yaml document holds a step written as
// the bare command or as a mapping keyed by the command.
func usesCommand(docs []string, command string) bool {
	re := regexp.MustCompile(`(?m)^\s*(-\s+)?` + regexp.QuoteMeta(command) + `\s*(:|$)`)
	for _, doc := range docs {
		if re.MatchString(doc) {
			return true
		}
	}
	return false
}

func referenceHeadings(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(referenceDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", referenceDocPath, err)
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "### ") {
			out[strings.TrimSpace(strings.TrimPrefix(line, "### "))] = true
		}
	}
	return out
}
