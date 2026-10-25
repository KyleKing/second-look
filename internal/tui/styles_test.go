package tui_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/tui"
)

// legibleRatio is WCAG's floor for text. Overlay0 placeholders are decoration and
// are not among the faces measured.
const legibleRatio = 4.5

func TestFacesAreReadableOnWhatTheyAreDrawnOn(t *testing.T) {
	t.Parallel()

	readings := tui.Readings()
	if len(readings) == 0 {
		t.Fatal("nothing was measured")
	}

	for _, r := range readings {
		if r.Ratio < legibleRatio {
			t.Errorf("%s is %.2f:1, want %.1f:1", r.Name, r.Ratio, legibleRatio)
		}
	}
}
