package runner

import (
	"testing"

	"dev.atomyx/internal/script"
)

func TestSummarizeStep_InputMaskHasOneWidthForEveryText(t *testing.T) {
	id := "pw"
	short := SummarizeStep(&script.TypeStep{Text: "a", Into: &script.ScriptSelector{ID: &id}})
	long := SummarizeStep(&script.TypeStep{Text: "a-much-longer-passphrase-than-twelve", Into: &script.ScriptSelector{ID: &id}})

	if short.Text != long.Text {
		t.Fatalf("summaries differ by text length: %q versus %q", short.Text, long.Text)
	}
	last := short.Tokens[len(short.Tokens)-1]
	if last.Kind != StepTokenMask || last.Text != typedSecretMask {
		t.Fatalf("last token = %+v, want the fixed mask", last)
	}
}
