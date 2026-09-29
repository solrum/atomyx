package skills

import "embed"

// contentFS holds the bundled skill and agent files under content/,
// laid out as content/skills/<name> and content/agents/<name>.
//
//go:embed content
var contentFS embed.FS

// contentRoot is contentFS's root directory, matching the directory
// name the go:embed directive above names.
const contentRoot = "content"
