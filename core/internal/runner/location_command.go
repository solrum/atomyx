package runner

import (
	"fmt"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/script"
)

// SetLocationCommand moves the device to the step's latitude and
// longitude. The position persists after the script, so a script that
// moves the device sets it back before it ends.
type SetLocationCommand struct{}

func (SetLocationCommand) Command() string { return script.CommandSetLocation }

func (SetLocationCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.SetLocationStep)
	location := driver.Location{Latitude: s.Latitude, Longitude: s.Longitude}
	if err := ctx.Orchestra.SetLocation(ctx.Context, location); err != nil {
		if result, ok := commandFailureFor(err); ok {
			return result
		}
		panic(err)
	}
	return CommandResult{Ok: true, Detail: strPtr("Moved to " + describeLocation(location))}
}

// TravelCommand moves the device through the step's points at the
// step's speed and leaves it at the last point. The step succeeds once
// the last point is set.
type TravelCommand struct{}

func (TravelCommand) Command() string { return script.CommandTravel }

func (TravelCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.TravelStep)
	if err := ctx.Orchestra.Travel(ctx.Context, s.Points, s.Speed); err != nil {
		if result, ok := commandFailureFor(err); ok {
			return result
		}
		panic(err)
	}
	last := s.Points[len(s.Points)-1]
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Traveled through %d points to %s", len(s.Points), describeLocation(last)))}
}

// describeLocation renders a position as `latitude, longitude`.
func describeLocation(l driver.Location) string {
	return fmt.Sprintf("%v, %v", l.Latitude, l.Longitude)
}

// describeTravel renders a travel as its point count and speed.
func describeTravel(s *script.TravelStep) string {
	return fmt.Sprintf("%d points at %v m/s", len(s.Points), s.Speed)
}
