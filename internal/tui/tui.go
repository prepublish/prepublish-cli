// Package tui holds the interactive screens: the home menu, the audit and
// upload progress views, browser login, the report pager, history, and the huh
// prompts. Each entry point owns a bubbletea program for its lifetime and
// returns the result the command needs.
//
// Two rules hold everywhere in this package. First, Update never blocks: every
// call into the network, the filesystem or another process is issued as a
// tea.Cmd or handed to a goroutine that reports back over a channel, and the
// model only ever reads what has already arrived. Second, ctrl+c and a
// cancelled context are the same thing to a caller — both surface as
// context.Canceled, so a command can tell "the user gave up" from "the API
// refused" without inspecting key presses.
package tui

import (
	"context"
	"errors"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/ui"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
)

// HomeState is what the home screen knows about the caller. Every field is
// optional: an anonymous machine has no User, no Usage and no Source, a paid
// account has no Free, and a failed lookup leaves Notice set and the rest nil.
type HomeState struct {
	User    *api.User
	Usage   *api.Usage
	Free    *api.CheckFree
	Source  config.CredentialSource
	Email   string
	Width   int
	APIURL  string
	Version string
	// Notice is a best-effort failure line ("offline: …"). It is shown dimly
	// under the banner and never blocks the menu.
	Notice string
}

// HomeChoice is the menu item the user picked.
type HomeChoice int

// The home menu, in the order it is drawn.
const (
	HomeQuit HomeChoice = iota
	HomeAudit
	HomeHook
	HomePolicy
	HomeAuthenticity
	HomeHistory
	HomeLogin
	HomeLogout
	HomeUpgrade
)

func (c HomeChoice) String() string {
	switch c {
	case HomeAudit:
		return "audit"
	case HomeHook:
		return "hook"
	case HomePolicy:
		return "policy"
	case HomeAuthenticity:
		return "authenticity"
	case HomeHistory:
		return "history"
	case HomeLogin:
		return "login"
	case HomeLogout:
		return "logout"
	case HomeUpgrade:
		return "upgrade"
	default:
		return "quit"
	}
}

// palette mirrors the report's colours. The TUI cannot import unexported
// styles from ui, so the handful of values it needs are repeated here and kept
// deliberately small: brand red, muted grey, and the three score bands.
var (
	cAccent  = lipgloss.Color("#EF4444")
	cMuted   = lipgloss.Color("#A3A3AB")
	cFaint   = lipgloss.Color("#71717A")
	cBorder  = lipgloss.Color("#232327")
	cSuccess = lipgloss.Color("#22C55E")
	cWarning = lipgloss.Color("#EAB308")
	cDanger  = lipgloss.Color("#EF4444")
	cInfo    = lipgloss.Color("#3B82F6")
	// cGradientEnd is where the wordmark's gradient lands: brand red into the
	// warm orange the chart's high-risk end uses.
	cGradientEnd = lipgloss.Color("#F97316")
)

// style helpers, kept in one place so the screens match each other.
var (
	styTitle  = lipgloss.NewStyle().Bold(true)
	styDim    = lipgloss.NewStyle().Foreground(cMuted)
	styFaint  = lipgloss.NewStyle().Foreground(cFaint)
	styAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	styBad    = lipgloss.NewStyle().Foreground(cDanger)
	styGood   = lipgloss.NewStyle().Foreground(cSuccess)
	styWarn   = lipgloss.NewStyle().Foreground(cWarning)
)

// keymap is the shared navigation binding set. It exists so every screen
// answers to the same keys, and so the help line under a screen is generated
// from the same definitions the handler matches on.
type keymap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Select key.Binding
	Back   key.Binding
	Quit   key.Binding
	Help   key.Binding
	Open   key.Binding
	Copy   key.Binding
	Top    key.Binding
	Bottom key.Binding
	Next   key.Binding
	Prev   key.Binding
}

func newKeymap() keymap {
	return keymap{
		Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		Select: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Open:   key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open browser")),
		Copy:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy code")),
		Top:    key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
		Bottom: key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
		Next:   key.NewBinding(key.WithKeys("pgdown", "f", " "), key.WithHelp("f/pgdn", "page down")),
		Prev:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("b/pgup", "page up")),
	}
}

var keys = newKeymap()

// hint renders the key hints a screen shows at its foot. It is deliberately
// hand-rolled rather than assembled from a help model: the hints are part of
// the design here, not a window into the keymap.
func hint(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, lipgloss.NewStyle().Foreground(cMuted).Bold(true).Render(pairs[i])+
			" "+styFaint.Render(pairs[i+1]))
	}
	return "  " + strings.Join(parts, styFaint.Render(" · "))
}

// wordmarkGlyphs draws PREPUBLISH as block letters: five rows, four columns
// each, filled with blocks and coloured by the brand gradient. A hand-drawn
// glyph table is the whole cost of a wordmark that looks like a product mark
// instead of a bold word, and it is the only form of it that needs no font file
// on the machine.
var wordmarkGlyphs = map[rune][]string{
	'P': {"████", "█  █", "████", "█   ", "█   "},
	'R': {"████", "█  █", "███ ", "█  █", "█  █"},
	'E': {"████", "█   ", "███ ", "█   ", "████"},
	'U': {"█  █", "█  █", "█  █", "█  █", "████"},
	'B': {"████", "█  █", "███ ", "█  █", "████"},
	'L': {"█   ", "█   ", "█   ", "█   ", "████"},
	'I': {"████", " ██ ", " ██ ", " ██ ", "████"},
	'S': {" ███", "█   ", " ██ ", "   █", "███ "},
	'H': {"█  █", "█  █", "████", "█  █", "█  █"},
}

const (
	wordmarkRows = 5
	// wordmarkMinWidth is the narrowest screen the block wordmark is used on:
	// below it the one-line mark is better than a squeezed one.
	wordmarkMinWidth = 44
)

// wordmark renders the block wordmark for word, or the one-line mark when the
// screen cannot seat it. The gradient runs left to right across the whole mark,
// the way the web wordmark does.
func wordmark(word string, w int) (string, bool) {
	letters := []rune(strings.ToUpper(word))
	mark := make([]string, wordmarkRows)
	for row := range wordmarkRows {
		var b strings.Builder
		for i, r := range letters {
			glyph, ok := wordmarkGlyphs[r]
			if !ok {
				return "", false
			}
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(glyph[row])
		}
		mark[row] = b.String()
	}
	width := lipgloss.Width(mark[0])
	if w < wordmarkMinWidth || width > w {
		return "", false
	}

	ramp := lipgloss.Blend1D(width, cAccent, cGradientEnd)
	var b strings.Builder
	for row, line := range mark {
		if row > 0 {
			b.WriteString("\n")
		}
		for col, r := range []rune(line) {
			if r == ' ' {
				b.WriteRune(r)
				continue
			}
			b.WriteString(lipgloss.NewStyle().Foreground(ramp[min(col, len(ramp)-1)]).Render(string(r)))
		}
	}
	return b.String(), true
}

// banner is the header the home screen shows: the block wordmark with the
// product line when the screen can seat them, and the compact mark otherwise.
func banner(w int) string {
	const tagline = "Script audits before you record."
	if mark, ok := wordmark("prepublish", w); ok {
		return mark + "\n" + styDim.Render(tagline)
	}
	return fitBlock(ui.Banner(), w)
}

// fitBlock truncates every line to w columns. The TUI's own chrome comes from
// ui (the banner) or from constants (item descriptions), and neither knows the
// terminal width, so this is the one place the screens guard their width.
func fitBlock(s string, w int) string {
	if w < 1 {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, l := range parts {
		parts[i] = ansi.Truncate(l, w, "…")
	}
	return strings.Join(parts, "\n")
}

// bar is the block bar the progress screens draw, matching the score gauges in
// the report: filled with the brand accent, empty in the grid colour. It is
// drawn here rather than pulled from bubbles/progress so the two screens need
// no animation dependency for a bar that is set, not sprung.
func bar(fraction float64, w int) string {
	w = max(w, 8)
	filled := clampInt(int(fraction*float64(w)+0.5), 0, w)
	return lipgloss.NewStyle().Foreground(cAccent).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1C1C20")).Render(strings.Repeat("░", w-filled))
}

// cancelled reports whether the program ended because the user interrupted it,
// and the error the caller should see. Bubble Tea signals both an interrupt
// message and a killed context with a wrapped error, so both are unwrapped
// here rather than at each entry point.
func cancelled(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return context.Canceled
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// run starts a program with the given model, tied to ctx. Every entry point in
// this package funnels through it so ctrl+c, SIGINT and a cancelled context
// behave identically everywhere.
func run(ctx context.Context, m tea.Model, opts ...tea.ProgramOption) (tea.Model, error) {
	opts = append([]tea.ProgramOption{tea.WithContext(ctx)}, opts...)
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	if err != nil {
		return final, cancelled(ctx, err)
	}
	return final, nil
}

// theme is the huh theme, so the prompts are the same product as the renderers:
// brand-red focus, dim labels, rounded-free field edges.
func theme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)
		accent := lipgloss.Color("#EF4444")
		muted := lipgloss.Color("#A3A3AB")
		faint := lipgloss.Color("#71717A")
		bone := lipgloss.Color("#F4F3F0")
		ink := lipgloss.Color("#0A0A0C")
		if !isDark {
			accent = lipgloss.Color("#DC2626")
			muted = lipgloss.Color("#52525B")
			faint = lipgloss.Color("#71717A")
			bone = lipgloss.Color("#18181B")
			ink = lipgloss.Color("#FAFAFA")
		}

		t.Focused.Base = lipgloss.NewStyle().PaddingLeft(1).BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).BorderForeground(accent)
		t.Focused.Card = t.Focused.Base
		t.Focused.Title = lipgloss.NewStyle().Foreground(bone).Bold(true)
		t.Focused.Description = lipgloss.NewStyle().Foreground(muted)
		t.Focused.ErrorIndicator = lipgloss.NewStyle().Foreground(accent).SetString(" ✖")
		t.Focused.ErrorMessage = lipgloss.NewStyle().Foreground(accent)
		t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(accent).SetString("▸ ")
		t.Focused.Option = lipgloss.NewStyle().Foreground(bone)
		t.Focused.FocusedButton = lipgloss.NewStyle().Foreground(ink).Background(accent).Bold(true)
		t.Focused.BlurredButton = lipgloss.NewStyle().Foreground(bone).Background(faint)
		t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(accent)
		t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(bone)
		t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(faint)
		t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(accent)

		t.Blurred = t.Focused
		t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.Title = lipgloss.NewStyle().Foreground(muted).Bold(true)
		t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(faint)
		t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(muted)

		t.Group.Title = lipgloss.NewStyle().Bold(true)
		t.Group.Description = lipgloss.NewStyle().Foreground(muted)
		return t
	})
}

// accountLine is the home screen's one-line summary of who is signed in and how
// much of today's allowance is left. It is built here rather than in ui because
// it is a single pill and a phrase, not a card.
//
// The identity and the plan never both appear as bare text: a signed-in caller
// gets their address plus the plan pill, and an anonymous caller gets one pill
// reading "Anonymous" rather than "not signed in" standing next to a pill that
// says "Anonymous".
func accountLine(s HomeState) string {
	tier, label := tierOf(s)
	pill := lipgloss.NewStyle().Foreground(inkFor(tier)).Background(tierColorFor(tier)).Bold(true).Padding(0, 1).Render(label)

	var parts []string
	switch {
	case s.User != nil:
		identity := strings.TrimSpace(s.Email)
		if identity == "" {
			identity = strings.TrimSpace(s.User.Email)
		}
		if identity == "" {
			identity = "Signed in"
		}
		parts = append(parts, styTitle.Render(identity), pill)
	default:
		parts = append(parts, pill)
	}
	if quota := quotaOf(s); quota != "" {
		parts = append(parts, styDim.Render(quota))
	}
	if s.Source == config.SourceEnv {
		parts = append(parts, styFaint.Render("key from PREPUBLISH_API_KEY"))
	}
	return " " + strings.Join(parts, "  ")
}

func tierOf(s HomeState) (tier, label string) {
	switch {
	case s.Usage != nil && s.Usage.Tier != "":
		tier = s.Usage.Tier
	case s.Free != nil && s.Free.Tier != "":
		tier = s.Free.Tier
	case s.User != nil:
		tier = api.TierFreeAccount
	default:
		tier = api.TierAnonymous
	}
	switch tier {
	case api.TierFreeAccount:
		label = "Free"
	case api.TierPaid:
		label = "Creator"
	case api.TierStudio:
		label = "Studio"
	default:
		label = "Anonymous"
	}
	return tier, label
}

func tierColorFor(tier string) color.Color {
	switch tier {
	case api.TierFreeAccount:
		return cInfo
	case api.TierPaid:
		return cAccent
	case api.TierStudio:
		return lipgloss.Color("#8B5CF6")
	default:
		return cFaint
	}
}

// inkFor is the text colour on a filled pill. Every tier colour the product
// uses is mid-to-dark, so the foreground is the bone white in both themes.
func inkFor(string) color.Color { return lipgloss.Color("#FAFAFA") }

// quotaOf is the "N of M audits left today" phrase, taken from whichever
// source the caller has.
func quotaOf(s HomeState) string {
	switch {
	case s.Usage != nil && s.Usage.AuditsLimit > 0:
		return itoa(s.Usage.AuditsRemaining) + " of " + itoa(s.Usage.AuditsLimit) + " audits left today"
	case s.Free != nil && s.Free.Limit > 0:
		return itoa(s.Free.Remaining) + " of " + itoa(s.Free.Limit) + " audits left today"
	case s.Free != nil:
		return itoa(s.Free.Remaining) + " audits left today"
	default:
		return ""
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
