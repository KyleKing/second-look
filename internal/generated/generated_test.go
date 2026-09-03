package generated_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/generated"
)

func TestMatchKnowsWhatAMachineWrote(t *testing.T) {
	t.Parallel()

	set := generated.New([]string{"api/schema/", ".gen.ts"})

	tests := []struct {
		path string
		want bool
	}{
		{"uv.lock", true},
		{"services/api/uv.lock", true},
		{"go.sum", true},
		{"internal/rpc/user.pb.go", true},
		{"web/vendor/lib/x.js", true},
		{"vendor/x.go", true},
		{"web/__snapshots__/App.test.js.snap", true},
		{"api/schema/types.json", true},
		{"web/src/client.gen.ts", true},

		{"internal/rate/rate.go", false},
		{"go.mod", false},
		{"README.md", false},
		// A directory pattern matches a path segment, not a prefix of one.
		{"vendored/x.go", false},
		{"internal/vendoring/x.go", false},
		// The suffix has to end the path, not sit inside it.
		{"uv.lock.bak", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			if got := set.Match(tc.path); got != tc.want {
				t.Errorf("Match(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// The config adds to the built-in patterns rather than replacing them: a
// monorepo naming its own generated tree does not stop a lockfile being one.
func TestConfiguredPatternsAddRatherThanReplace(t *testing.T) {
	t.Parallel()

	set := generated.New([]string{"build/"})

	if !set.Match("uv.lock") {
		t.Error("naming a pattern dropped the built-in ones")
	}

	if !set.Match("build/out.js") {
		t.Error("the configured pattern does not match")
	}
}
