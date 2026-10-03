package runner

import (
	"fmt"
	"net/http"

	"dev.atomyx/internal/script"
)

// ScreenshotCommand captures a screenshot and stores it in the run's
// artifacts collector under the step's label, or "step-N" (1-indexed)
// when the step carries none.
type ScreenshotCommand struct{}

func (ScreenshotCommand) Command() string { return script.CommandScreenshot }

func (ScreenshotCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.ScreenshotStep)
	data := must(ctx.Orchestra.Screenshot(ctx.Context))

	label := fmt.Sprintf("step-%d", ctx.StepIndex+1)
	shownLabel := label
	if s.Label != nil {
		label = *s.Label
		shownLabel = *authored[*script.ScreenshotStep](step).Label
	}
	// Driver.Screenshot carries no mime type of its own (Android
	// encodes JPEG, iOS encodes PNG), so it is sniffed from the image's
	// own magic bytes rather than assumed from the active platform.
	ctx.Artifacts.AddScreenshot(label, data, http.DetectContentType(data))
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Screenshot saved as \"%s\"", shownLabel))}
}
