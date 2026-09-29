package ui

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The primitives every renderer is built from: pills, gauges, section heads,
// cards and label/value rows. They live here rather than in each renderer so
// the report, the tools and the account card cannot drift apart.

// badge is a filled pill: bold text on a solid colour with one space of
// padding. It is the report's only filled element, which is what makes a
// priority or a plan readable at a glance.
func badge(text string, c color.Color) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().
		Foreground(onColor(c)).
		Background(c).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

// tag is bare coloured bold text for a label that sits inside a sentence.
func tag(text string, c color.Color) string {
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(text)
}

// chip is a dim bracketed marker for a state the reader cannot act on here
// ("locked", "cached", "interpolated").
func chip(text string, c color.Color) string {
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(c).Render("[" + text + "]")
}

// square is the legend marker the chart and the key-moment list share.
func square(c color.Color) string { return lipgloss.NewStyle().Foreground(c).Render("▪") }

// lockedChip is the marker on a passage the server stripped. It is text, not a
// glyph: a padlock needs an emoji font to show up, and a report that renders
// "▯" where it means "locked" is worse than one that says the word.
func lockedChip() string { return chip("locked", th.faint) }

// pad is the row padding inside a gauge or a table: two columns, always.
const rowGap = " " + " "

// gauge renders one score as a labelled block bar with the value and, when
// there is room, the band's name. Colour comes from scoreColor, so the bar
// carries the same three bands as the web app.
func gauge(label string, score int, w int, withWord bool) string {
	score = clampInt(score, 0, 100)
	value := st.bold.Render(padLeft(strconv.Itoa(score), 3))

	// Below this the label and the bar cannot share a line without the bar
	// shrinking to nothing; the label goes above it instead.
	if w < 34 {
		barW := clampInt(w-4, 4, 40)
		return st.dim.Render(truncate(label, w)) + "\n" + value + " " + bar(score, barW)
	}

	withWord = withWord && w >= narrowAt
	extra := 0
	if withWord {
		extra = 2 + len("unknown")
	}
	barW := clampInt(w-labelWidth-6-extra, 8, 40)
	row := padRight(st.dim.Render(truncate(label, labelWidth)), labelWidth) + " " +
		bar(score, barW) + " " + value
	if withWord {
		row += "  " + lipgloss.NewStyle().Foreground(scoreColor(score)).Render(scoreWord(score))
	}
	return row
}

// bar is the score bar: the same block bar under every 0-100 number in the
// product, coloured by the band the score falls in.
func bar(score, w int) string { return barWith(score, scoreColor(score), w) }

// barWith is bar with an explicit colour, for the one gauge that is not a score
// (today's quota, which is a fraction rather than a verdict).
func barWith(score int, c color.Color, w int) string {
	if w < 1 {
		return ""
	}
	filled := int(math.Round(float64(clampInt(score, 0, 100)) / 100 * float64(w)))
	filled = clampInt(filled, 0, w)
	return lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(th.grid).Render(strings.Repeat("░", w-filled))
}

// sectionHead renders a numbered section head: the title on the left, optional
// meta pushed to the right edge, and a rule underneath spanning the width.
func sectionHead(num int, title, meta string, w int) string {
	left := st.title.Render(title)
	if num > 0 {
		left = st.faint.Render(strconv.Itoa(num)+" ") + left
	}
	head := left
	if meta != "" && w >= narrowAt {
		m := st.dim.Render(truncate(meta, max(w/2, 8)))
		if lipgloss.Width(left)+2+lipgloss.Width(m) <= w {
			head = padRight(left, w-lipgloss.Width(m)) + m
		}
	} else if meta != "" {
		m := st.dim.Render(truncate(meta, max(w-lipgloss.Width(left)-2, 4)))
		if lipgloss.Width(left)+2+lipgloss.Width(m) <= w {
			head = left + "  " + m
		}
	}
	return head + "\n" + rule(w)
}

// boxInner is the content width inside a box: one border column and one space
// of padding on each side.
func boxInner(w int) int { return max(w-4, 4) }

// box draws a rounded border with the title set into the top edge, padding
// every line to the box's inner width. Each body line must already fit
// boxInner(w) columns; the renderers build theirs with that budget.
func box(title string, body []string, w int) string {
	if w < 12 {
		return strings.Join(body, "\n")
	}
	inner := boxInner(w)
	edge := lipgloss.NewStyle().Foreground(th.border)

	top := "╭"
	if strings.TrimSpace(ansi.Strip(title)) == "" {
		top += edge.Render(strings.Repeat("─", w-2))
	} else {
		t := st.title.Render(truncate(title, max(w-8, 4)))
		top += edge.Render("─ ") + t + edge.Render(" "+strings.Repeat("─", max(w-5-lipgloss.Width(t), 1)))
	}
	top += edge.Render("╮")

	var b strings.Builder
	b.WriteString(top)
	for _, item := range body {
		// A body element may itself be a multi-line styled block (a wrapped
		// paragraph, a stage list); each of its lines needs its own border, so
		// the split happens here rather than at every call site.
		for _, line := range strings.Split(item, "\n") {
			b.WriteString("\n" + edge.Render("│") + " " + padRight(line, inner) + " " + edge.Render("│"))
		}
	}
	b.WriteString("\n" + edge.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

// card is box for a plain-text body: the text is wrapped to the box's inner
// width and styled on the way in.
func card(title, body string, w int) string {
	body = strings.TrimRight(body, "\n")
	return box(title, wrap(body, boxInner(w)), w)
}

// kv renders aligned label/value rows: the label in a dim column, the value
// wrapped under it. When the width cannot seat both, the label goes above the
// value rather than being truncated into meaninglessness.
func kv(rows [][2]string, w int) string {
	if len(rows) == 0 {
		return ""
	}
	keyW := 0
	for _, r := range rows {
		keyW = max(keyW, lipgloss.Width(r[0]))
	}
	stacked := keyW+4+12 > w
	if stacked {
		keyW = 0
	}
	var out []string
	for _, r := range rows {
		key, value := strings.TrimSpace(r[0]), strings.TrimSpace(r[1])
		if key == "" && value == "" {
			continue
		}
		if value == "" {
			out = append(out, st.dim.Render(key))
			continue
		}
		if stacked {
			out = append(out, st.dim.Render(key)+"\n"+hang(value, 0, 0, w))
			continue
		}
		out = append(out, hang(st.dim.Render(padRight(key, keyW))+rowGap+value, 0, keyW+2, w))
	}
	return strings.Join(out, "\n")
}

// clampInt is the numeric clamp the layout math reads better with.
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
