// Package shellrun keeps what a shell session printed.
//
// It exists for one motion: run the code under review in the pane, then attach
// what it printed to the comment about it. A citation says where to look and a
// transcript says what actually happened, and only the second one survives a
// disagreement.
package shellrun

import "os"

// Shell is the shell to run, from $SHELL.
func Shell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}

	return "sh"
}
