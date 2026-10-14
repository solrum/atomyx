package cli

import "context"

// ModuleExecutor runs one top-level module's commands.
type ModuleExecutor func(ctx context.Context, deps Deps, args []string) int

// Modules lists the top-level modules this build registers.
var Modules = map[string]ModuleExecutor{
	"driver": ExecuteDriver,
	"skills": ExecuteSkills,
	"runs":   ExecuteRuns,
}

// Shortcut resolves a top-level shortcut command into the module and
// argv it dispatches to.
type Shortcut func(args []string) (module string, resolvedArgs []string)

// Shortcuts lists the top-level shortcuts this build registers.
var Shortcuts = map[string]Shortcut{
	"run": func(args []string) (string, []string) {
		return "driver", append([]string{"run"}, args...)
	},
	"suite": func(args []string) (string, []string) {
		return "driver", append([]string{"suite"}, args...)
	},
	"devices": func(args []string) (string, []string) {
		return "driver", append([]string{"list-devices"}, args...)
	},
	"doctor": func(args []string) (string, []string) {
		return "driver", append([]string{"doctor"}, args...)
	},
	"inspect": func(args []string) (string, []string) {
		return "driver", append([]string{"inspect"}, args...)
	},
	"setup": func(args []string) (string, []string) {
		return "driver", append([]string{"setup"}, args...)
	},
	"init": func(args []string) (string, []string) {
		return "skills", append([]string{"init"}, args...)
	},
	"update-skills": func(args []string) (string, []string) {
		return "skills", append([]string{"update-skills"}, args...)
	},
}
