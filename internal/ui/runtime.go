package ui

import (
	"github.com/prepublish/prepublish-cli/internal/scriptinfo"
)

// RenderRuntime renders the local word count and runtime estimate. It is the
// one renderer that never touches the network, and the one a creator reaches
// for before writing a word: "how long will this be".
func RenderRuntime(words int, e scriptinfo.Estimate, width int) string {
	w := contentWidth(width)
	inner := boxInner(w)

	words = max(words, 0)
	var body []string
	body = append(body, st.h1.Render(count(int64(words))+" words"))
	if words == 0 {
		body = append(body, st.dim.Render(wrapBlock("Nothing to count yet: pass a script file, pipe one in, or write a few lines first.", inner)))
		return box("Runtime estimate", body, w)
	}
	if e.Typical <= 0 {
		// A caller that forgot to derive the estimate still gets a real one.
		e = scriptinfo.Runtime(words)
	}

	const labelW, valueW = 8, 7
	for _, r := range []struct {
		label string
		d     int
		wpm   int
		lead  bool
	}{
		{"Slow", int(e.Slow.Seconds()), scriptinfo.SlowWPM, false},
		{"Typical", int(e.Typical.Seconds()), scriptinfo.TypicalWPM, true},
		{"Fast", int(e.Fast.Seconds()), scriptinfo.FastWPM, false},
	} {
		label, value := st.dim, st.body
		if r.lead {
			label, value = st.faint, st.bold
		}
		body = append(body, "  "+label.Render(padRight(r.label, labelW))+
			value.Render(padLeft(clock(float64(r.d)), valueW))+
			"  "+st.faint.Render("at "+itoa(r.wpm)+" wpm"))
	}
	card := box("Runtime estimate", body, w)

	hint := st.dim.Render(wrapBlock(
		"Delivery moves this by more than most people expect, which is why it is a range rather than a number.", w)) +
		"\n\n  " + st.key.Render("prepublish runtime --minutes 10") + "  " +
		st.dim.Render("for how many words fill a target length.")
	return fit(stack("\n\n", card, hint), w)
}
