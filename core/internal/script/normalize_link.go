package script

// normalizeOpenLink accepts the link as a bare string, or an object with
// a required `link` and an optional `appId` naming the app that opens it.
func normalizeOpenLink(value any) (ScriptStep, error) {
	if text, ok := asString(value); ok {
		return &OpenLinkStep{Command: CommandOpenLink, Link: text}, nil
	}
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("openLink: expected a link string or an object with `link` and `appId`, got %s", goType(value))
	}
	rawLink, present := m["link"]
	if !present {
		return nil, newParseError("openLink: the object form needs a `link`")
	}
	link, ok := asString(rawLink)
	if !ok {
		return nil, newParseError("openLink.link: must be a string, got %s", goType(rawLink))
	}
	step := &OpenLinkStep{Command: CommandOpenLink, Link: link}
	if raw, present := m["appId"]; present && raw != nil {
		appID, ok := asString(raw)
		if !ok {
			return nil, newParseError("openLink.appId: must be a string, got %s", goType(raw))
		}
		step.AppID = &appID
	}
	return step, nil
}
