package main_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/ghcassette"

	"github.com/kyleking/second-look/internal/artifact"
)

// TestReviewWithNoCheckout is the whole point of naming a repository: a pull
// request is prepared, read back, and guarded from a directory that is not a
// checkout of anything, with its state in the user state directory.
//
// Every gh call it makes carries --repo, which is what the recording holds,
// because there is no working directory for gh to read a repository off.
func TestReviewWithNoCheckout(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, awayCassette(t))
	home := t.TempDir()
	env := homeEnv(home)

	res := runCLIEnv(t, s, t.TempDir(), env, "get", "KyleKing/second-look#2")
	if res.code != 0 {
		t.Fatalf("get failed: %s%s", res.stdout, res.stderr)
	}

	store := storeIn(t, home)

	if _, err := os.Stat(artifact.Path(store, 2)); err != nil {
		t.Fatalf("the prepared review is not in the state directory: %v", err)
	}

	if _, err := os.Stat(artifact.DiffPath(store, fixtureHeadSHA)); err != nil {
		t.Fatalf("the diff was not cached in the state directory: %v", err)
	}

	if !strings.Contains(res.stdout, artifact.Path(store, 2)) {
		t.Errorf("get never said where the review is:\n%s", res.stdout)
	}

	// A different working directory again, because a review kept outside a
	// checkout has to be reachable from anywhere rather than from where it was
	// prepared.
	res = runCLIEnv(t, s, t.TempDir(), env, "show", "KyleKing/second-look#2")
	if res.code != 0 {
		t.Fatalf("show failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, `"head_sha": "`+fixtureHeadSHA) {
		t.Errorf("show read something other than the staged review:\n%s", res.stdout)
	}

	res = runCLIEnv(t, s, t.TempDir(), env, "reviews", "--json")
	if res.code != 0 {
		t.Fatalf("reviews failed: %s%s", res.stdout, res.stderr)
	}

	if !strings.Contains(res.stdout, "KyleKing/second-look") {
		t.Errorf("reviews never listed the review with no checkout:\n%s", res.stdout)
	}
}

// TestReviewScreenWithNoCheckoutRefusesAShell is the other half: the screen
// opens and reads fine with no working copy, and the two keys that need one say
// so rather than running against whatever the working directory happens to be.
func TestReviewScreenWithNoCheckoutRefusesAShell(t *testing.T) {
	t.Parallel()

	s := ghcassette.Replay(t, awayCassette(t))
	home := t.TempDir()

	sc := openReview(t, s, t.TempDir(), "HOME="+home, "XDG_CONFIG_HOME="+home+"/.config",
		"KyleKing/second-look#2")
	sc.await("KyleKing/second-look #2")

	sc.press("!")
	sc.await("would run somewhere else")

	sc.press("C")
	sc.await("clone it first")

	sc.press("q")
	sc.wait()
}

// awayCassette is what preparing and opening a pull request costs with no
// checkout: the two reads and the thread query, all named with --repo, twice
// over because the screen reads the pull request again when it opens.
func awayCassette(t *testing.T) string {
	t.Helper()

	return deriveFrom(t, "post-review", "away", func(c *ghcassette.Cassette) {
		once := make([]ghcassette.Interaction, 0, reads+1)
		once = append(once, c.Interactions[:reads]...)
		once = append(once, threadInteraction(t)...)

		twice := make([]ghcassette.Interaction, 0, 2*len(once))
		twice = append(twice, once...)
		twice = append(twice, once...)

		c.Interactions = twice
	})
}

func homeEnv(home string) []string {
	return []string{"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}
}

// storeIn is where the state directory lands for the fixture repository. The
// config directory is the platform's, so the test asks for the same rule the
// binary follows rather than spelling one path out.
func storeIn(t *testing.T, home string) string {
	t.Helper()

	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support",
			"second-look", "github.com", "KyleKing", "second-look")
	}

	return filepath.Join(home, ".config", "second-look", "github.com", "KyleKing", "second-look")
}
