package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DriverCase is one differential selector-engine case read from a
// corpus file under corpus/driver/. The corpus file format is
// documented in corpus/format.md. Which fields are required depends
// on Function; see loadDriverCase's validation switch for the exact
// per-function requirements.
type DriverCase struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Function          string          `json:"function"`
	Selector          json.RawMessage `json:"selector,omitempty"`
	Tree              json.RawMessage `json:"tree,omitempty"`
	Screen            json.RawMessage `json:"screen,omitempty"`
	Direction         string          `json:"direction,omitempty"`
	Fraction          *float64        `json:"fraction,omitempty"`
	Bounds            json.RawMessage `json:"bounds,omitempty"`
	MaxScrollFraction *float64        `json:"maxScrollFraction,omitempty"`
	TargetPath        []int           `json:"targetPath,omitempty"`
	Value             *float64        `json:"value,omitempty"`
	Min               *float64        `json:"min,omitempty"`
	Max               *float64        `json:"max,omitempty"`
}

var selectorAndTreeFunctions = map[string]bool{
	"compileSelector":   true,
	"find-in-tree":      true,
	"detectObscurement": true,
}

var scrollCenterDirections = map[string]bool{"up": true, "down": true}

// loadDriverCase reads and validates a single driver corpus case
// file.
func loadDriverCase(path string) (*DriverCase, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading driver corpus case %s: %w", path, err)
	}
	var c DriverCase
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parsing driver corpus case %s: %w", path, err)
	}
	if c.Name == "" {
		return nil, fmt.Errorf("driver corpus case %s: missing name", path)
	}
	switch {
	case selectorAndTreeFunctions[c.Function]:
		if len(c.Selector) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing selector", path)
		}
		if len(c.Tree) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing tree", path)
		}
	case c.Function == "suggestSelectors":
		if len(c.Tree) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing tree", path)
		}
		if c.TargetPath == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing targetPath", path)
		}
	case c.Function == "hashVisibleLeaves":
		if len(c.Tree) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing tree", path)
		}
	case c.Function == "bucketBounds":
		if len(c.Bounds) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing bounds", path)
		}
	case c.Function == "swipeDirection":
		if len(c.Screen) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing screen", path)
		}
		if c.Direction == "" {
			return nil, fmt.Errorf("driver corpus case %s: missing direction", path)
		}
		if c.Fraction == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing fraction", path)
		}
	case c.Function == "scrollTowardElement":
		if len(c.Bounds) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing bounds", path)
		}
		if len(c.Screen) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing screen", path)
		}
		if c.MaxScrollFraction == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing maxScrollFraction", path)
		}
	case c.Function == "swipeCenter":
		if len(c.Screen) == 0 {
			return nil, fmt.Errorf("driver corpus case %s: missing screen", path)
		}
		if !scrollCenterDirections[c.Direction] {
			return nil, fmt.Errorf("driver corpus case %s: direction must be \"up\" or \"down\", got %q", path, c.Direction)
		}
		if c.MaxScrollFraction == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing maxScrollFraction", path)
		}
	case c.Function == "clamp":
		if c.Value == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing value", path)
		}
		if c.Min == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing min", path)
		}
		if c.Max == nil {
			return nil, fmt.Errorf("driver corpus case %s: missing max", path)
		}
	default:
		return nil, fmt.Errorf(
			"driver corpus case %s: function must be one of \"compileSelector\", \"find-in-tree\", \"detectObscurement\", \"hashVisibleLeaves\", \"bucketBounds\", \"swipeDirection\", \"scrollTowardElement\", \"swipeCenter\", \"clamp\", \"suggestSelectors\", got %q",
			path, c.Function,
		)
	}
	return &c, nil
}

// LoadDriverCorpus reads every *.json case file directly under dir,
// sorted by filename for deterministic execution order.
func LoadDriverCorpus(dir string) ([]*DriverCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading driver corpus directory %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	cases := make([]*DriverCase, 0, len(names))
	for _, name := range names {
		c, err := loadDriverCase(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, nil
}
