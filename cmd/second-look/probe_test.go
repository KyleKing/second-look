package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/diag"
	"github.com/kyleking/second-look/internal/diff"
)

// The wiring is what this covers: a review's own blob reader, the commit it
// reads at, and a real language server started in the checkout. The protocol
// itself is covered against a stub in internal/diag/lsp, and the placement in
// internal/diag; what is only true here is that the three fit together.
//
// It runs against gopls because this repository is Go and gopls is what its
// checks already need. A laptop without it skips, the same way the review
// screen offers nothing there.
func TestProberAnswersFromARealServer(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}

	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), []byte("module probe\n\ngo 1.25\n"))

	// The error is one no diff can see: the field is not a member of the type,
	// which is the question the review screen exists to stop leaving for.
	write(t, filepath.Join(root, "a.go"), []byte(`package probe
type Row struct {
	Name string
}
func Archived(r Row) bool {
	return r.IsArchived
}
`))

	sha := commitAll(t, root)

	d := diff.Parse([]byte(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,6 +1,7 @@
 package probe
 type Row struct {
 	Name string
 }
 func Archived(r Row) bool {
+	return r.IsArchived
 }
`))

	p := main.CheckerFor(t.Context(), root, d, blob.Reader{Work: root, SHA: sha})
	if p == nil {
		t.Fatal("a Go checkout with gopls installed offered no checker")
	}

	defer p.Close()

	notes, err := p.Notes(t.Context())
	if err != nil {
		t.Fatalf("checking: %v", err)
	}

	said := make([]string, 0, len(notes))
	for _, n := range notes {
		said = append(said, n.Path+": "+n.Message)
	}

	if !slices.ContainsFunc(said, func(s string) bool { return strings.Contains(s, "IsArchived") }) {
		t.Fatalf("nothing said the field is not a member; gopls said %v", said)
	}

	// The same warm session answers where a name lives: r on the return line
	// was declared as the parameter two lines above it.
	jump, err := p.Jumps(t.Context(), "a.go", 6, len("\treturn "), diag.Definitions)
	if err != nil {
		t.Fatalf("asking where the name lives: %v", err)
	}

	lives := make([]string, 0, len(jump.At))
	for _, s := range jump.At {
		lives = append(lives, jump.Name+"→"+s.Path+":"+strconv.Itoa(s.Line))
	}

	if !slices.Contains(lives, "r→a.go:5") {
		t.Fatalf("r was not traced to its parameter; gopls said %v", lives)
	}
}

// A call graph asked from a line inside the function is the one asked from its
// name, drawn from the commit's text, and a callee in the standard library keeps
// the absolute path the server gave.
func TestProberReadsACallGraphFromARealServer(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}

	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), []byte("module probe\n\ngo 1.25\n"))
	write(t, filepath.Join(root, "a.go"), []byte(`package probe

import "strings"

func leaf(s string) string { return s }

func middle(s string) string {
	leaf(s)
	_ = strings.TrimSpace(s)
	return leaf(s + "!")
}

func top() string { return middle("x") }
`))

	sha := commitAll(t, root)

	d := diff.Parse([]byte(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -5,3 +5,3 @@
 func leaf(s string) string { return s }

+func top() string { return middle("x") }
`))

	p := main.CheckerFor(t.Context(), root, d, blob.Reader{Work: root, SHA: sha})
	if p == nil {
		t.Fatal("a Go checkout with gopls installed offered no checker")
	}

	defer p.Close()

	if name, ok := p.Server("a.go"); !ok || name != "gopls" {
		t.Fatalf("the server for a Go file is %q, %v", name, ok)
	}

	g, err := p.Calls(t.Context(), "a.go", 10)
	if err != nil {
		t.Fatalf("asking for the call graph: %v", err)
	}

	if !strings.Contains(g.Center.Name, "middle") || g.Center.Path != "a.go" || g.Center.Line != 7 {
		t.Errorf("the center is %+v, want middle at a.go:7", g.Center)
	}

	if len(g.Callers) != 1 || !strings.Contains(g.Callers[0].Name, "top") {
		t.Errorf("the callers are %+v, want top", g.Callers)
	}

	sites, outside := calleeSites(g)
	if !slices.Equal(sites["leaf"], []int{8, 10}) || !outside["TrimSpace"] || outside["leaf"] {
		t.Errorf("the callees are %+v, want leaf at lines 8 and 10 and TrimSpace outside the checkout", g.Callees)
	}
}

// calleeSites is the lines each callee is called on, and whether its definition
// is outside the checkout.
func calleeSites(g diag.Graph) (map[string][]int, map[string]bool) {
	sites, outside := map[string][]int{}, map[string]bool{}

	for _, c := range g.Callees {
		for _, s := range c.Sites {
			sites[c.Name] = append(sites[c.Name], s.Line)
		}

		outside[c.Name] = filepath.IsAbs(c.Path)
	}

	return sites, outside
}

// commit puts the tree in the object store, since a review reads the commit
// rather than the working copy.
func commitAll(t *testing.T, root string) string {
	t.Helper()

	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-qm", "probe"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...) //#nosec G204 -- constants
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}

	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = root

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("reading the head: %v", err)
	}

	return strings.TrimSpace(string(out))
}
