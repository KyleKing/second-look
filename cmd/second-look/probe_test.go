package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
	"github.com/kyleking/second-look/internal/blob"
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
@@ -1,8 +1,8 @@
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

	var said []string
	for _, n := range notes {
		said = append(said, n.Path+": "+n.Message)

		if strings.Contains(n.Message, "IsArchived") {
			return
		}
	}

	t.Fatalf("nothing said the field is not a member; gopls said %v", said)
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
