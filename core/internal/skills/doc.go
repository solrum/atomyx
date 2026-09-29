// Package skills embeds the Atomyx skill and agent markdown files a
// consumer project installs under its own .claude/ directory, and
// copies them onto disk. The content under content/ is a byte-for-
// byte copy of the canonical source at packages/skills/content.
// TestEmbeddedContentMatchesCanonicalSource enforces that the two
// never drift apart.
package skills
