package shellrun_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/shellrun"
)

// $SHELL is what the person chose; sh is what exists.
func TestShellFallsBackToSh(t *testing.T) {
	t.Setenv("SHELL", "")

	if got := shellrun.Shell(); got != "sh" {
		t.Errorf("Shell() = %q with $SHELL unset, want sh", got)
	}
}

// Each shell's flag skips its rc files a different way; -f is noglob on bash,
// so the name on the path is what picks the flag.
func TestArgvSkipsRcFiles(t *testing.T) {
	tests := []struct {
		shell string
		want  []string
	}{
		{"/bin/zsh", []string{"/bin/zsh", "-f", "-o", "no_prompt_sp"}},
		{"/opt/homebrew/bin/bash", []string{"/opt/homebrew/bin/bash", "--norc", "--noprofile"}},
		{"/usr/bin/fish", []string{"/usr/bin/fish", "--no-config"}},
		{"/bin/sh", []string{"/bin/sh"}},
		{"xonsh", []string{"xonsh"}},
	}

	for _, tc := range tests {
		t.Run(tc.shell, func(t *testing.T) {
			t.Setenv("SHELL", tc.shell)

			got := shellrun.Argv()
			if len(got) != len(tc.want) {
				t.Fatalf("Argv() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Argv() = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
