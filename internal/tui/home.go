package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

// RunHome shows the menu a bare `prepublish` opens in a terminal: the wordmark,
// one line of account state, and the things the CLI can do. It returns the
// chosen item; quitting and ctrl+c both return HomeQuit.
func RunHome(state HomeState) (HomeChoice, error) {
	final, err := run(context.Background(), newHomeModel(state))
	if err != nil {
		return HomeQuit, err
	}
	if hm, ok := final.(homeModel); ok {
		return hm.choice, nil
	}
	return HomeQuit, nil
}

type homeItem struct {
	choice HomeChoice
	label  string
	desc   string
	// hidden entries are skipped when the menu is built (sign in versus sign
	// out); keeping both in one list is what makes the order stable.
	hidden bool
}

type homeModel struct {
	state  HomeState
	items  []homeItem
	cursor int
	width  int
	choice HomeChoice
}

func newHomeModel(state HomeState) homeModel {
	signedIn := state.User != nil
	tier, _ := tierOf(state)
	items := []homeItem{
		{choice: HomeAudit, label: "Audit a script", desc: "Full report: scores, attention-risk map, prioritized rewrites"},
		{choice: HomeHook, label: "Check a hook", desc: "Score the first thirty seconds and get rewrites"},
		{choice: HomePolicy, label: "Policy pre-flight", desc: "Which passages match YouTube's published guidelines"},
		{choice: HomeAuthenticity, label: "Authenticity check", desc: "Reused or inauthentic content risk"},
		{choice: HomeHistory, label: "History", desc: "Past audits, with their scores"},
		{choice: HomeLogin, label: "Sign in", desc: "Browser login, or paste an API key", hidden: signedIn},
		{choice: HomeLogout, label: "Sign out", desc: "Forget the credential saved on this machine", hidden: !signedIn},
		{choice: HomeUpgrade, label: upgradeLabel(tier), desc: upgradeDesc(tier), hidden: tier == api.TierStudio},
		{choice: HomeQuit, label: "Quit"},
	}
	m := homeModel{state: state, width: state.Width, choice: HomeQuit}
	for _, it := range items {
		if !it.hidden {
			m.items = append(m.items, it)
		}
	}
	return m
}

// upgradeLabel and upgradeDesc pitch the next plan up, not the one the caller
// already has: offering Creator to a Creator is the fastest way to make a paid
// screen look broken. A Studio subscriber has nothing left to buy here, so the
// item is hidden and `prepublish billing` remains the way to manage the
// subscription.
func upgradeLabel(tier string) string {
	if tier == api.TierPaid {
		return "Upgrade to Studio"
	}
	return "Upgrade"
}

func upgradeDesc(tier string) string {
	if tier == api.TierPaid {
		return "Studio: the client-ready export, on the same 50 audits a day"
	}
	return "Creator: 50 audits a day and full reports"
}

func (m homeModel) Init() tea.Cmd { return nil }

func (m homeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyPressMsg:
		switch {
		case msg.String() == "ctrl+c", msg.String() == "esc", msg.String() == "q":
			m.choice = HomeQuit
			return m, tea.Quit
		case key.Matches(msg, keys.Up):
			m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
		case key.Matches(msg, keys.Down):
			m.cursor = (m.cursor + 1) % len(m.items)
		case key.Matches(msg, keys.Select):
			m.choice = m.items[m.cursor].choice
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m homeModel) View() tea.View {
	w := m.width
	if w <= 0 {
		w = ui.MaxWidth
	}
	w = clampInt(w, 40, ui.MaxWidth)

	var b strings.Builder
	b.WriteString(banner(w))
	if m.state.Notice != "" {
		b.WriteString("\n" + styWarn.Render("! "+ansi.Truncate(m.state.Notice, w-2, "…")))
	}
	b.WriteString("\n\n" + styFaint.Render(strings.Repeat("─", w)))
	b.WriteString("\n" + accountLine(m.state))
	b.WriteString("\n\n")

	for i, it := range m.items {
		selected := i == m.cursor
		marker, label := "  ", "  "+it.label
		if selected {
			marker, label = styAccent.Render("▸ "), "  "+styTitle.Render(it.label)
		}
		line := marker + label
		if it.desc != "" {
			pad := max(30-lipgloss.Width(it.label), 2)
			switch {
			case lipgloss.Width(line)+pad+lipgloss.Width(it.desc) <= w:
				line += strings.Repeat(" ", pad) + styFaint.Render(it.desc)
			case selected:
				// No room beside the label: the description moves under the
				// selected item, where it is the line actually being read, and
				// is cut to the width rather than allowed to wrap the terminal.
				// The unselected items stay quiet instead of showing a column of
				// half-sentences.
				line += "\n" + styFaint.Render("      "+ansi.Truncate(it.desc, max(w-8, 8), "…"))
			}
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + hint("↑/↓", "move", "enter", "select", "q", "quit"))
	footer := strings.TrimSpace(strings.Join(nonEmpty(m.state.Version, m.state.APIURL), " · "))
	if footer != "" {
		b.WriteString("\n" + styFaint.Render(" "+footer))
	}
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
