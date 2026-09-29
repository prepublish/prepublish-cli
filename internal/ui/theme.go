// Package ui renders Prepublish's terminal output: one adaptive theme plus
// width-aware, pure renderers for reports, the standalone tools, the account
// card, runtime estimates, messages and errors.
//
// Every renderer takes the column width it must fit and returns a styled
// string. None of them touch the terminal, and each guarantees that no line it
// returns is wider than that width, so a caller can print the result
// unmodified — through lipgloss.Fprintln (which downsamples to the terminal's
// colour profile and honours NO_COLOR) or through a colorprofile writer.
//
// The palette is the web app's, translated: one signal colour (brand red),
// near-black surfaces separated by 1px borders, and a soft bone foreground
// rather than pure white. Scores carry their own semantics so a number means
// the same thing in every surface: green at 75 and up, amber from 50, red
// below.
package ui

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// MaxWidth is the column count the report is designed around. A wider terminal
// does not make the report wider: the line length is a reading decision, not a
// window size, and the chart and cards are laid out for this measure.
const MaxWidth = 100

// labelWidth is the left column of a score gauge, and narrowAt the width below
// which optional side-by-side detail is dropped rather than squeezed.
const (
	labelWidth = 12
	narrowAt   = 58
)

// theme is one resolved palette. The product is dark-first — near-black ink, a
// soft bone foreground, one signal colour — so that is the default; the light
// variant keeps the same semantic hues while darkening the ones that would
// otherwise read as pastel on white.
type theme struct {
	ink    color.Color // text set ON a coloured fill
	bone   color.Color
	fg     color.Color // body text
	muted  color.Color // secondary text
	faint  color.Color // tertiary: labels, rules, borders
	border color.Color

	accent  color.Color // brand red: risk, brand, selection
	success color.Color
	warning color.Color
	danger  color.Color // risk red
	info    color.Color
	orange  color.Color
	violet  color.Color
	grid    color.Color
}

// th is the process-wide palette. It is resolved once from the environment:
// the renderers are pure and must not query the terminal, and a theme that
// changed between two renders of the same report would be a bug.
var th = newTheme(detectDark())

func newTheme(dark bool) theme {
	if dark {
		return theme{
			ink:     lipgloss.Color("#0A0A0C"),
			bone:    lipgloss.Color("#F4F3F0"),
			fg:      lipgloss.Color("#F4F3F0"),
			muted:   lipgloss.Color("#A3A3AB"),
			faint:   lipgloss.Color("#71717A"),
			border:  lipgloss.Color("#232327"),
			accent:  lipgloss.Color("#EF4444"),
			success: lipgloss.Color("#22C55E"),
			warning: lipgloss.Color("#EAB308"),
			danger:  lipgloss.Color("#EF4444"),
			info:    lipgloss.Color("#3B82F6"),
			orange:  lipgloss.Color("#F97316"),
			violet:  lipgloss.Color("#8B5CF6"),
			grid:    lipgloss.Color("#1C1C20"),
		}
	}
	return theme{
		ink:     lipgloss.Color("#18181B"),
		bone:    lipgloss.Color("#FAFAFA"),
		fg:      lipgloss.Color("#18181B"),
		muted:   lipgloss.Color("#52525B"),
		faint:   lipgloss.Color("#71717A"),
		border:  lipgloss.Color("#E4E4E7"),
		accent:  lipgloss.Color("#DC2626"),
		success: lipgloss.Color("#15803D"),
		warning: lipgloss.Color("#A16207"),
		danger:  lipgloss.Color("#DC2626"),
		info:    lipgloss.Color("#1D4ED8"),
		orange:  lipgloss.Color("#C2410C"),
		violet:  lipgloss.Color("#6D28D9"),
		grid:    lipgloss.Color("#E4E4E7"),
	}
}

// detectDark decides the palette without querying the terminal (a background
// query blocks for up to a second and a half, which is unacceptable in a CI
// pipe and visible in an interactive one).
//
// COLORFGBG is the one signal terminals do publish: "fg;bg" written on exit by
// xterm, rxvt, konsole, iTerm and friends. When it is absent — most terminals
// today, including the ones that answer a background query perfectly well — the
// brand's dark palette is the default, which is what the product looks like.
func detectDark() bool {
	v := os.Getenv("COLORFGBG")
	if v == "" {
		return true
	}
	parts := strings.Split(v, ";")
	n, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return true
	}
	// 7 is white and 15 is bright white; 8 (bright black, i.e. grey-on-black) is
	// still a dark background.
	switch {
	case n == 7 || n == 15:
		return false
	case n == 8:
		return true
	default:
		return n < 7
	}
}

// styles is the resolved set of reusable styles. Every renderer composes these
// rather than calling lipgloss.NewStyle inline, so the report cannot drift from
// the account card.
type styles struct {
	title    lipgloss.Style // section and card titles
	h1       lipgloss.Style // the report's own title
	bold     lipgloss.Style
	body     lipgloss.Style
	dim      lipgloss.Style // secondary text
	faint    lipgloss.Style // labels, meta, rules
	rule     lipgloss.Style
	quote    lipgloss.Style // the creator's own words
	improved lipgloss.Style // the rewrite that replaces them
	key      lipgloss.Style // key hints and commands
	accent   lipgloss.Style
	good     lipgloss.Style
	warn     lipgloss.Style
	bad      lipgloss.Style
}

var st = newStyles(th)

func newStyles(t theme) styles {
	return styles{
		title:    lipgloss.NewStyle().Foreground(t.fg).Bold(true),
		h1:       lipgloss.NewStyle().Foreground(t.fg).Bold(true),
		bold:     lipgloss.NewStyle().Foreground(t.fg).Bold(true),
		body:     lipgloss.NewStyle().Foreground(t.fg),
		dim:      lipgloss.NewStyle().Foreground(t.muted),
		faint:    lipgloss.NewStyle().Foreground(t.faint),
		rule:     lipgloss.NewStyle().Foreground(t.grid),
		quote:    lipgloss.NewStyle().Foreground(t.muted).Italic(true),
		improved: lipgloss.NewStyle().Foreground(t.fg).Bold(true),
		key:      lipgloss.NewStyle().Foreground(t.accent).Bold(true),
		accent:   lipgloss.NewStyle().Foreground(t.accent),
		good:     lipgloss.NewStyle().Foreground(t.success),
		warn:     lipgloss.NewStyle().Foreground(t.warning),
		bad:      lipgloss.NewStyle().Foreground(t.danger),
	}
}

// contentWidth normalises a caller's width the way every renderer needs it:
// zero or negative means "the default measure", anything above MaxWidth is
// capped, and a narrower width is honoured exactly — the alternative is
// wrapping the terminal itself.
func contentWidth(w int) int {
	if w <= 0 {
		return MaxWidth
	}
	if w > MaxWidth {
		return MaxWidth
	}
	return w
}

// Banner is the gradient wordmark plus the product line, shown in the home
// screen and in `prepublish --help`.
func Banner() string {
	const word = "prepublish"
	runes := []rune(word)
	ramp := lipgloss.Blend1D(len(runes), th.accent, th.orange)
	var b strings.Builder
	for i, r := range runes {
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Bold(true).Render(string(r)))
	}
	tagline := st.dim.Render("Find the passages most likely to lose attention — before you record.")
	return b.String() + "\n" + tagline
}

// ReportURL is the permalink of a report in the web app. It is the same URL a
// signed-in browser would open, which is what makes it useful in a terminal: it
// is the one place a shared report can be read in full.
func ReportURL(appURL, id string) string {
	if id == "" {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" {
		base = "https://prepublish.ai"
	}
	return base + "/analysis/" + id
}

// Success, Info and Warn are the one-line status messages the commands print
// outside a TUI. They are deliberately glyph-and-colour only: a message that
// needs a paragraph belongs in a renderer, and one that needs a box belongs in
// a card.
func Success(msg string) string { return message("✔", th.success, msg) }

func Info(msg string) string { return message("•", th.info, msg) }

func Warn(msg string) string { return message("!", th.warning, msg) }

func message(glyph string, c color.Color, msg string) string {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return ""
	}
	head := lipgloss.NewStyle().Foreground(c).Bold(true)
	lines := strings.Split(msg, "\n")
	pad := strings.Repeat(" ", lipgloss.Width(glyph)+1)
	var b strings.Builder
	for i, line := range lines {
		switch i {
		case 0:
			b.WriteString(head.Render(glyph) + " " + line)
		default:
			b.WriteString("\n" + pad + line)
		}
	}
	return b.String()
}

// scoreColor is the one place the score thresholds live. 75 and up is green,
// 50-74 amber, below 50 red; the web app paints the same three bands, so a
// score reads the same wherever it is seen.
func scoreColor(score int) color.Color {
	switch {
	case score >= 75:
		return th.success
	case score >= 50:
		return th.warning
	default:
		return th.danger
	}
}

// scoreWord is the band's name, for the places with room for a word next to the
// number.
func scoreWord(score int) string {
	switch {
	case score >= 75:
		return "strong"
	case score >= 50:
		return "fair"
	case score > 0:
		return "weak"
	default:
		return "unknown"
	}
}

// priorityColor maps an improvement's priority onto the same red-to-blue ramp
// the web app uses, so the ordering survives greyscale: CRITICAL is the risk
// colour, LOW the calm one.
func priorityColor(p string) color.Color {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "CRITICAL":
		return th.danger
	case "HIGH":
		return th.orange
	case "MEDIUM":
		return th.warning
	case "LOW":
		return th.info
	default:
		return th.faint
	}
}

// severityColor is the policy/authenticity counterpart of priorityColor.
func severityColor(s string) color.Color {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "high", "high_matches":
		return th.danger
	case "medium", "review_suggested", "moderate":
		return th.warning
	case "low", "no_matches", "none":
		return th.success
	default:
		return th.faint
	}
}

// riskColor maps a risk level ("low", "medium", "high") onto its colour.
func riskColor(level string) color.Color {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low":
		return th.success
	case "medium", "moderate":
		return th.warning
	case "high":
		return th.danger
	default:
		return th.faint
	}
}

// onColor picks the readable foreground for a filled pill. The brand reds and
// blues want bone, the light greens and ambers want ink; measuring the fill is
// cheaper than a second table that can drift.
func onColor(bg color.Color) color.Color {
	if luminance(bg) > 0.55 {
		return th.ink
	}
	return th.bone
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 65535
}

// tierLabel and tierColor name the plan the way the product does: Creator and
// Studio are the paid tiers, and an anonymous caller is not a "free account".
func tierLabel(tier string) string {
	switch tier {
	case "anonymous", "":
		return "Anonymous"
	case "free_account":
		return "Free"
	case "paid":
		return "Creator"
	case "studio":
		return "Studio"
	default:
		return tier
	}
}

func tierColor(tier string) color.Color {
	switch tier {
	case "anonymous", "":
		return th.faint
	case "free_account":
		return th.info
	case "paid":
		return th.accent
	case "studio":
		return th.violet
	default:
		return th.faint
	}
}
