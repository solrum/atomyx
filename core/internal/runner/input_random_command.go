package runner

import (
	"fmt"
	"unicode/utf8"

	"dev.atomyx/internal/script"
)

// InputRandomCommand types a generated value into the field that
// currently has focus. The value comes from the run's RandomSource, so a
// seeded source makes it repeatable.
type InputRandomCommand struct{}

func (InputRandomCommand) Command() string { return script.CommandInputRandom }

func (InputRandomCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.InputRandomStep)
	length := script.DefaultRandomLength
	if s.Length != nil {
		length = *s.Length
	}
	value := generateRandomValue(s.Kind, length, ctx.Random)

	if err := ctx.Orchestra.TypeText(ctx.Context, value); err != nil {
		if failed, ok := commandFailureFor(err); ok {
			return failed
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf(
		"Typed a random %s of %d characters into focused field", s.Kind, utf8.RuneCountInString(value)))}
}
