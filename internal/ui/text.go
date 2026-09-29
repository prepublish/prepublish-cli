package ui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// This file is the layout layer: everything here works on plain text and
// measures in display columns, so a styled block is built by wrapping first and
// styling the result. Styling before wrapping is what breaks layouts — an
// escape sequence counts towards the wrap unless the wrapper understands ANSI,
// and every renderer above would then need to.

// wrap breaks plain text into lines no wider than w display columns. Blank
// lines and paragraph breaks survive; a single word wider than w (a URL, a
// script id) is split rather than allowed to overhang.
func wrap(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		para = strings.TrimRight(para, " \t")
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		var line string
		lineW := 0
		for _, word := range strings.Fields(para) {
			for ansi.StringWidth(word) > w {
				head, rest := splitWord(word, w)
				if head == "" {
					break
				}
				if line != "" {
					out = append(out, line)
					line, lineW = "", 0
				}
				out = append(out, head)
				word = rest
			}
			ww := ansi.StringWidth(word)
			switch {
			case line == "":
				line, lineW = word, ww
			case lineW+1+ww <= w:
				line += " " + word
				lineW += 1 + ww
			default:
				out = append(out, line)
				line, lineW = word, ww
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// splitWord cuts the widest prefix of word that fits in w columns. A single
// rune wider than w is returned whole rather than vanishing, which is what
// keeps the caller's loop moving.
func splitWord(word string, w int) (head, rest string) {
	width := 0
	for i, r := range word {
		rw := ansi.StringWidth(string(r))
		if width+rw > w {
			if i == 0 {
				return word[:len(string(r))], word[len(string(r)):]
			}
			return word[:i], word[i:]
		}
		width += rw
	}
	return word, ""
}

// wrapBlock is wrap for the common case of wanting one string back.
func wrapBlock(s string, w int) string {
	return strings.Join(wrap(s, w), "\n")
}

// truncate shortens s to w columns with a single-character ellipsis. It is for
// text that has a better home elsewhere (a recorded report, a browser); prose
// in a report is wrapped instead.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// padRight and padLeft align a possibly styled string in a column. Both measure
// display width, so escape sequences do not count.
func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

func padLeft(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// indent prefixes every line, including blank ones, so a block keeps its
// internal shape when it is nested.
func indent(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	pad := strings.Repeat(" ", n)
	parts := strings.Split(s, "\n")
	for i, l := range parts {
		parts[i] = pad + l
	}
	return strings.Join(parts, "\n")
}

// hang wraps text and indents it, using one indent for the first line and
// another for the continuation. It is how a quoted passage, a bullet or a
// labelled paragraph stays aligned once it wraps.
func hang(s string, first, rest, w int) string {
	if w <= 0 {
		return s
	}
	budget := w - max(first, rest)
	if budget < 8 {
		budget = max(w-first, 4)
	}
	parts := wrap(s, budget)
	var b strings.Builder
	for i, l := range parts {
		if i > 0 {
			b.WriteString("\n")
		}
		if l == "" {
			continue
		}
		n := rest
		if i == 0 {
			n = first
		}
		b.WriteString(strings.Repeat(" ", n) + l)
	}
	return b.String()
}

// bullet renders a list as marker-plus-hanging-text. An empty slice renders
// nothing, so callers can build a list unconditionally.
func bullet(items []string, marker string, c color.Color, w int) string {
	head := lipgloss.NewStyle().Foreground(c).Render(marker)
	var out []string
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		out = append(out, hang(head+" "+wrapBlock(item, max(w-2, 4)), 0, 2, w))
	}
	return strings.Join(out, "\n")
}

// checkList is bullet with the ✓ / ✗ pair the report uses for strengths and
// weaknesses.
func checkList(items []string, ok bool, w int) string {
	marker, c := "✓", th.success
	if !ok {
		marker, c = "✗", th.danger
	}
	return bullet(items, marker, c, w)
}

// stack joins sections with sep, dropping the ones with nothing to say. A
// section that is empty therefore contributes no stray blank line.
func stack(sep string, parts ...string) string {
	var kept []string
	for _, s := range parts {
		if strings.TrimSpace(ansi.Strip(s)) != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, sep)
}

// rule is a dim horizontal rule across w columns.
func rule(w int) string {
	if w < 1 {
		return ""
	}
	return st.rule.Render(strings.Repeat("─", w))
}

// plural keeps the "1 improvement / 3 improvements" agreement out of the
// renderers.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// itoa is strconv.Itoa under the name the layout code reads better with: these
// are column widths and counts, not numbers anyone parses.
func itoa(n int) string { return strconv.Itoa(n) }

// count formats n with thousands separators, for word counts and byte sizes.
func count(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(",")
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
