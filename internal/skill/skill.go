// Package skill holds the agent-facing instructions for driving second-look.
//
// They are embedded rather than published separately so the binary and its
// instructions ship together, and the schema itself stays in `--help`, which
// the code that enforces it lives beside.
package skill

import _ "embed"

// Content is the skill file, ready to write to a skills directory as it is.
//
//go:embed SKILL.md
var Content string
