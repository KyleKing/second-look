package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/handoff"
)

const (
	dashboardName     = "repo-dashboard"
	dashboardTemplate = "gh repo-dashboard {path}"
	dashboardInstall  = "gh extension install KyleKing/gh-repo-dashboard"
)

func dashboardValues(dir string) map[string]string {
	return map[string]string{"path": dir}
}

// openDashboard hands the terminal to gh-repo-dashboard on the checkout and
// takes it back when that exits.
func (m *Model) openDashboard() tea.Cmd {
	if m.repoDir == "" {
		m.say("no checkout of "+m.review.Owner+"/"+m.review.Repo+" here for the dashboard to open", false)

		return nil
	}

	cmd, err := handoff.Run(dashboardName, dashboardTemplate, dashboardValues(m.repoDir), m.repoDir, nil)
	if err != nil {
		m.say(err.Error(), true)

		return nil
	}

	return cmd
}

// dashboardDone answers the hand-off's return. The dashboard can move the
// checkout's branch, so the head is asked about again.
func (m *Model) dashboardDone(msg handoff.DoneMsg) tea.Cmd {
	if msg.Name != dashboardName {
		return nil
	}

	if msg.Err != nil {
		m.say(fmt.Sprintf("gh repo-dashboard failed: %v (%s)", msg.Err, dashboardInstall), true)

		return nil
	}

	m.say("", false)

	return m.checkHead()
}
