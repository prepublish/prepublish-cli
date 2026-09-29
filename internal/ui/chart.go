package ui

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// chartRows is the height of the plotted area in terminal rows. A braille cell
// packs four dot rows into each one, so five rows is twenty vertical samples:
// enough shape to read a drop without turning a terminal report into a
// dashboard.
const chartRows = 5

// yAxisWidth is the label column: three digits and the axis glyph.
const yAxisWidth = 4

// brailleBits maps (row, column) inside a braille cell to its dot bit. Column 0
// is the left of the pair and rows run top to bottom, which is what makes the
// raster read the same way the terminal does.
var brailleBits = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// riskRamp colours the curve by the value it carries: the hold index is a risk
// measure, so a low passage reads red, a mid one amber and a high one green.
// Colouring by height rather than by a single accent is what lets a reader find
// the dangerous passage without reading a single number.
var riskRamp = lipgloss.Blend1D(101, th.danger, th.warning, th.success)

// chartMoment is one annotated point: its label in the marker row, the cell
// column it sits in, and its index into the (sorted) point list.
type chartMoment struct {
	label string
	col   int
	order int
}

// curve renders the attention-risk curve: a braille line chart across the full
// width, the y axis labelled 0-100, the x axis labelled 0%..100%, a hairline
// and a numbered marker for every point that carries an event, and a legend
// under the axis. duration, when known, converts a percentage into m:ss in the
// legend; passing nil keeps percentages only.
//
// The curve is drawn only where there is data. A curve that starts at 12% does
// not get a fabricated flat line back to 0%: the x axis is fixed, so a gap is
// the honest rendering of "this draft has no data there" rather than a claim
// about the opening.
func curve(points []api.RetentionPoint, duration *int, source string, w int) string {
	if len(points) == 0 {
		return st.dim.Render(wrapBlock("Script-level risk map unavailable for this analysis.", w))
	}
	plotW := max(w-yAxisWidth, 6)
	// The rightmost cell of the plot is an edge, not data: drawing it on every
	// row is what keeps the chart's rows the same width, and a chart whose rows
	// end at different columns reads as a rendering bug even when it is not.
	const edge = 1
	bodyW := max(plotW-edge, 4)
	dots := bodyW * 2
	dotRows := chartRows * 4

	pts := append([]api.RetentionPoint(nil), points...)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Timestamp < pts[j].Timestamp })

	vals := make([]float64, dots)
	for i := range vals {
		vals[i] = math.NaN()
	}
	dotX := func(ts int) int {
		x := int(math.Round(float64(clampInt(ts, 0, 100)) / 100 * float64(dots-1)))
		return clampInt(x, 0, dots-1)
	}
	if len(pts) == 1 {
		// One point is a level, not a shape: plot the single dot it is and let
		// the axis and the legend carry the meaning.
		vals[dotX(pts[0].Timestamp)] = pts[0].Retention
	}
	for i := range len(pts) - 1 {
		x0, x1 := dotX(pts[i].Timestamp), dotX(pts[i+1].Timestamp)
		if x1 <= x0 {
			vals[x1] = pts[i+1].Retention
			continue
		}
		for x := x0; x <= x1; x++ {
			t := float64(x-x0) / float64(x1-x0)
			vals[x] = pts[i].Retention + t*(pts[i+1].Retention-pts[i].Retention)
		}
	}

	// Rasterise into braille cells, remembering which column each cell belongs
	// to so the cell can take that column's colour.
	grid := make([][]byte, chartRows)
	cellValue := make([][]float64, chartRows)
	for r := range grid {
		grid[r] = make([]byte, plotW)
		cellValue[r] = make([]float64, plotW)
		for c := range cellValue[r] {
			cellValue[r][c] = math.NaN()
		}
	}
	for x, v := range vals {
		if math.IsNaN(v) {
			continue
		}
		y := dotRows - 1 - int(math.Round(clampFloat(v, 0, 100)/100*float64(dotRows-1)))
		y = clampInt(y, 0, dotRows-1)
		row, col := y/4, min(x/2, bodyW-1)
		grid[row][col] |= brailleBits[y%4][x%2]
		if math.IsNaN(cellValue[row][col]) {
			cellValue[row][col] = 0
		}
		cellValue[row][col] = math.Max(cellValue[row][col], v)
	}

	// Event markers: one hairline column per event, plus the event's number
	// above the plot. The hairline only fills cells the curve left empty, so it
	// never erases the data it annotates.
	var moments []chartMoment
	markerCols := map[int]bool{}
	for i, p := range pts {
		if strings.TrimSpace(p.Event) == "" {
			continue
		}
		col := dotX(p.Timestamp) / 2
		// The markers are numbered in the order the reader meets them, not by
		// their index in the curve: a reader counting "3" wants the third key
		// moment, and the legend below uses the same number.
		moments = append(moments, chartMoment{label: strconv.Itoa(len(moments) + 1), col: col, order: i})
		markerCols[col] = true
	}
	ruler := []rune(strings.Repeat(" ", plotW))
	for _, m := range moments {
		// Right-align the label on its column so a two-digit number still ends
		// where its hairline is, and fall back to the last digit when the extra
		// character would land on another label.
		digits := []rune(m.label)
		start := m.col - len(digits) + 1
		if start < 0 || start+len(digits) > len(ruler) {
			start = m.col
			digits = digits[len(digits)-1:]
		}
		clear := true
		for i := range digits {
			if start+i < 0 || start+i >= len(ruler) || ruler[start+i] != ' ' {
				clear = false
				break
			}
		}
		if !clear {
			start, digits = m.col, digits[len(digits)-1:]
		}
		for i, d := range digits {
			if col := start + i; col >= 0 && col < len(ruler) {
				ruler[col] = d
			}
		}
	}

	var b strings.Builder
	if len(moments) > 0 {
		b.WriteString(strings.Repeat(" ", yAxisWidth) +
			st.faint.Render(strings.TrimRight(string(ruler), " ")) + "\n")
	}
	for r := range chartRows {
		pct := 100 - r*(100/(chartRows-1))
		label := st.faint.Render(padLeft(strconv.Itoa(pct), 3) + "┤")
		b.WriteString(label + renderCells(grid[r][:bodyW], cellValue[r][:bodyW], markerCols) +
			st.faint.Render("│") + "\n")
	}
	b.WriteString("   " + st.faint.Render("└"+strings.Repeat("─", max(plotW-1, 1))) + "\n")
	b.WriteString("   " + st.faint.Render("0%") +
		strings.Repeat(" ", max(plotW-6, 1)) + st.faint.Render("100%"))

	if len(moments) > 0 {
		b.WriteString("\n\n" + keyMoments(pts, moments, duration, w))
	}
	if source == "interpolated" {
		b.WriteString("\n" + st.faint.Render(wrapBlock("Some points on this curve are interpolated.", w)))
	}
	return b.String()
}

// renderCells turns one raster row into styled runs. Consecutive cells of the
// same colour are emitted as one run: a hundred single-character escapes per
// row would be unreadable in a log and slow over ssh.
func renderCells(bits []byte, values []float64, markers map[int]bool) string {
	var b strings.Builder
	i := 0
	for i < len(bits) {
		if bits[i] == 0 {
			if markers[i] {
				b.WriteString(st.faint.Render("│"))
				i++
				continue
			}
			j := i
			for j < len(bits) && bits[j] == 0 && !markers[j] {
				j++
			}
			b.WriteString(strings.Repeat(" ", j-i))
			i = j
			continue
		}
		idx := rampIndex(values[i])
		style := lipgloss.NewStyle().Foreground(riskRamp[idx])
		j := i
		var run strings.Builder
		for j < len(bits) && bits[j] != 0 && rampIndex(values[j]) == idx {
			run.WriteRune(rune(0x2800) | rune(bits[j]))
			j++
		}
		b.WriteString(style.Render(run.String()))
		i = j
	}
	return b.String()
}

// rampIndex quantises a value onto the colour ramp, defaulting a cell with no
// value to the ramp's middle so a marker column can never panic.
func rampIndex(v float64) int {
	if math.IsNaN(v) {
		return 50
	}
	return clampInt(int(math.Round(clampFloat(v, 0, 100))), 0, 100)
}

// keyMoments lists the events the chart marked, numbered to match the markers
// above the plot. It is the legend, so it repeats the position in the reader's
// own units: m:ss when the draft has a known runtime, a percentage otherwise.
func keyMoments(pts []api.RetentionPoint, moments []chartMoment, duration *int, w int) string {
	var out []string
	for _, m := range moments {
		p := pts[m.order]
		pos := strconv.Itoa(p.Timestamp) + "%"
		if duration != nil && *duration > 0 {
			pos = clock(float64(p.Timestamp) / 100 * float64(*duration))
		}
		c := th.danger
		if strings.EqualFold(p.Sentiment, "positive") {
			c = th.success
		}
		head := " " + st.faint.Render(m.label) + " " + square(c) + " " + st.dim.Render(pos) + " "
		out = append(out, hangStyled(head, p.Event, w))
	}
	return strings.Join(out, "\n")
}

// hangStyled wraps plain text to the space left after a styled head and aligns
// the continuation under it. Wrapping the styled string instead would count the
// escape sequences as columns and overflow the width.
func hangStyled(head, body string, w int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return head
	}
	hw := lipgloss.Width(head)
	if budget := w - hw; budget >= 8 {
		return head + strings.Join(wrap(body, budget), "\n"+strings.Repeat(" ", hw))
	}
	return head + truncate(body, max(w-hw, 0))
}

// clock formats seconds as m:ss, the unit a creator reads a timestamp in.
func clock(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(math.Round(seconds)) * time.Second
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
