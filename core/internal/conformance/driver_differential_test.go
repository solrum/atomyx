package conformance

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
)

func TestDriverDifferential(t *testing.T) {
	cases, err := LoadDriverCorpus("corpus/driver")
	if err != nil {
		t.Fatalf("loading driver corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			goOutput, err := goDriverOutput(c)
			if err != nil {
				t.Fatalf("computing Go output: %v", err)
			}

			checkGolden(t, filepath.Join("driver", c.Name+".json"), goOutput, DiffJSON)
		})
	}
}

// goDriverOutput runs the Go equivalent of c.Function and marshals
// the result to the same JSON shape the frozen golden fixture holds,
// so DiffJSON can compare them directly.
func goDriverOutput(c *DriverCase) ([]byte, error) {
	switch c.Function {
	case "compileSelector", "find-in-tree", "detectObscurement":
		var selector orchestra.Selector
		if err := json.Unmarshal(c.Selector, &selector); err != nil {
			return nil, fmt.Errorf("decoding selector: %w", err)
		}
		var tree wire.TreeNodeWire
		if err := json.Unmarshal(c.Tree, &tree); err != nil {
			return nil, fmt.Errorf("decoding tree: %w", err)
		}
		filter := orchestra.CompileSelector(selector)

		switch c.Function {
		case "compileSelector":
			matches := filter(orchestra.FromTree(tree))
			nodes := make([]wire.TreeNodeWire, len(matches))
			for i, cur := range matches {
				nodes[i] = cur.Node
			}
			return json.Marshal(nodes)
		case "find-in-tree":
			resolved, found := orchestra.ResolveFirst(filter, tree)
			if !found {
				return json.Marshal(struct {
					Found bool `json:"found"`
				}{Found: false})
			}
			return json.Marshal(struct {
				Found  bool              `json:"found"`
				Node   wire.TreeNodeWire `json:"node"`
				Bounds *wire.Bounds      `json:"bounds"`
				Point  *wire.Point       `json:"point"`
			}{
				Found:  true,
				Node:   resolved.Cursor.Node,
				Bounds: resolved.Bounds,
				Point:  resolved.Point,
			})
		default: // detectObscurement
			target, found := orchestra.FindOne(filter, tree)
			if !found {
				return json.Marshal(struct {
					Found bool `json:"found"`
				}{Found: false})
			}
			result := orchestra.DetectObscurement(tree, target)
			return json.Marshal(struct {
				Found  bool                        `json:"found"`
				Result orchestra.ObscurementResult `json:"result"`
			}{Found: true, Result: result})
		}
	case "swipeDirection":
		var screen wire.Size
		if err := json.Unmarshal(c.Screen, &screen); err != nil {
			return nil, fmt.Errorf("decoding screen: %w", err)
		}
		var direction orchestra.Direction
		switch c.Direction {
		case "up":
			direction = orchestra.Up
		case "down":
			direction = orchestra.Down
		case "left":
			direction = orchestra.Left
		case "right":
			direction = orchestra.Right
		default:
			return nil, fmt.Errorf("unknown direction %q", c.Direction)
		}
		from, to := orchestra.ComputeSwipeDirectionEndpoints(screen, direction, *c.Fraction)
		return json.Marshal(struct {
			From wire.Point `json:"from"`
			To   wire.Point `json:"to"`
		}{From: from, To: to})
	case "hashVisibleLeaves":
		var tree wire.TreeNodeWire
		if err := json.Unmarshal(c.Tree, &tree); err != nil {
			return nil, fmt.Errorf("decoding tree: %w", err)
		}
		return json.Marshal(orchestra.HashVisibleLeaves(tree))
	case "bucketBounds":
		var bounds string
		if err := json.Unmarshal(c.Bounds, &bounds); err != nil {
			return nil, fmt.Errorf("decoding bounds: %w", err)
		}
		return json.Marshal(orchestra.BucketBounds(bounds))
	case "scrollTowardElement":
		var bounds wire.Bounds
		if err := json.Unmarshal(c.Bounds, &bounds); err != nil {
			return nil, fmt.Errorf("decoding bounds: %w", err)
		}
		var screen wire.Size
		if err := json.Unmarshal(c.Screen, &screen); err != nil {
			return nil, fmt.Errorf("decoding screen: %w", err)
		}
		from, to := orchestra.ComputeScrollTowardElementEndpoints(bounds, screen, *c.MaxScrollFraction)
		return json.Marshal(struct {
			From wire.Point `json:"from"`
			To   wire.Point `json:"to"`
		}{From: from, To: to})
	case "swipeCenter":
		var screen wire.Size
		if err := json.Unmarshal(c.Screen, &screen); err != nil {
			return nil, fmt.Errorf("decoding screen: %w", err)
		}
		from, to := orchestra.ComputeSwipeCenterEndpoints(screen, c.Direction, *c.MaxScrollFraction)
		return json.Marshal(struct {
			From wire.Point `json:"from"`
			To   wire.Point `json:"to"`
		}{From: from, To: to})
	case "clamp":
		return json.Marshal(orchestra.ClampF(*c.Value, *c.Min, *c.Max))
	default:
		panic("unreachable: DriverCase validates function during load")
	}
}
