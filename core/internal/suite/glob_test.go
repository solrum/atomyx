package suite

import (
	"regexp"
	"testing"
)

func TestMatchesGlobLiteralSegment(t *testing.T) {
	if !MatchesGlob("flows/smoke.yml", "flows/smoke.yml") {
		t.Errorf("exact literal path did not match")
	}
	if MatchesGlob("flows/smoke.yml", "flows/other.yml") {
		t.Errorf("different literal path matched")
	}
}

func TestMatchesGlobSingleStarStaysWithinSegment(t *testing.T) {
	if !MatchesGlob("flows/smoke.yml", "flows/*.yml") {
		t.Errorf("*.yml should match a file directly under flows/")
	}
	if MatchesGlob("flows/nested/smoke.yml", "flows/*.yml") {
		t.Errorf("* should not cross a path separator")
	}
}

func TestMatchesGlobDoubleStarCrossesSegments(t *testing.T) {
	if !MatchesGlob("flows/nested/deep/smoke.yml", "flows/**") {
		t.Errorf("flows/** should match an arbitrarily nested file")
	}
	if !MatchesGlob("flows/smoke.yml", "**/*.yml") {
		t.Errorf("**/*.yml should also match a file at the search root")
	}
	if !MatchesGlob("flows/nested/smoke.yml", "**/*.yml") {
		t.Errorf("**/*.yml should match a nested file too")
	}
	if MatchesGlob("flows/smoke.yaml", "**/*.yml") {
		t.Errorf("**/*.yml should not match a .yaml file")
	}
}

func TestMatchesGlobQuestionMarkMatchesOneCharacter(t *testing.T) {
	if !MatchesGlob("flows/a.yml", "flows/?.yml") {
		t.Errorf("? should match a single character")
	}
	if MatchesGlob("flows/ab.yml", "flows/?.yml") {
		t.Errorf("? should not match two characters")
	}
	if MatchesGlob("flows//.yml", "flows/?.yml") {
		t.Errorf("? should not cross a path separator")
	}
}

func TestMatchesGlobEscapesRegexMetacharacters(t *testing.T) {
	if !MatchesGlob("flows/a.b+c.yml", "flows/a.b+c.yml") {
		t.Errorf("literal '.' and '+' should be matched literally, not as regex metacharacters")
	}
	if MatchesGlob("flows/aXbXc.yml", "flows/a.b+c.yml") {
		t.Errorf("literal '.' and '+' should not behave as unescaped regex operators")
	}
}

func TestGlobMatcherMatchesEquivalentlyToMatchesGlob(t *testing.T) {
	matcher := NewGlobMatcher([]string{"flows/*.yml", "**/*.yaml"})
	if !matcher.MatchAny("flows/smoke.yml") {
		t.Errorf("MatchAny should match flows/smoke.yml against flows/*.yml")
	}
	if !matcher.MatchAny("nested/deep/notes.yaml") {
		t.Errorf("MatchAny should match nested/deep/notes.yaml against **/*.yaml")
	}
	if matcher.MatchAny("flows/nested/smoke.yml") {
		t.Errorf("MatchAny should not match a path none of its patterns cover")
	}
}

func TestNewGlobMatcherCompilesEachPatternExactlyOnce(t *testing.T) {
	previous := compileGlobPattern
	t.Cleanup(func() { compileGlobPattern = previous })

	compileCount := 0
	compileGlobPattern = func(pattern string) *regexp.Regexp {
		compileCount++
		return previous(pattern)
	}

	matcher := NewGlobMatcher([]string{"flows/*.yml", "**/*.yaml"})
	if compileCount != 2 {
		t.Fatalf("NewGlobMatcher compiled patterns %d times, want 2 (one per pattern)", compileCount)
	}

	for range 5 {
		matcher.MatchAny("flows/smoke.yml")
	}
	if compileCount != 2 {
		t.Fatalf("MatchAny recompiled a pattern: compileCount = %d after matching, want still 2", compileCount)
	}
}
