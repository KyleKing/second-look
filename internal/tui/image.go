package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/termimg"
)

// imageRef is every ![alt](dest) in a body, the dest bracketed or bare. It is
// looser than post's attachRef on purpose: a thread posted on GitHub names
// user-attachments URLs, a staged comment names attach: paths, and both are
// worth a preview.
var imageRef = regexp.MustCompile(`!\[[^\]]*\]\((<[^>]+>|[^)\s]+)\)`)

// previewedMsg is the viewer closing, with why it closed badly if it did.
type previewedMsg struct{ err error }

// previewImages is u then i: the images the row under the cursor names, drawn
// on the real terminal through a child holding it — the pane's own pty would
// eat the graphics escapes the terminal is being asked for. The row's whole
// conversation is what it draws from, since a thread names its images across
// notes rather than in one place.
func (m *Model) previewImages() tea.Cmd {
	dests := m.imageDests()
	if len(dests) == 0 {
		m.say("no images on this row", false)

		return nil
	}

	if termimg.Detect() == termimg.ProtoNone {
		m.say("this terminal has no inline image protocol (kitty, iTerm2, or sixel)", false)

		return nil
	}

	m.say("drawing "+dests[0]+" on the terminal", false)

	cmd := exec.CommandContext(m.ctx, os.Args[0], //nolint:gosec // argv0 is this binary itself
		append([]string{"image"}, dests...)...)

	return tea.ExecProcess(cmd, func(err error) tea.Msg { return previewedMsg{err: err} })
}

// imageDests collects the destinations of every image the current row names,
// resolved the way a post resolves them: a URL is itself, ~ is home, / is
// absolute, and anything else sits under the review's root. A local file that
// is not there is left out, since the viewer cannot draw it anyway and its
// absence is told by the count that is drawn.
func (m *Model) imageDests() []string {
	var dests []string

	seen := map[string]bool{}

	for _, body := range m.imageBodies() {
		for _, match := range imageRef.FindAllStringSubmatch(body, -1) {
			d := strings.TrimSuffix(strings.TrimPrefix(match[1], "<"), ">")
			d = strings.TrimPrefix(d, "attach:")

			if !strings.HasPrefix(d, "http://") && !strings.HasPrefix(d, "https://") {
				full, err := resolveImage(m.store, d)
				if err != nil {
					continue
				}

				if _, err := os.Stat(full); err != nil {
					continue
				}

				d = full
			}

			if !seen[d] {
				seen[d] = true
				dests = append(dests, d)
			}
		}
	}

	return dests
}

// imageBodies is every body the row under the cursor speaks for: the whole
// thread when it is one, the review body when it is the review's own row, and
// the staged comment otherwise.
func (m *Model) imageBodies() []string {
	if t := m.currentThread(); t >= 0 {
		bodies := make([]string, 0, len(m.threads[t].Notes))
		for _, n := range m.threads[t].Notes {
			bodies = append(bodies, n.Body)
		}

		return bodies
	}

	switch i := m.current(); i {
	case reviewBody:
		return []string{m.review.Body}
	case noComment:
		return nil
	default:
		return []string{m.review.Comments[i].Body}
	}
}

// resolveImage turns the path a body names into a file the viewer opens: ~ is
// the author's home, an absolute path is itself, and anything else sits under
// root, which is the store the review's files live beside.
func resolveImage(root, path string) (string, error) {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err //nolint:wrapcheck // the caller only asks whether it resolved
		}

		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}

	return filepath.Join(root, path), nil
}
