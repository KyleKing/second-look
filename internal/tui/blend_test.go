package tui_test

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/kyleking/second-look/internal/tui"
)

// A band is the accent's hue at the background's depth rather than a mix of the
// two, because a mix lands in the grey ramp. The 256-color cube carries almost
// no dark tints either, so a terminal that cannot mix its own gets the bands
// deeper: held back there, added and removed quantize to one slot and the band
// stops saying which side a line is on. Reaching that case takes only tmux,
// which reports 256 colors on a laptop that has millions.
func TestBothSidesStayApartAtEveryDepth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		millions bool
		profile  colorprofile.Profile
	}{
		{"millions", true, colorprofile.TrueColor},
		{"256 colors", false, colorprofile.ANSI256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			seen := map[string]string{}

			for face, c := range tui.BandColors(tc.millions) {
				painted := fmt.Sprint(tc.profile.Convert(c))

				if was, clash := seen[painted]; clash {
					t.Errorf("%s and %s are both %s", was, face, painted)
				}

				seen[painted] = face
			}
		})
	}
}
