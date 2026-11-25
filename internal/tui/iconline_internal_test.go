package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/tui/icons"
)

// The `?` row naming the icon set never wraps: from 80 columns it names the set
// and why whole, and narrower it cuts the change first.
func TestIconsLine_FitsTheWidth(t *testing.T) {
	t.Parallel()

	resolutions := map[string]icons.Resolution{
		"auto nerd":  {Mode: icons.Nerd, Source: icons.FromAuto, Detail: "WezTerm"},
		"auto plain": {Mode: icons.Unicode, Source: icons.FromAuto},
		"env":        {Mode: icons.None, Source: icons.FromEnv, Detail: "off"},
		"setting":    {Mode: icons.Nerd, Source: icons.FromSetting, Detail: "nerd"},
	}

	for name, r := range resolutions {
		for _, width := range []int{30, 60, 79, 80, 119, 200} {
			t.Run(name+"/"+strconv.Itoa(width), func(t *testing.T) {
				t.Parallel()

				line := iconsLine(loadStyles(), r, width)
				if got := textWidth(line); got > width {
					t.Fatalf("%d cells at width %d: %q", got, width, line)
				}

				if width >= 79 && !strings.Contains(line, r.String()) {
					t.Fatalf("%q does not name %q whole at width %d", line, r.String(), width)
				}

				if width >= 79 && !strings.Contains(line, "=") {
					t.Fatalf("%q does not say how to change it at width %d", line, width)
				}
			})
		}
	}
}

// The change offered is the other set worth trying, and an override is named
// as what the config cannot beat.
func TestIconsHow_OffersTheOtherSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		r    icons.Resolution
		want string
	}{
		{icons.Resolution{Mode: icons.Nerd, Source: icons.FromAuto, Detail: "kitty"}, `icons = "plain"`},
		{icons.Resolution{Mode: icons.Unicode, Source: icons.FromAuto}, `icons = "nerd"`},
		{icons.Resolution{Mode: icons.Unicode, Source: icons.FromEnv, Detail: "plain"}, "unset it"},
	}

	for _, tt := range tests {
		if got := iconsHow(tt.r)[0]; !strings.Contains(got, tt.want) {
			t.Errorf("%v: %q lacks %q", tt.r, got, tt.want)
		}
	}
}
