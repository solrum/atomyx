package config

import "gopkg.in/yaml.v3"

// decodeYAMLDocument parses raw YAML into a generic value tree ready
// for ValidateDocument: mappings become map[string]any, sequences
// become []any, and scalars become their natural Go type. ok is false
// when raw is not parseable YAML at all, or when its root is not a
// mapping. Both cases zod would report as a single invalid_type
// issue at the document root, so ValidateDocument treats them the
// same way rather than surfacing a distinct Go-side error.
func decodeYAMLDocument(raw []byte) (root map[string]any, environmentOrder []string, ok bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, false
	}
	if len(doc.Content) == 0 {
		return map[string]any{}, nil, true
	}
	rootNode := doc.Content[0]
	if rootNode.Kind != yaml.MappingNode {
		return nil, nil, false
	}

	value, err := decodeNode(rootNode)
	if err != nil {
		return nil, nil, false
	}
	decoded, isMap := value.(map[string]any)
	if !isMap {
		return nil, nil, false
	}
	return decoded, environmentsDeclarationOrder(rootNode), true
}

// environmentsDeclarationOrder reads the source-order key list of the
// top-level "environments" mapping directly from the YAML node tree,
// mirroring the order a JavaScript object built from the same YAML
// source would iterate via Object.keys(). It returns nil when
// "environments" is absent or is not itself a mapping; both are
// validation failures ValidateDocument reports on its own.
func environmentsDeclarationOrder(rootNode *yaml.Node) []string {
	for i := 0; i+1 < len(rootNode.Content); i += 2 {
		if rootNode.Content[i].Value != "environments" {
			continue
		}
		envNode := rootNode.Content[i+1]
		if envNode.Kind != yaml.MappingNode {
			return nil
		}
		order := make([]string, 0, len(envNode.Content)/2)
		for j := 0; j+1 < len(envNode.Content); j += 2 {
			order = append(order, envNode.Content[j].Value)
		}
		return order
	}
	return nil
}

// decodeNode decodes one YAML node into the same dynamic shapes a
// plain yaml.Node.Decode into an interface{} would produce for a
// scalar, except every mapping decodes into map[string]any instead of
// map[string]interface{} so ValidateDocument's type assertions have a
// single map type to match against.
func decodeNode(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		out := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			var key string
			if err := node.Content[i].Decode(&key); err != nil {
				return nil, err
			}
			value, err := decodeNode(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[key] = value
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(node.Content))
		for i, item := range node.Content {
			value, err := decodeNode(item)
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	default:
		var v any
		if err := node.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	}
}
