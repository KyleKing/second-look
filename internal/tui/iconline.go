package tui

import (
	"github.com/kyleking/aragonite/tui/icons"
)

// iconsLabel heads the row `?` keeps under itself, as config.toml spells the key.
const iconsLabel = "icons"

// iconsResolution is which icon set the screens draw and why, read the way the
// styles were built: the override, then the configured value, then the terminal.
func iconsResolution() icons.Resolution {
	built.Lock()
	setting := built.icons
	built.Unlock()

	return icons.Explain(setting)
}

// iconsLine is the row the legend keeps under itself: which icon set the files
// draw with, why, and how to change it, in the longest wording of the change
// that fits the width. Where none fits, the set and its reason keep their place
// and the change is cut, so the row never wraps.
func iconsLine(s styles, r icons.Resolution, width int) string {
	says := r.String()
	lead := iconsLabel + "  " + says + " · "
	ways := iconsHow(r)

	how := ways[len(ways)-1]
	for _, way := range ways {
		if textWidth(lead+way) <= width {
			how = way

			break
		}
	}

	room := width - textWidth(iconsLabel+"  ")
	says = cut(says, room)
	how = cut(how, room-textWidth(says+" · "))

	line := s.hintKey.Render(iconsLabel) + "  " + s.hintWord.Render(says)
	if how != "" {
		line += s.note.Render(" · " + how)
	}

	return line
}

// iconsHow are the ways to change the icon set a resolution names, longest
// first: the other set worth trying (plain where Nerd icons draw, nerd
// elsewhere), and where ARAGONITE_ICONS decided, that the config cannot win
// until the variable is changed or unset.
func iconsHow(r icons.Resolution) []string {
	want := "nerd"
	if r.Mode == icons.Nerd {
		want = "plain"
	}

	env := icons.EnvOverride + "=" + want
	if r.Source == icons.FromEnv {
		return []string{
			"set " + env + ", or unset it for config.toml's icons",
			"set " + env + " or unset it",
			env,
		}
	}

	set := iconsLabel + ` = "` + want + `"`

	return []string{
		"set " + set + " in ~/.config/second-look/config.toml or " + env,
		"set " + set + " or " + env,
		set + " or " + env,
		env,
	}
}
