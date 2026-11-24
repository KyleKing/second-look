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

// The palettes a diff is drawn on: Ink on a dark terminal and Latte on a light one.
func palettes() map[string]theme.Palette {
	return map[string]theme.Palette{"ink": theme.Ink(), "latte": theme.Latte()}
}

// depths are the profiles a diff is drawn at with a background: truecolor mixes
// its own tints, and 256 colors gets greys, since the cube's dark greens and reds
// are saturated blocks. Reaching 256 takes only tmux, which reports it on a
// laptop that has millions.
var depths = map[string]colorprofile.Profile{
	"millions": colorprofile.TrueColor, "256 colors": colorprofile.ANSI256,
}

// In truecolor each side carries its own hue, so all four backgrounds differ.
// In 256 colors both sides wash in one grey and mark in a deeper one, so the
// side is said by the sign and the line number: those must differ there, and
// the mark must still stand off its wash.
func TestBothSidesStayApartAtEveryDepth(t *testing.T) {
	t.Parallel()

	for name, p := range palettes() {
		for depth, profile := range depths {
			t.Run(name+" "+depth, func(t *testing.T) {
				t.Parallel()

				c := tui.DiffColors(p, profile)
				painted := func(face string) string { return fmt.Sprint(profile.Convert(c[face])) }

				pairs := [][2]string{
					{"added sign", "removed sign"}, {"added wash", "added mark"}, {"removed wash", "removed mark"},
				}
				if profile == colorprofile.TrueColor {
					pairs = append(pairs,
						[2]string{"added wash", "removed wash"}, [2]string{"added mark", "removed mark"})
				}

				for _, pair := range pairs {
					if painted(pair[0]) == painted(pair[1]) {
						t.Errorf("%s and %s are both %s", pair[0], pair[1], painted(pair[0]))
					}
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
// The mark says which runs changed, so it has to be far enough from the page and
// from its wash to be seen, and no deeper than the code on it survives.
func TestAChangeIsVisibleAndItsCodeStaysReadable(t *testing.T) {
	t.Parallel()

	const (
		fromPage = 6.0
		fromWash = 4.0
		onMark   = 4.0
	)

	for name, p := range palettes() {
		for depth, profile := range depths {
			t.Run(name+" "+depth, func(t *testing.T) {
				t.Parallel()

				c := shown(tui.DiffColors(p, profile), profile)

				for _, side := range []string{"added", "removed"} {
					mark := c[side+" mark"]

					apart(t, side+" mark from the page", mark, c["page"], fromPage)
					apart(t, side+" mark from its wash", mark, c[side+" wash"], fromWash)
					legible(t, "code on the "+side+" mark", c["text"], mark, onMark)
					legible(t, "code on the "+side+" wash", c["text"], c[side+" wash"], onMark)
				}

				if profile == colorprofile.TrueColor {
					apart(t, "the two marks", c["added mark"], c["removed mark"], 10.0)
					apart(t, "the two washes", c["added wash"], c["removed wash"], 3.0)
				}
			})
		}
	}
}

// shown is what the terminal actually paints: a 256-color terminal is measured
// after its own quantization rather than before, because the slot is what the
// eye is given.
func shown(c map[string]color.Color, profile colorprofile.Profile) map[string]color.Color {
	out := make(map[string]color.Color, len(c))
	for face, v := range c {
		out[face] = profile.Convert(v)
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
