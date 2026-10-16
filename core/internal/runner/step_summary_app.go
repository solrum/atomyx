package runner

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/script"
)

// summarizeLaunchApp renders a launchApp step as its command name,
// followed by the options the step sets, for example
// `launchApp: clearState=true, stopApp=false`.
func summarizeLaunchApp(b *tokenBuilder, s *script.LaunchAppStep) {
	b.kw("launchApp")
	var options []string
	if s.ClearState != nil {
		options = append(options, fmt.Sprintf("clearState=%t", *s.ClearState))
	}
	if s.StopApp != nil {
		options = append(options, fmt.Sprintf("stopApp=%t", *s.StopApp))
	}
	if len(options) > 0 {
		b.punct(": ").rawStr(strings.Join(options, ", "))
	}
}

// summarizeAppStep renders an app lifecycle step as its command name,
// followed by the app it targets when the step names one.
func summarizeAppStep(b *tokenBuilder, command string, appID *string) {
	b.kw(command)
	if appID != nil {
		b.punct(": ").str(*appID)
	}
}
