package script

import (
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// splitResult is the outcome of separating a script's config document
// from its steps document, mirroring splitDocs in yaml-parser.ts.
type splitResult struct {
	config    map[string]any
	rawSteps  []any
	stepLines []int
}

// splitScriptDocs parses yamlContent's one or two YAML documents into
// a config mapping and a raw step list, matching the two physical
// script forms parseScript accepts: a single document (a bare step
// sequence, or a mapping with a `steps` key), or two documents
// separated by `---` (config, then a step sequence).
func splitScriptDocs(yamlContent string) (*splitResult, error) {
	docs, err := decodeAllDocuments(yamlContent)
	if err != nil {
		return nil, newParseError("%s", err.Error())
	}
	if len(docs) == 0 {
		return nil, newParseError("Empty script: no YAML documents found")
	}

	if len(docs) == 1 {
		return splitSingleDoc(docs[0])
	}
	return splitTwoDocs(docs[0], docs[1])
}

func splitSingleDoc(root *yaml.Node) (*splitResult, error) {
	switch root.Kind {
	case yaml.SequenceNode:
		content, err := decodeGenericSequence(root)
		if err != nil {
			return nil, newParseError("decoding script steps: %s", err.Error())
		}
		return &splitResult{
			config:    map[string]any{},
			rawSteps:  content,
			stepLines: lineNumbers(root),
		}, nil

	case yaml.MappingNode:
		var content map[string]any
		if err := root.Decode(&content); err != nil {
			return nil, newParseError("decoding script config: %s", err.Error())
		}
		stepsNode := findMappingValue(root, "steps")
		if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
			return nil, newParseError(
				"Single-document script must have a 'steps' array, or use --- separator to split config from steps",
			)
		}
		steps, err := decodeGenericSequence(stepsNode)
		if err != nil {
			return nil, newParseError("decoding script steps: %s", err.Error())
		}
		return &splitResult{
			config:    content,
			rawSteps:  steps,
			stepLines: lineNumbers(stepsNode),
		}, nil

	default:
		return nil, newParseError("Script must be a YAML object with 'steps' key, or use --- separator")
	}
}

func splitTwoDocs(configRoot, stepsRoot *yaml.Node) (*splitResult, error) {
	config := map[string]any{}
	if configRoot.Kind == yaml.MappingNode {
		if err := configRoot.Decode(&config); err != nil {
			return nil, newParseError("decoding script config: %s", err.Error())
		}
	}

	if stepsRoot.Kind != yaml.SequenceNode {
		return nil, newParseError("Second YAML document (after ---) must be a list of steps")
	}
	steps, err := decodeGenericSequence(stepsRoot)
	if err != nil {
		return nil, newParseError("decoding script steps: %s", err.Error())
	}

	return &splitResult{
		config:    config,
		rawSteps:  steps,
		stepLines: lineNumbers(stepsRoot),
	}, nil
}

// decodeAllDocuments decodes every YAML document in content and
// returns each one's root node (a document's Content[0], not the
// wrapping DocumentNode).
func decodeAllDocuments(content string) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(content))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		root := &doc
		if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
			root = doc.Content[0]
		}
		docs = append(docs, root)
	}
	return docs, nil
}

// lineNumbers returns the 1-indexed source line of each item in a
// sequence node. The result becomes ScriptDefinition.StepLines
// (serialized as `_stepLines`), so a consumer can map a step's index
// back to the exact source line it was written on, for example to
// point an error at the right line.
func lineNumbers(seq *yaml.Node) []int {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	lines := make([]int, len(seq.Content))
	for i, item := range seq.Content {
		lines[i] = item.Line
	}
	return lines
}

func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
