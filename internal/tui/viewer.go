package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// RunReportViewer pages a rendered report. It is a pager, not a screen: it
// renders inline (no alternate screen) so that quitting leaves the report in the
// scrollback where the reader can keep using it.
//
// q and esc quit; the arrow keys, j/k, the page keys, and the mouse wheel scroll.
func RunReportViewer(report string) error {
	m := newViewer(report, nil)
	_, err := run(context.Background(), m)
	return err
}

// viewerModel is the pager. back is nil when the viewer was opened directly from
// a command; when it is set, esc returns to the screen that opened the report
// instead of quitting.
type viewerModel struct {
	vp    viewport.Model
	back  func() (tea.Model, tea.Cmd)
	title string
	width int
}

func newViewer(content string, back func() (tea.Model, tea.Cmd)) viewerModel {
	vp := viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))
	vp.SoftWrap = true
	vp.SetContent(strings.TrimRight(content, "\n"))
	return viewerModel{vp: vp, back: back, width: 100}
}

func (m viewerModel) Init() tea.Cmd { return nil }

func (m viewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.vp.SetWidth(msg.Width)
		m.vp.SetHeight(max(msg.Height-2, 3))
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			if m.back != nil {
				return m.back()
			}
			return m, tea.Quit
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m viewerModel) View() tea.View {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(styTitle.Render(m.title) + "\n")
	}
	b.WriteString(m.vp.View())
	b.WriteString("\n" + m.status())
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// status is the footer: how far through the report the reader is, and the keys
// that matter here. Esc means "back" only when there is somewhere to go back to.
func (m viewerModel) status() string {
	through := styFaint.Render(" " + itoa(int(m.vp.ScrollPercent()*100+0.5)) + "%")
	pairs := []string{"↑/↓", "scroll", "space", "page"}
	if m.back != nil {
		pairs = append(pairs, "esc", "back", "q", "quit")
	} else {
		pairs = append(pairs, "q/esc", "quit")
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(through + hint(pairs...))
}
