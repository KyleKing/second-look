// Package shellrun keeps what a shell session printed.
//
// It exists for one motion: run the code under review in the pane, then attach
// what it printed to the comment about it. A citation says where to look and a
// transcript says what actually happened, and only the second one survives a
// disagreement.
package shellrun

import (
	"os"
	"path/filepath"
)

// Shell is the shell to run, from $SHELL.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}

	return "sh"
}

// Argv runs the user's shell with its rc files skipped: a prompt that
// repaints itself leaves its decoration on every transcript line, and an rc
// that asks something interactive (mise trust) hijacks the pane. The
// parent's environment still applies, so PATH resolves the same.
func Argv() []string {
	s := Shell()

	switch filepath.Base(s) {
	case "bash":
		return []string{s, "--norc", "--noprofile"}
	case "fish":
		return []string{s, "--no-config"}
	case "zsh":
		// PROMPT_SP is the % -padding zsh repaints over an unterminated line;
		// in a transcript it reads as residue around every prompt.
		return []string{s, "-f", "-o", "no_prompt_sp"}
	default:
		return []string{s}
	}
}
