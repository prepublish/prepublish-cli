package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// RunHistory lists the account's past audits and opens any of them. enter opens
// the selected audit through the open callback, which returns the report already
// rendered; esc returns to the list, and q quits.
func RunHistory(ctx context.Context, fetch func(ctx context.Context, page int) (*api.AnalysisPage, error), open func(ctx context.Context, id string) (string, error)) error {
	if fetch == nil {
		return context.Canceled
	}
	m := historyModel{
		ctx:     ctx,
		fetch:   fetch,
		open:    open,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		loading: true,
	}
	_, err := run(ctx, m)
	return err
}

type historyModel struct {
	ctx   context.Context
	fetch func(ctx context.Context, page int) (*api.AnalysisPage, error)
	open  func(ctx context.Context, id string) (string, error)

	spinner spinner.Model
	page    int
	items   []*api.Analysis
	total   int
	pages   int
	loading bool
	cursor  int
	err     error
	notice  string

	viewer        *viewerModel
	width, height int
}

// historyPageMsg is one fetched page, or the error the fetch returned.
type historyPageMsg struct {
	page   int
	result *api.AnalysisPage
	err    error
}

// historyOpenMsg is the rendered report the open callback returned.
type historyOpenMsg struct {
	report string
	err    error
}

func fetchPageCmd(ctx context.Context, fetch func(context.Context, int) (*api.AnalysisPage, error), page int) tea.Cmd {
	return func() tea.Msg {
		res, err := fetch(ctx, page)
		return historyPageMsg{page: page, result: res, err: err}
	}
}

func openReportCmd(ctx context.Context, open func(context.Context, string) (string, error), id string) tea.Cmd {
	return func() tea.Msg {
		report, err := open(ctx, id)
		return historyOpenMsg{report: report, err: err}
	}
}

func (m historyModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, fetchPageCmd(m.ctx, m.fetch, 1))
}

func (m historyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case historyPageMsg:
		m.loading = false
		if msg.err != nil {
			// The first page failing is fatal: there is no list to show. A later
			// page failing only costs the reader that page.
			if m.page == 0 {
				m.err = msg.err
				return m, tea.Quit
			}
			m.notice = "could not load that page: " + msg.err.Error()
			return m, nil
		}
		m.page = msg.page
		m.items, m.total, m.pages = nil, 0, 0
		if msg.result != nil {
			m.items = msg.result.Analyses
			m.total = msg.result.Total
			m.pages = msg.result.TotalPages
		}
		m.cursor = 0
		m.notice = ""
		return m, nil
	case historyOpenMsg:
		m.loading = false
		if msg.err != nil {
			m.notice = "could not open that audit: " + msg.err.Error()
			return m, nil
		}
		vm := newViewer(msg.report, nil)
		if m.width > 0 {
			vm.vp.SetWidth(m.width)
			vm.vp.SetHeight(max(m.height-2, 3))
			vm.width = m.width
		}
		// esc in the report comes back here; the closure has to capture this
		// call's model, which is why it is rebuilt on every delegation below.
		m.viewer = &vm
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		if m.viewer != nil {
			m.viewer.back = func() (tea.Model, tea.Cmd) {
				m.viewer = nil
				return m, nil
			}
			updated, cmd := m.viewer.Update(msg)
			if vm, ok := updated.(viewerModel); ok {
				m.viewer = &vm
				return m, cmd
			}
			return updated, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "n", "pgdown":
			return m, m.changePage(1)
		case "p", "pgup":
			return m, m.changePage(-1)
		case "r":
			m.loading = true
			return m, fetchPageCmd(m.ctx, m.fetch, max(m.page, 1))
		case "enter":
			if id := m.selectedID(); id != "" {
				m.loading = true
				m.notice = ""
				return m, openReportCmd(m.ctx, m.open, id)
			}
		}
	}
	return m, nil
}

func (m *historyModel) moveCursor(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor = (m.cursor + delta + len(m.items)) % len(m.items)
}

func (m *historyModel) changePage(delta int) tea.Cmd {
	next := m.page + delta
	if m.pages > 0 && (next < 1 || next > m.pages) {
		m.notice = "that is the last page"
		return nil
	}
	if next < 1 {
		return nil
	}
	m.loading = true
	return fetchPageCmd(m.ctx, m.fetch, next)
}

func (m historyModel) selectedID() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	if a := m.items[m.cursor]; a != nil {
		return a.ID
	}
	return ""
}

func (m historyModel) View() tea.View {
	if m.viewer != nil {
		v := m.viewer.View()
		return v
	}

	w := m.width
	if w <= 0 {
		w = 100
	}
	w = clampInt(w, 40, 120)

	var b strings.Builder
	meta := pluralCount(m.total, "audit", "audits")
	if m.pages > 0 {
		meta += " · page " + itoa(max(m.page, 1)) + " of " + itoa(m.pages)
	}
	b.WriteString(styTitle.Render("History") + "  " + styDim.Render(meta) + "\n")
	b.WriteString(styFaint.Render(strings.Repeat("─", w)) + "\n")

	switch {
	case m.loading && len(m.items) == 0:
		b.WriteString("  " + m.spinner.View() + " " + styDim.Render("loading your audits…") + "\n")
	case len(m.items) == 0:
		b.WriteString(styDim.Render("  No audits yet. Run `prepublish audit -f script.md` and they will collect here.") + "\n")
	default:
		b.WriteString(m.rows(w) + "\n")
	}

	if m.notice != "" {
		b.WriteString("\n" + styWarn.Render("  "+ansi.Truncate(m.notice, w-4, "…")))
	}
	if m.loading && len(m.items) > 0 {
		b.WriteString("\n" + "  " + m.spinner.View() + " " + styDim.Render("working…"))
	}
	b.WriteString("\n\n" + hint("↑/↓", "move", "enter", "open", "n/p", "page", "q", "quit") + "\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// The history table's columns, laid out once: index, title, score, status, and
// the created date. A header and its rows must be built by the same arithmetic
// or the labels drift over the values — "Score" padded to exactly its own width
// left no gap before "Status".
const (
	historyIndexW  = 3
	historyScoreW  = 5
	historyStatusW = 11
	historyDateW   = 12
	historyGap     = 2
)

// historyLayout is one row's column set. A zero width means the column does not
// fit at this terminal width and is left out of both the header and the rows.
type historyLayout struct {
	title   int
	status  bool
	created bool
}

// newHistoryLayout fits as many columns as the width allows: everything from 70
// columns up, the date dropped next, then the status.
func newHistoryLayout(w int) historyLayout {
	// marker + index + score + the gap before the title and after the score.
	const fixed = 1 + historyIndexW + 2*historyGap + historyScoreW
	switch {
	case w >= 70:
		return historyLayout{
			title:   max(w-fixed-historyStatusW-historyDateW-2*historyGap, 12),
			status:  true,
			created: true,
		}
	case w >= 52:
		return historyLayout{title: max(w-fixed-historyStatusW-historyGap, 12), status: true}
	default:
		return historyLayout{title: max(w-fixed, 4)}
	}
}

// row renders one line of the table: the cursor marker, the index, the title and
// the score always, the status and the date when they fit. Styled cells measure
// correctly because the padding counts display columns.
func (l historyLayout) row(marker, index, title, score, status, created string) string {
	line := marker + pad(index, historyIndexW) + strings.Repeat(" ", historyGap) +
		pad(title, l.title) + strings.Repeat(" ", historyGap) +
		pad(score, historyScoreW)
	if l.status {
		line += strings.Repeat(" ", historyGap) + pad(status, historyStatusW)
	}
	if l.created {
		line += strings.Repeat(" ", historyGap) + created
	}
	return line
}

// rows draws the list: a faint header, then one line per audit. The title takes
// whatever is left, so a narrow terminal loses the title column rather than the
// numbers.
func (m historyModel) rows(w int) string {
	layout := newHistoryLayout(w)
	var out []string
	out = append(out, styFaint.Render(layout.row(" ", "", "Title", "Score", "Status", "Created")))
	for i, a := range m.items {
		selected := i == m.cursor
		marker, title := " ", styFaint
		if selected {
			marker, title = styAccent.Render("▸"), styTitle
		}

		status, score, created := "unknown", "—", ""
		if a != nil {
			status = strings.ReplaceAll(a.Status, "_", " ")
			if a.Status == api.StatusCompleted {
				score = itoa(a.OverallScore)
			}
			if !a.CreatedAt.IsZero() {
				created = a.CreatedAt.Local().Format("2 Jan 15:04")
			}
		}
		out = append(out, layout.row(marker, itoa(i+1), title.Render(shortTitle(a)),
			scoreCell(score, a), statusCell(status, a), styFaint.Render(created)))
	}
	return strings.Join(out, "\n")
}

func shortTitle(a *api.Analysis) string {
	if a == nil {
		return "(missing)"
	}
	title := strings.TrimSpace(a.VideoTitle)
	if title == "" {
		return "(untitled)"
	}
	return title
}

func scoreCell(score string, a *api.Analysis) string {
	style := styFaint
	if a != nil && a.Status == api.StatusCompleted {
		switch {
		case a.OverallScore >= 75:
			style = styGood
		case a.OverallScore >= 50:
			style = styWarn
		default:
			style = styBad
		}
	}
	return style.Render(score)
}

func statusCell(status string, a *api.Analysis) string {
	style := styFaint
	if a != nil {
		switch a.Status {
		case api.StatusCompleted:
			style = styGood
		case api.StatusFailed:
			style = styBad
		default:
			style = styWarn
		}
	}
	return style.Render(status)
}

// pad truncates or pads to exactly one column width, so the columns line up
// whatever the data contains.
func pad(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func pluralCount(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return itoa(n) + " " + many
}
