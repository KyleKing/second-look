package shellrun_test

import (
	"slices"
	"testing"

	"github.com/kyleking/second-look/internal/shellrun"
)

// $SHELL is what the person chose and sh is what exists. Each shell's flag
// skips its rc files a different way; -f is noglob on bash, so the name on the
// path is what picks the flag. It sets $SHELL, so it cannot run in parallel.
func TestArgvSkipsRcFiles(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{"", []string{"sh"}},
		{"/bin/zsh", []string{"/bin/zsh", "-f", "-o", "no_prompt_sp"}},
		{"/opt/homebrew/bin/bash", []string{"/opt/homebrew/bin/bash", "--norc", "--noprofile"}},
		{"/usr/bin/fish", []string{"/usr/bin/fish", "--no-config"}},
		{"/bin/sh", []string{"/bin/sh"}},
		{"xonsh", []string{"xonsh"}},
	}

	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			t.Setenv("SHELL", tc.shell)

			if got := shellrun.Shell(); got != tc.want[0] {
				t.Errorf("Shell() = %q, want %q", got, tc.want[0])
			}

			if got := shellrun.Argv(); !slices.Equal(got, tc.want) {
				t.Errorf("Argv() = %v, want %v", got, tc.want)
			}
		})
	}
}
