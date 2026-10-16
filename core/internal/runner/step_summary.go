package runner

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"dev.atomyx/internal/script"
)

// StepTokenKind classifies one StepToken so a progress timeline can
// map it to a theme role for syntax highlighting.
//
//   - "keyword": command name and reserved words
//   - "identifier": selector field path, capture variable, and similar
//   - "string": literal user value (selector value, typed text, sleep duration)
//   - "punct": separators (":", "=", the left arrow, ",")
//   - "mask": a masked secret (bullet characters for a password field)
type StepTokenKind string

const (
	StepTokenKeyword    StepTokenKind = "keyword"
	StepTokenIdentifier StepTokenKind = "identifier"
	StepTokenString     StepTokenKind = "string"
	StepTokenPunct      StepTokenKind = "punct"
	StepTokenMask       StepTokenKind = "mask"
)

// typedSecretMask stands in for the text of an input step in a step
// summary. Every input renders the same eight bullet characters, so the
// summary reveals neither the content nor the length of what was typed.
const typedSecretMask = "••••••••"

// StepToken is one colored token in a step's rendered description.
type StepToken struct {
	Kind StepTokenKind
	Text string
}

// StepSummary is a step rendered into a colored token sequence plus a
// one-line plain-text fallback, suitable for a progress timeline.
type StepSummary struct {
	// Text is the plain-text fallback used by consumers that cannot
	// render colored tokens, built by concatenating every token's text
	// in order.
	Text string
	// Tokens is the colored token sequence.
	Tokens []StepToken
}

// SummarizeStep renders step into a colored token sequence plus
// plain-text fallback, suitable for a progress timeline. The summary
// text and the token sequence are a stable rendering contract: they
// must always agree with each other byte for byte. A step whose written
// form is lost renders as its command name alone.
func SummarizeStep(step script.ScriptStep) StepSummary {
	b := &tokenBuilder{}
	if script.WrittenFormLost(step) {
		return b.kw(step.StepCommand()).build()
	}
	step = script.AuthoredOf(step)
	switch s := step.(type) {
	case *script.TapStep:
		b.kw("tap").punct(": ").selector(&s.Selector)
	case *script.TypeStep:
		b.kw("input")
		if s.Into != nil {
			b.punct(": ").selector(s.Into).punct(" ← ")
		} else {
			b.punct(": ")
		}
		b.mask(typedSecretMask)
	case *script.WaitForStep:
		b.kw("waitFor").punct(": ").selector(&s.Selector)
		if s.TimeoutMs != nil {
			b.punct(" (").id("timeout").punct("=").str(fmt.Sprintf("%dms", *s.TimeoutMs)).punct(")")
		}
	case *script.AssertVisibleStep:
		b.kw("assertVisible").punct(": ").selector(&s.Selector)
	case *script.AssertNotVisibleStep:
		b.kw("assertNotVisible").punct(": ").selector(&s.Selector)
	case *script.AssertExistsStep:
		b.kw("assertExists").punct(": ").selector(&s.Selector)
	case *script.ScreenshotStep:
		b.kw("screenshot")
		if s.Label != nil {
			b.punct(": ").str(*s.Label)
		}
	case *script.SwipeStep:
		b.kw("swipe").punct(": ").str(s.Direction)
	case *script.PressKeyStep:
		b.kw("pressKey").punct(": ").str(s.Key)
	case *script.SleepStep:
		b.kw("sleep").punct(": ").str(fmt.Sprintf("%dms", s.Ms))
	case *script.BackStep:
		b.kw("back")
	case *script.LaunchAppStep:
		b.kw("launchApp")
	case *script.CaptureStep:
		b.kw("capture").punct(": ").id(s.As).punct(" ← ").str(s.Pattern)
	case *script.HandleStep:
		count := len(s.Branches)
		b.kw("handle").punct(": ").str(fmt.Sprintf("%d branch%s", count, pluralSuffix(count, "es")))
		if s.Timeout != nil {
			b.punct(" (").id("timeout").punct("=").str(fmt.Sprintf("%dms", *s.Timeout)).punct(")")
		}
	case *script.BranchStep:
		cases := len(s.On)
		b.kw("branch").punct(" on ").id("$" + s.From).
			punct(" (").str(fmt.Sprintf("%d case%s", cases, pluralSuffix(cases, "s"))).punct(")")
	case *script.RunFlowStep:
		b.kw("runFlow").punct(": ").str(s.File)
	case *script.RepeatStep:
		summarizeRepeat(b, s)
	case *script.AssertApiStep:
		b.kw("assertApi").punct(": ").id("$" + s.From)
		var parts []string
		if s.Status != nil {
			parts = append(parts, fmt.Sprintf("status=%d", *s.Status))
		}
		if s.Body != nil && s.Body.Len() > 0 {
			parts = append(parts, fmt.Sprintf("body×%d", s.Body.Len()))
		}
		if len(parts) > 0 {
			b.punct(" with ").rawStr(strings.Join(parts, ", "))
		}
	case *script.ExtractStep:
		valueKeys := s.Values.Keys()
		display := "?"
		if len(valueKeys) > 0 {
			display = strings.Join(valueKeys, ", ")
		}
		b.kw("extract").punct(": ").id("$" + s.From).punct(" into ").rawStr(display)
	default:
		b.kw(step.StepCommand())
	}
	return b.build()
}

// pluralSuffix renders suffix when count is not 1: "es" for "branch"
// versus "branches", "s" for "case" versus "cases".
func pluralSuffix(count int, suffix string) string {
	if count == 1 {
		return ""
	}
	return suffix
}

// tokenBuilder accumulates StepTokens for one step.
type tokenBuilder struct {
	tokens []StepToken
}

func (b *tokenBuilder) kw(text string) *tokenBuilder    { return b.push(StepTokenKeyword, text) }
func (b *tokenBuilder) id(text string) *tokenBuilder    { return b.push(StepTokenIdentifier, text) }
func (b *tokenBuilder) punct(text string) *tokenBuilder { return b.push(StepTokenPunct, text) }
func (b *tokenBuilder) mask(text string) *tokenBuilder  { return b.push(StepTokenMask, text) }

func (b *tokenBuilder) str(text string) *tokenBuilder {
	return b.push(StepTokenString, fmt.Sprintf("%q", jsTruncate(text, 48)))
}

func (b *tokenBuilder) rawStr(text string) *tokenBuilder {
	return b.push(StepTokenString, jsTruncate(text, 48))
}

// selector renders sel as "element.<field> = "<value>"", or
// "element.?" when sel is nil or carries none of the fields the
// runner's priority-broadened selectors compile from, in the same
// field-priority order those selectors resolve in: id, text, label,
// hint, role.
func (b *tokenBuilder) selector(sel *script.ScriptSelector) *tokenBuilder {
	if sel == nil {
		return b.id("element").punct(".").id("?")
	}
	field, value, ok := selectorFieldNameAndValue(sel)
	if !ok {
		return b.id("element").punct(".").id("?")
	}
	return b.id("element").punct(".").id(field).punct(" = ").str(value)
}

func (b *tokenBuilder) push(kind StepTokenKind, text string) *tokenBuilder {
	b.tokens = append(b.tokens, StepToken{Kind: kind, Text: text})
	return b
}

func (b *tokenBuilder) build() StepSummary {
	var sb strings.Builder
	for _, t := range b.tokens {
		sb.WriteString(t.Text)
	}
	return StepSummary{Text: sb.String(), Tokens: b.tokens}
}

// selectorFieldNameAndValue picks the first field a compiled selector
// would actually resolve to, in priority order: id, text, label,
// hint, role. This keeps the rendered summary showing the field the
// selector really matches on, rather than an arbitrary field that
// happens to be set.
func selectorFieldNameAndValue(sel *script.ScriptSelector) (field string, value string, ok bool) {
	switch {
	case sel.IDRegex != nil:
		return "id", sel.IDRegex.Regex, true
	case sel.ID != nil:
		return "id", *sel.ID, true
	case sel.TextRegex != nil:
		return "text", sel.TextRegex.Regex, true
	case sel.Text != nil:
		return "text", *sel.Text, true
	case sel.LabelRegex != nil:
		return "label", sel.LabelRegex.Regex, true
	case sel.Label != nil:
		return "label", *sel.Label, true
	case sel.HintRegex != nil:
		return "hint", sel.HintRegex.Regex, true
	case sel.Hint != nil:
		return "hint", *sel.Hint, true
	case sel.Role != nil:
		return "role", *sel.Role, true
	default:
		return "", "", false
	}
}

// jsTruncate truncates value to at most max UTF-16 code units, leaving it unchanged when it
// already fits. A longer value keeps its first max-1 code units and
// gets an ellipsis appended, so the truncated result still occupies
// no more than max units in total.
func jsTruncate(value string, max int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= max {
		return value
	}
	return string(utf16.Decode(units[:max-1])) + "…"
}

// summarizeRepeat renders a repeat step as its loop bounds, for example
// `repeat: 3 times` or `repeat: while visible element.text = "More"`.
func summarizeRepeat(b *tokenBuilder, s *script.RepeatStep) {
	b.kw("repeat").punct(": ")
	if s.Times != nil {
		b.rawStr(fmt.Sprintf("%d %s", *s.Times, pluralWord(*s.Times, "time")))
		if s.While != nil {
			b.punct(", ")
		}
	}
	if s.While == nil {
		return
	}
	b.id("while").punct(" ")
	switch {
	case s.While.Visible != nil:
		b.id("visible").punct(" ").selector(selectorOf(*s.While.Visible))
	case s.While.NotVisible != nil:
		b.id("notVisible").punct(" ").selector(selectorOf(*s.While.NotVisible))
	case s.While.Expr != nil:
		b.str(*s.While.Expr)
	}
}

func selectorOf(v script.StringOrSelector) *script.ScriptSelector {
	sel := toScriptSelector(v)
	return &sel
}
