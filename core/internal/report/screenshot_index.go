package report

import (
	"fmt"
	"regexp"
	"strings"
)

// RootScriptScope is the scope every ReportScreenshot for a
// standalone script run carries, since there is only one script in
// the result and no position to key on.
const RootScriptScope = ""

// ScenarioChildScope builds the scope for one scenario child script,
// unique across children that share the same scriptPath because it
// carries the child's position in the scenario's scripts list. Pass
// parentScope as RootScriptScope for a standalone scenario run, or as
// the owning suite entry's own scope (see SuiteEntryScope) for a
// scenario nested inside a suite, so the child's scope stays distinct
// from a same-path child in a sibling entry. This is the single
// source of truth for the scope format: both the producer building
// ReportScreenshot values and BuildScreenshotIndex's consumer side
// must key screenshots identically, so every caller on either side
// calls this function rather than reimplementing its format.
func ScenarioChildScope(parentScope string, scriptIndex int, scriptPath string) string {
	own := fmt.Sprintf("%d::%s", scriptIndex, scriptPath)
	if parentScope != "" {
		return parentScope + "::" + own
	}
	return own
}

// SuiteEntryScope builds the scope for one suite entry, unique across
// entries that share the same path because it carries the entry's
// position in the suite's entry list. See ScenarioChildScope for why
// this is the single source of truth for the format.
func SuiteEntryScope(entryIndex int, path string) string {
	return fmt.Sprintf("%d::%s", entryIndex, path)
}

// ScreenshotRef is a screenshot's resolved location inside a written
// report bundle: its owning scope, its original label, and the file
// name it is written under, relative to the bundle's screenshots
// directory.
type ScreenshotRef struct {
	Scope string
	Label string
	File  string
}

// ScreenshotsByScope groups every ScreenshotRef for a run by owning
// scope.
type ScreenshotsByScope map[string][]ScreenshotRef

// ScreenshotIndex is the full screenshot index for a run: the flat,
// filename-assigned list and its grouping by scope.
type ScreenshotIndex struct {
	Refs    []ScreenshotRef
	ByScope ScreenshotsByScope
}

// BuildScreenshotIndex assigns every screenshot a stable,
// filesystem-safe file name and groups the results by owning scope.
// The name is derived from the screenshot's position in the slice,
// so every caller that receives the same ReportInput screenshots
// produces the same file name for the same screenshot, without any
// caller needing to coordinate with another. Compute this once per
// run and pass the result into a Reporter and every renderer, rather
// than having each one recompute it.
func BuildScreenshotIndex(screenshots []ReportScreenshot) ScreenshotIndex {
	refs := make([]ScreenshotRef, len(screenshots))
	for i, s := range screenshots {
		refs[i] = ScreenshotRef{Scope: s.Scope, Label: s.Label, File: screenshotFileName(s, i)}
	}
	byScope := make(ScreenshotsByScope)
	for _, ref := range refs {
		byScope[ref.Scope] = append(byScope[ref.Scope], ref)
	}
	return ScreenshotIndex{Refs: refs, ByScope: byScope}
}

var nonAlphanumericRun = regexp.MustCompile(`[^a-z0-9]+`)

func screenshotFileName(screenshot ReportScreenshot, index int) string {
	base := screenshot.Label
	if screenshot.Scope != "" {
		base = screenshot.Scope + "-" + screenshot.Label
	}
	base = sanitizeSegment(base)
	if base == "" {
		base = "screenshot"
	}
	return fmt.Sprintf("%02d-%s%s", index+1, base, extensionForMimeType(screenshot.MimeType))
}

// extensionForMimeType maps a screenshot's MIME type to the file
// extension its report bundle entry is written under. An empty or
// unrecognized MIME type falls back to ".png": every screenshot
// recorded before MimeType existed carried PNG bytes, so treating
// "unknown" as PNG keeps those report bundles' file names unchanged.
func extensionForMimeType(mimeType string) string {
	if mimeType == "image/jpeg" {
		return ".jpg"
	}
	return ".png"
}

func sanitizeSegment(value string) string {
	slug := nonAlphanumericRun.ReplaceAllString(strings.ToLower(value), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	return slug
}
