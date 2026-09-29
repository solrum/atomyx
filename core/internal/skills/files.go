package skills

// SkillFiles lists the skill markdown files copied into
// <target>/skills, in the order they are copied and printed.
var SkillFiles = []string{
	"atomyx-test-loop.md",
	"atomyx-debug-failure.md",
	"atomyx-script-authoring.md",
}

// AgentFiles lists the agent markdown files copied into
// <target>/agents, in the order they are copied and printed.
var AgentFiles = []string{
	"atomyx-explorer.md",
	"atomyx-replayer.md",
}

// AllFiles lists every bundled file, skills before agents, in the
// order CopyTo writes them and the order init/update-skills print
// them.
func AllFiles() []string {
	all := make([]string, 0, len(SkillFiles)+len(AgentFiles))
	all = append(all, SkillFiles...)
	all = append(all, AgentFiles...)
	return all
}
