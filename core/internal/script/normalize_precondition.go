package script

// Precondition normalization: turning a raw YAML precondition entry
// into its check-and-setup pair.

// normalizePreconditionSetup normalizes one precondition's inline
// `setup` list, the same way handle's inline `do` blocks are
// normalized. A file-path setup passes through unchanged.
func normalizePreconditionSetup(raw any) (*Precondition, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("preconditions: expected an object with `check` and `setup`")
	}
	check, err := normalizePreconditionCheck(m["check"])
	if err != nil {
		return nil, err
	}
	setup, err := normalizeDoValue(m["setup"])
	if err != nil {
		return nil, err
	}
	p := &Precondition{Check: *check, Setup: *setup}
	if v, present := m["policy"]; present && v != nil {
		s := jsString(v)
		p.Policy = &s
	}
	return p, nil
}

func normalizePreconditionCheck(raw any) (*PreconditionCheck, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("preconditions.check: expected an object")
	}
	check := &PreconditionCheck{}
	if v, present := m["visible"]; present && v != nil {
		sv, err := stringOrSelectorFromRaw(v)
		if err != nil {
			return nil, err
		}
		check.Visible = sv
	}
	if v, present := m["notVisible"]; present && v != nil {
		sv, err := stringOrSelectorFromRaw(v)
		if err != nil {
			return nil, err
		}
		check.NotVisible = sv
	}
	return check, nil
}
