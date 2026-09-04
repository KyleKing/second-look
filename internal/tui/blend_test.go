package tui_test

import (
	"fmt"
	"image/color"
	"math"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/kyleking/aragonite/tui/theme"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/kyleking/second-look/internal/tui"
)

// The palettes a diff is drawn on. Both are checked because the depths are one
// set of numbers for both, and a lift that separates two bands on a dark
// background can leave them touching on a light one.
func palettes() map[string]theme.Palette {
	return map[string]theme.Palette{"dark": theme.Macchiato(), "light": theme.Latte()}
}

// A band is the accent's hue at the background's depth rather than a mix of the
// two, because a mix lands in the grey ramp. The 256-color cube carries almost
// no dark tints either, so a terminal that cannot mix its own gets the bands
// deeper: held back there, added and removed quantize to one slot and the band
// stops saying which side a line is on. Reaching that case takes only tmux,
// which reports 256 colors on a laptop that has millions.
func TestBothSidesStayApartAtEveryDepth(t *testing.T) {
	t.Parallel()

	for name, p := range palettes() {
		for _, tc := range []struct {
			name     string
			millions bool
			profile  colorprofile.Profile
		}{
			{"millions", true, colorprofile.TrueColor},
			{"256 colors", false, colorprofile.ANSI256},
		} {
			t.Run(name+" "+tc.name, func(t *testing.T) {
				t.Parallel()

				seen := map[string]string{}

				for face, c := range tui.DiffColors(p, tc.millions) {
					painted := fmt.Sprint(tc.profile.Convert(c))

					if was, clash := seen[painted]; clash {
						t.Errorf("%s and %s are both %s", was, face, painted)
					}

					seen[painted] = face
				}
			})
		}
	}
}

// Distinct is not the same as legible: two colors a terminal spells differently
// can still be one color to look at. These are the separations the renderer was
// tuned to, measured the way an eye reads them, so a palette change or a depth
// tweak that flattens the diff fails here rather than in a review.
//
// The mark carries an underline as well, which is what lets its floor be the
// low one: a background alone cannot get further from its band without getting
// closer to the text sitting on it.
func TestAChangeIsVisibleAndItsCodeStaysReadable(t *testing.T) {
	t.Parallel()

	const (
		fromPage = 12.0
		fromEach = 25.0
		fromBand = 8.0
		onBand   = 4.0
	)

	for name, p := range palettes() {
		for _, millions := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s millions=%v", name, millions), func(t *testing.T) {
				t.Parallel()

				c := shown(tui.DiffColors(p, millions), millions)

				for _, side := range []string{"added", "removed"} {
					band, mark := c[side+" band"], c[side+" mark"]

					apart(t, side+" band from the page", band, c["page"], fromPage)
					apart(t, side+" mark from its band", mark, band, fromBand)

					legible(t, "code on the "+side+" band", c["text"], band, onBand)
					legible(t, "code on the "+side+" mark", c["text"], mark, onBand)
				}

				apart(t, "the two bands", c["added band"], c["removed band"], fromEach)
				apart(t, "the two marks", c["added mark"], c["removed mark"], fromEach)
			})
		}
	}
}

// shown is what the terminal actually paints. A 256-color terminal is measured
// after its own quantization rather than before, because the cube is what the
// eye is given: it snaps colors apart as often as it collapses them, and the
// depths for it were chosen against the slots rather than against the mix.
func shown(c map[string]color.Color, millions bool) map[string]color.Color {
	if millions {
		return c
	}

	out := make(map[string]color.Color, len(c))
	for face, v := range c {
		out[face] = colorprofile.ANSI256.Convert(v)
	}

	return out
}

// apart measures how far two colors are to look at, in CIEDE2000, where about
// 2 is the least a trained eye catches side by side and 10 is a difference
// nobody has to look for.
func apart(t *testing.T, what string, a, b color.Color, least float64) {
	t.Helper()

	from, _ := colorful.MakeColor(a)
	to, _ := colorful.MakeColor(b)

	if got := from.DistanceCIEDE2000(to) * 100; got < least {
		t.Errorf("%s: %.1f apart, wanted at least %.1f (%s, %s)", what, got, least, from.Hex(), to.Hex())
	}
}

// legible is the WCAG contrast of text on a background, which is the measure
// that says whether the code on a band can still be read.
func legible(t *testing.T, what string, text, on color.Color, least float64) {
	t.Helper()

	a, _ := colorful.MakeColor(text)
	b, _ := colorful.MakeColor(on)

	high, low := relative(a), relative(b)
	if high < low {
		high, low = low, high
	}

	if got := (high + 0.05) / (low + 0.05); got < least {
		t.Errorf("%s: contrast %.2f, wanted at least %.2f (%s on %s)", what, got, least, a.Hex(), b.Hex())
	}
}

func relative(c colorful.Color) float64 {
	channel := func(v float64) float64 {
		const knee, slope, offset, gain, gamma = 0.03928, 12.92, 0.055, 1.055, 2.4

		if v <= knee {
			return v / slope
		}

		return math.Pow((v+offset)/gain, gamma)
	}

	const red, green, blue = 0.2126, 0.7152, 0.0722

	return red*channel(c.R) + green*channel(c.G) + blue*channel(c.B)
}
