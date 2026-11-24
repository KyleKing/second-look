package tui_test

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/framecheck"
)

// The goldens pin two widths of each frame; the faults they cannot see are the
// ones that show at the sizes nobody recorded. This draws the review and the
// queue with each of their overlays open at the sizes a terminal is opened at,
// and holds every frame to framecheck: no row past the screen's width, no box
// corner with an arm that joins nothing, no side broken partway down.
func TestEveryOverlayJoinsItsEdgesAtEverySize(t *testing.T) {
	t.Parallel()

	sizes := [][2]int{{80, 24}, {100, 30}, {120, 40}, {200, 60}}
	keys := map[string][]tea.KeyPressMsg{
		"review":   nil,
		"legend":   {key('?')},
		"palette":  {key(':'), key('h'), key('u')},
		"comment":  {{Code: tea.KeyTab}},
		"comments": {key('c')},
		"code":     {key('v')},
		"split":    {key('u'), key('v')},
		"confirm":  {key('m'), key('x'), key('S')},
		"prefix":   {key(']')},
	}

	for name, presses := range keys {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("review %s/%dx%d", name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				m := triaged(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range presses {
					press(t, m, k)
				}

				framecheck.Require(t, t.Name(), m.Frame(), framecheck.MaxWidth(size[0]))
			})
		}
	}

	lists := map[string][]tea.KeyPressMsg{
		"queue":  nil,
		"keys":   {key('?')},
		"filter": {key('/'), key('z'), key('z')},
	}

	for name, presses := range lists {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("queue %s/%dx%d", name, size[0], size[1]), func(t *testing.T) {
				t.Parallel()

				l := list(t, nil)
				l.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})

				for _, k := range presses {
					l.Update(k)
				}

				framecheck.Require(t, t.Name(), l.ListFrame(), framecheck.MaxWidth(size[0]))
			})
		}
	}
}
