package tui_test

import (
	"os"
	"testing"

	"github.com/kyleking/aragonite/hermetic"
)

// TestMain clears what icon detection reads, so the goldens draw the plain set
// and name it as auto's fallback in whatever terminal runs them, and runs the
// tests in a home of their own, failing the run if one reaches the real one.
func TestMain(m *testing.M) {
	unsetIconDetection()
	hermetic.Main(m, hermetic.App("second-look"))
}

// unsetIconDetection removes every variable aragonite's icons.Explain reads to
// pick a set, and a TERM that names a Nerd-bundling terminal.
func unsetIconDetection() {
	for _, v := range []string{
		"ARAGONITE_ICONS", "TERM_PROGRAM", "WEZTERM_PANE", "WEZTERM_EXECUTABLE",
		"KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR",
	} {
		if err := os.Unsetenv(v); err != nil {
			panic(err)
		}
	}

	if t := os.Getenv("TERM"); t == "xterm-kitty" || t == "xterm-ghostty" {
		if err := os.Setenv("TERM", "xterm-256color"); err != nil {
			panic(err)
		}
	}
}
