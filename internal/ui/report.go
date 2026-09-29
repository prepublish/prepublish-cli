package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/scriptinfo"
)

// The report is the product: the one screen a creator reads twice — once to
// decide what to fix, once to check the fix landed. Its order is therefore
// fixed and deliberate: what this is, how it scored, where attention drops,
// the single fix that matters most, every fix in priority order, the title,
// the three written components, then the two risk sections, then the history of
// the draft chain, then the honest scope.
//
// Everything is width-aware: sections drop their side-by-side detail on a
// narrow terminal rather than overflowing, and no line ever exceeds the width
// the caller asked for.

// appURL is the base a rendered report link points at. It is a variable rather
// than a constant so a CLI configured with --app-url points the footer at the
// same host its commands open, while the default keeps the renderer usable with
// no setup at all.
var appURL = config.DefaultAppURL

// SetAppURL points every report link rendered afterwards at base. Call it once
// at startup when the app URL is configurable.
func SetAppURL(base string) {
	if strings.TrimSpace(base) != "" {
		appURL = strings.TrimRight(strings.TrimSpace(base), "/")
	}
}

// RenderReport renders one audit. A completed audit renders the full report; a
// pending one renders its stage checklist and progress; a failed one renders
// what the worker said. The width is the terminal's, and no returned line is
// wider than it.
func RenderReport(a *api.Analysis, width int) string {
	if a == nil {
		return ""
	}
	w := contentWidth(width)
	switch a.Status {
	case api.StatusCompleted:
		return fit(completedReport(a, w), w)
	case api.StatusFailed:
		return fit(failedReport(a, w), w)
	default:
		return fit(pendingReport(a, w), w)
	}
}

// reportBuilder numbers the sections it actually renders, so a report with no
// authenticity section is not left with a gap in its numbering.
type reportBuilder struct {
	n int
	w int
}

func (b *reportBuilder) section(title, meta, body string) string {
	if strings.TrimSpace(ansi.Strip(body)) == "" {
		return ""
	}
	b.n++
	return sectionHead(b.n, title, meta, b.w) + "\n" + body
}

func completedReport(a *api.Analysis, w int) string {
	b := &reportBuilder{w: w}
	return strings.TrimRight(stack("\n\n",
		headerCard(a, w),
		b.section("Scores", scoreMeta(a), scoreBlock(a, w)),
		b.section("Script attention-risk map", curveMeta(a),
			curve(a.RetentionCurve, a.VideoDuration, a.RetentionCurveSource, w)),
		b.section("Fix this first", "", keyFix(a, w)),
		b.section("Improvements", improvementsMeta(a), improvementsBlock(a, w)),
		b.section("Title", "", titleBlock(a, w)),
		b.section("Hook, structure and pacing", "", componentsBlock(a, w)),
		b.section("Authenticity", authenticityMeta(a), authenticityBlock(a, w)),
		b.section("Policy pre-flight", policyMeta(a), policyBlock(a, w)),
		b.section("Revision", revisionMeta(a), revisionBlock(a, w)),
		reportFooter(a, w),
	), "\n")
}

// pendingReport is the report before it exists. It uses the worker's own stage
// names, so "computing saliency" in the terminal and "computing saliency" in
// the log mean the same thing.
func pendingReport(a *api.Analysis, w int) string {
	progress := clampInt(a.Progress, 0, 100)
	inner := boxInner(w)
	var body []string
	if title := strings.TrimSpace(a.VideoTitle); title != "" {
		body = append(body, st.h1.Render(wrapBlock(title, inner)))
	}
	body = append(body, badge(statusLabel(a.Status), statusColor(a.Status))+"  "+
		st.dim.Render(itoa(progress)+"% complete"))
	body = append(body, "")
	body = append(body, stageList(a.Status, progress, inner))
	head := box("Audit "+statusLabel(a.Status), body, w)

	var detail []string
	if a.Status == api.StatusTranscribing {
		detail = append(detail, st.dim.Render(wrapBlock("The worker is transcribing the file; the rest of the audit starts as soon as it has a script.", w)))
	}
	where := st.key.Render(ReportURL(appURL, a.ID))
	footer := st.dim.Render(wrapBlock(
		"The audit keeps running on the server. Re-run this command to pick it up where it is, or read it in the browser:", w)) +
		"\n" + where
	return head + "\n\n" + stack("\n\n", stack("\n", detail...), footer)
}

// failedReport says what happened and what to do next. The worker's message is
// passed through rather than paraphrased: it is written for the creator.
func failedReport(a *api.Analysis, w int) string {
	var body []string
	if title := strings.TrimSpace(a.VideoTitle); title != "" {
		body = append(body, st.h1.Render(wrapBlock(title, boxInner(w))))
	}
	body = append(body, badge("failed", th.danger)+"  "+st.faint.Render(truncate(a.ID, max(w/2, 8))))
	head := box("Audit failed", body, w)

	reason := ""
	if a.ErrorMessage != nil {
		reason = strings.TrimSpace(*a.ErrorMessage)
	}
	if reason == "" {
		reason = "The worker stopped before this audit finished. Running it again usually clears it."
	}
	out := []string{
		head,
		st.body.Render(wrapBlock(reason, w)),
		st.dim.Render(wrapBlock("Nothing was lost and nothing was spent twice: the audit failed on the server, not on your draft. Run the audit again; if it keeps failing the draft is usually not the cause.", w)),
	}
	if link := ReportURL(appURL, a.ID); link != "" {
		out = append(out, st.key.Render(link))
	}
	return strings.Join(out, "\n\n")
}

// headerCard is the report's identity: what was audited, whether it is done,
// which plan produced it, and how long the draft is.
func headerCard(a *api.Analysis, w int) string {
	inner := boxInner(w)
	var body []string
	title := strings.TrimSpace(a.VideoTitle)
	if title == "" {
		title = "Untitled draft"
	}
	body = append(body, st.h1.Render(wrapBlock(title, inner)))

	badges := []string{badge(statusLabel(a.Status), statusColor(a.Status))}
	if a.ReadyToRecord {
		badges = append(badges, badge("ready to record", th.success))
	}
	if a.Tier != "" {
		badges = append(badges, badge(tierLabel(a.Tier), tierColor(a.Tier)))
	}
	body = append(body, strings.Join(badges, " "))

	var meta []string
	words := scriptinfo.WordCount(a.ScriptText)
	if words > 0 {
		meta = append(meta, count(int64(words))+" words")
	}
	switch {
	case a.VideoDuration != nil && *a.VideoDuration > 0:
		meta = append(meta, clock(float64(*a.VideoDuration))+" runtime")
	case words > 0:
		meta = append(meta, "≈"+clock(scriptinfo.Runtime(words).Typical.Seconds())+" to read")
	}
	if n := len(a.RetentionCurve); n > 0 {
		meta = append(meta, plural(n, "curve point", "curve points"))
	}
	if !a.CreatedAt.IsZero() {
		meta = append(meta, a.CreatedAt.Local().Format("2 Jan 2006 15:04"))
	}
	if len(meta) > 0 {
		body = append(body, st.dim.Render(wrapBlock(strings.Join(meta, " · "), inner)))
	}
	body = append(body, st.faint.Render(truncate(a.ID, inner)))
	return box("Report", body, w)
}

func scoreMeta(a *api.Analysis) string {
	parts := []string{"relative 0–100"}
	if a.ProcessingTimeMs != nil && *a.ProcessingTimeMs > 0 {
		parts = append(parts, "audited in "+clock(float64(*a.ProcessingTimeMs)/1000))
	}
	return strings.Join(parts, " · ")
}

// scoreBlock renders the overall score and the three components, each followed
// by the model's explanation for it. The explanations are why the numbers are
// worth anything: without them a gauge is a verdict with no evidence.
func scoreBlock(a *api.Analysis, w int) string {
	word := w >= narrowAt
	rows := []string{gauge("Overall", a.OverallScore, w, word)}
	for _, c := range []struct {
		label string
		score *int
		key   string
	}{
		{"Hook", a.HookScore, "hook"},
		{"Structure", a.StructureScore, "structure"},
		{"Pacing", a.PacingScore, "pacing"},
	} {
		if c.score == nil {
			continue
		}
		rows = append(rows, gauge(c.label, *c.score, w, word))
		if e := explanation(a, c.key); e != "" {
			rows = append(rows, st.dim.Render(hang(wrapBlock(e, max(w-labelWidth-1, 8)), labelWidth+1, labelWidth+1, w)))
		}
	}
	return strings.Join(rows, "\n")
}

// explanation is the model's own sentence about one component. The API explains
// the three components, not the overall score, so "overall" has none.
func explanation(a *api.Analysis, which string) string {
	if a.ScoreExplanations == nil {
		return ""
	}
	switch which {
	case "hook":
		return strings.TrimSpace(a.ScoreExplanations.Hook)
	case "structure":
		return strings.TrimSpace(a.ScoreExplanations.Structure)
	case "pacing":
		return strings.TrimSpace(a.ScoreExplanations.Pacing)
	default:
		return ""
	}
}

func curveMeta(a *api.Analysis) string {
	var meta []string
	if n := len(a.RetentionCurve); n > 0 {
		meta = append(meta, plural(n, "point", "points"))
		if events := countEvents(a.RetentionCurve); events > 0 {
			meta = append(meta, plural(events, "key moment", "key moments"))
		}
		if a.RetentionCurveSource == "interpolated" {
			meta = append(meta, "interpolated")
		}
	}
	return strings.Join(meta, " · ")
}

func countEvents(points []api.RetentionPoint) int {
	n := 0
	for _, p := range points {
		if strings.TrimSpace(p.Event) != "" {
			n++
		}
	}
	return n
}

// keyFix is the one change the model would make first. It carries the priority
// and the position of the top improvement too: "fix this first" without a
// number is advice, and with one it is a task.
func keyFix(a *api.Analysis, w int) string {
	text := strings.TrimSpace(a.OneKeyImprovement)
	if text == "" {
		return ""
	}
	inner := boxInner(w)
	var body []string
	if len(a.Improvements) > 0 {
		top := a.Improvements[0]
		var head []string
		if top.Priority != "" {
			head = append(head, badge(strings.ToUpper(top.Priority), priorityColor(top.Priority)))
		}
		if top.Timestamp != "" {
			head = append(head, st.dim.Render(top.Timestamp))
		}
		if len(head) > 0 {
			body = append(body, strings.Join(head, " "))
		}
		// The title is wrapped rather than truncated: it is the name of the fix
		// the whole card is about.
		if title := strings.TrimSpace(top.Title); title != "" {
			body = append(body, st.bold.Render(wrapBlock(title, inner)))
		}
	}
	body = append(body, st.body.Render(wrapBlock(text, inner)))
	return box("", body, w)
}

func improvementsMeta(a *api.Analysis) string {
	total := len(a.Improvements)
	if total == 0 {
		return ""
	}
	locked := countLocked(a.Improvements)
	if locked == 0 {
		return plural(total, "fix", "fixes")
	}
	return plural(total-locked, "fix", "fixes") + " unlocked · " + plural(locked, "locked", "locked")
}

func countLocked(items []api.Improvement) int {
	n := 0
	for _, imp := range items {
		if imp.Locked {
			n++
		}
	}
	return n
}

// improvementsBlock renders every fix in priority order, numbered as the API
// ordered them. An unlocked fix gets the whole block: problem, quote, rewrite,
// reason, risk. A locked one gets a single dim line — its priority, position,
// title and the `locked` marker, which is what the server leaves in the
// response — followed by the count line and the one way to unlock the rest.
//
// Listing the locked ones rather than hiding them is deliberate: the shape of
// what is behind the plan limit is the honest thing to show, and the rewrites
// themselves were never in the response to leak.
func improvementsBlock(a *api.Analysis, w int) string {
	var out []string
	locked := 0
	for n, imp := range a.Improvements {
		if n > 0 {
			out = append(out, "")
		}
		if imp.Locked {
			locked++
			out = append(out, lockedItem(n+1, imp, w))
			continue
		}
		out = append(out, improvementItem(n+1, imp, w))
	}
	if locked > 0 {
		out = append(out, "", lockedLine(len(a.Improvements), locked, w), "", unlockHint(a, w))
	}
	return strings.Join(out, "\n")
}

// lockedItem is the collapsed form: one line, no rewrite, no reason, and the
// marker that says why it is short. The marker trails the title rather than
// sitting in the middle of the head, and the category drops out when the line
// would not otherwise have room for a readable title.
func lockedItem(n int, imp api.Improvement, w int) string {
	title := firstNonEmpty(imp.Title, problemLabel(imp))
	chipText := " " + lockedChip()

	head := []string{st.faint.Render(itoa(n))}
	if imp.Priority != "" {
		head = append(head, badge(strings.ToUpper(imp.Priority), priorityColor(imp.Priority)))
	}
	if imp.Timestamp != "" {
		head = append(head, st.dim.Render(imp.Timestamp))
	}
	headText := " " + strings.Join(head, " ") + " "
	if imp.Category != "" && lipgloss.Width(headText)+len(imp.Category)+1+lipgloss.Width(chipText)+24 <= w-2 {
		headText += st.faint.Render(imp.Category) + " "
	}

	headW := lipgloss.Width(headText)
	lines := wrap(title, max(w-2-headW-lipgloss.Width(chipText), 8))
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		prefix := strings.Repeat(" ", headW)
		if i == 0 {
			prefix = headText
		}
		suffix := ""
		if i == len(lines)-1 {
			suffix = chipText
		}
		out = append(out, prefix+st.dim.Render(l)+suffix)
	}
	return strings.Join(out, "\n")
}

func improvementItem(n int, imp api.Improvement, w int) string {
	var head []string
	head = append(head, st.faint.Render(itoa(n)))
	if imp.Priority != "" {
		head = append(head, badge(strings.ToUpper(imp.Priority), priorityColor(imp.Priority)))
	}
	if imp.Timestamp != "" {
		head = append(head, st.dim.Render(imp.Timestamp))
	}
	if imp.Category != "" {
		head = append(head, st.faint.Render(imp.Category))
	}
	title := strings.TrimSpace(imp.Title)
	if title == "" {
		title = problemLabel(imp)
	}
	block := " " + hangStyled(" "+strings.Join(head, " ")+" ", title, w-2)

	const indent = 3
	problem := firstNonEmpty(imp.ProblemSummary, imp.Description, imp.Impact)
	why := firstNonEmpty(imp.Why, imp.WhyItWorks)
	if sameText(problem, why) {
		// The API regularly sends the same sentence as the problem and as the
		// reason; printing it twice under two labels reads like a bug.
		why = ""
	}
	fields := stack("\n",
		reportField("Problem", problem, st.dim, indent, w),
		reportField("Now", quote(firstNonEmpty(imp.OriginalQuote, imp.Current)), st.quote, indent, w),
		reportHighlight("Try", imp.Improved, indent, w),
		reportField("Why", why, st.body, indent, w),
		reportField("Risk", imp.RetentionImpact, st.dim, indent, w),
	)
	return stack("\n", block, fields)
}

// sameText reports whether two pieces of prose say the same thing, ignoring
// case, whitespace runs and trailing punctuation — the differences a model
// introduces when it repeats itself.
func sameText(a, b string) bool {
	na, nb := normalizeText(a), normalizeText(b)
	return na != "" && na == nb
}

func normalizeText(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(s), " "))
	return strings.TrimRight(s, " .,:;!?…")
}

// problemLabel turns a problem type into a readable label for the fixes the
// model left untitled.
func problemLabel(imp api.Improvement) string {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(imp.ProblemType, "_", " ")))
	if s == "" {
		return "Needs a rewrite"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// lockedLine is the count line: how much of the report this plan can read. The
// per-item markers above already say which fixes are held back, so this line
// only has to say how much is left behind.
func lockedLine(total, locked int, w int) string {
	readable := total - locked
	head := itoa(readable) + " of " + itoa(total) + " fixes are readable on this plan."
	if readable == 1 {
		head = "1 of " + itoa(total) + " fixes is readable on this plan."
	}
	if readable == 0 {
		head = "All " + itoa(total) + " fixes are locked on this plan."
	}
	return " " + st.dim.Render(wrapBlock(head, max(w-1, 8)))
}

// unlockHint is the call to action the gated sections share. The lock reason
// picks it: an anonymous caller has an account to create, a free account has a
// plan to buy.
func unlockHint(a *api.Analysis, w int) string {
	var text, command string
	switch a.LockReason {
	case api.LockReasonSignupRequired:
		text = "Create a free account to read every rewrite, the title suggestion and the policy passages on this report."
		command = "prepublish login"
	case api.LockReasonUpgradeRequired:
		text = "Creator unlocks full reports on every audit: every rewrite, the title suggestion and the policy passages."
		command = "prepublish upgrade"
	default:
		return st.dim.Render(wrapBlock("Sign in or upgrade for the full report.", w))
	}
	return st.body.Render(wrapBlock(text, w)) + "\n  " + st.key.Render(command)
}

func titleBlock(a *api.Analysis, w int) string {
	if a.TitleRewrite == nil {
		if a.Locked {
			return st.dim.Render(wrapBlock("Title suggestions are part of the full report: "+lockNote(a), w))
		}
		return ""
	}
	t := a.TitleRewrite
	const indent = 2
	return stack("\n",
		reportField("Now", t.Original, st.quote, indent, w),
		reportHighlight("Try", t.Improved, indent, w),
		reportField("Why", t.Why, st.body, indent, w),
	)
}

func lockNote(a *api.Analysis) string {
	switch a.LockReason {
	case api.LockReasonSignupRequired:
		return "run `prepublish login` to see it."
	case api.LockReasonUpgradeRequired:
		return "run `prepublish upgrade` to unlock it."
	default:
		return "sign in to see it."
	}
}

// componentsBlock is the written half of the report: what the hook does, how
// the script is shaped, and how it paces. The scores live in the Scores
// section, so this one is only the reasoning.
//
// A component with nothing to say contributes no heading. A gated payload
// carries strengths and weaknesses but no suggestions, and a payload whose
// model returned nothing at all would otherwise leave three bare headings
// standing over empty space.
func componentsBlock(a *api.Analysis, w int) string {
	var out []string
	if h := a.HookAnalysis; h != nil {
		var meta []string
		if h.HookDuration > 0 {
			meta = append(meta, "first "+clock(float64(h.HookDuration))+" · "+scoreWord(h.Score))
		}
		if g := strings.TrimSpace(h.AttentionGrabber); g != "" {
			meta = append(meta, "grabber: "+g)
		}
		out = append(out, componentBlock("Hook", meta, h.Strengths, h.Weaknesses, "", w)...)
	}
	if s := a.StructureAnalysis; s != nil {
		var meta []string
		if s.FlowRating != "" {
			meta = append(meta, "flow: "+s.FlowRating)
		}
		out = append(out, componentBlock("Structure", meta, s.Strengths, s.Weaknesses, sectionRows(s.Sections, w), w)...)
	}
	if p := a.PacingAnalysis; p != nil {
		var meta []string
		if p.OverallPace != "" {
			meta = append(meta, p.OverallPace)
		}
		if p.EnergyVariation != "" {
			meta = append(meta, "variation: "+p.EnergyVariation)
		}
		if len(p.DropoffRiskPoints) > 0 {
			meta = append(meta, "riskiest at "+percentList(p.DropoffRiskPoints))
		}
		out = append(out, componentBlock("Pacing", meta, p.Strengths, p.Weaknesses, "", w)...)
	}
	return stack("\n", out...)
}

// componentBlock renders one written component: a heading, its meta line, its
// strengths and its weaknesses, plus whatever extra block it owns (the
// structure list). It returns nothing when there is nothing to render, which is
// what removes an empty component from the report instead of leaving a heading
// over blank lines.
func componentBlock(name string, meta, strengths, weaknesses []string, extra string, w int) []string {
	if len(meta) == 0 && len(strengths) == 0 && len(weaknesses) == 0 && strings.TrimSpace(ansi.Strip(extra)) == "" {
		return nil
	}
	head := tag(name, th.fg)
	if len(meta) > 0 {
		head = hangStyled(head+"  ", strings.Join(meta, " · "), w)
	}
	out := []string{head}
	if body := checkList(strengths, true, w); body != "" {
		out = append(out, indent(body, 2))
	}
	if body := checkList(weaknesses, false, w); body != "" {
		out = append(out, indent(body, 2))
	}
	if strings.TrimSpace(ansi.Strip(extra)) != "" {
		out = append(out, extra)
	}
	return out
}

// sectionRows is the structure breakdown as a compact list: where each part
// starts and ends, and how it reads.
func sectionRows(sections []api.ContentSection, w int) string {
	if len(sections) == 0 {
		return ""
	}
	const spanW = 8
	var out []string
	for _, s := range sections {
		quality := strings.TrimSpace(s.Quality)
		qW := 0
		if quality != "" && w >= 40 {
			qW = lipgloss.Width(quality) + 2
		} else {
			quality = ""
		}
		titleW := max(w-2-spanW-2-qW, 6)
		line := "  " + st.faint.Render(padLeft(itoa(s.StartPercentage)+"–"+itoa(s.EndPercentage)+"%", spanW)) +
			"  " + st.body.Render(truncate(s.Title, titleW))
		if quality != "" {
			line += "  " + st.dim.Render(quality)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func authenticityMeta(a *api.Analysis) string {
	if a.Authenticity == nil {
		return ""
	}
	return "partner program · " + strings.ToLower(a.Authenticity.RiskLevel) + " risk"
}

// authenticityBlock is the Partner Program "inauthentic or reused content"
// check inside a report. The score runs the other way from every other gauge in
// the report — 100 is fully authentic — so the line under it says that out
// loud rather than leaving the reader to infer it from the colour.
func authenticityBlock(a *api.Analysis, w int) string {
	au := a.Authenticity
	if au == nil {
		return ""
	}
	var out []string
	out = append(out, gauge("Authenticity", au.Score, w, false))
	out = append(out, st.faint.Render(wrapBlock(
		"100 is fully authentic. A low score means the draft leans on reused or formulaic material.", w)))
	if body := checkList(au.Issues, false, w); body != "" {
		out = append(out, body)
	}
	if body := bullet(au.Suggestions, "→", th.info, w); body != "" {
		out = append(out, body)
	}
	return strings.Join(out, "\n")
}

func policyMeta(a *api.Analysis) string {
	p := a.PolicyPreflight
	if p == nil {
		return ""
	}
	meta := verdictLabel(p.Verdict)
	if p.Locked {
		meta += " · summary only"
	}
	if p.RubricVersion > 0 {
		meta += " · rubric v" + itoa(p.RubricVersion)
	}
	return meta
}

// policyBlock is the in-report policy summary. The full per-category rendering
// lives in RenderPolicy; here the reader gets the verdict, the passages that
// matched, and the sentence that keeps the claim honest.
func policyBlock(a *api.Analysis, w int) string {
	p := a.PolicyPreflight
	if p == nil {
		return ""
	}
	line := badge(verdictLabel(p.Verdict), severityColor(p.Verdict))
	if p.TotalFlags > 0 {
		line += "  " + st.dim.Render(plural(p.TotalFlags, "flag", "flags"))
	}
	out := []string{line}
	out = append(out, flagList(p, w)...)
	if p.Locked && (p.TotalFlags > 0 || len(p.Flags) > 0) {
		out = append(out, "", st.dim.Render(wrapBlock("The matched passages are part of the full report: "+lockNote(a), w)))
	}
	if scope := strings.TrimSpace(p.ScopeStatement); scope != "" {
		out = append(out, "", st.faint.Render(wrapBlock(scope, w)))
	}
	// Joined, not stacked: the blank separator lines above are deliberate
	// paragraph breaks, and stack() would swallow them.
	return strings.Join(out, "\n")
}

func revisionMeta(a *api.Analysis) string {
	if a.RevisionSummary == nil {
		return ""
	}
	return "draft " + itoa(a.RevisionSummary.DraftNumber)
}

// revisionBlock is the draft chain: what this rewrite fixed. It is the one
// section that can show a number moving the right way twice.
func revisionBlock(a *api.Analysis, w int) string {
	r := a.RevisionSummary
	if r == nil {
		return ""
	}
	var out []string
	delta := a.OverallScore - r.PreviousScore
	deltaText, deltaColor := "+"+itoa(delta), th.success
	switch {
	case delta < 0:
		deltaText, deltaColor = itoa(delta), th.danger
	case delta == 0:
		deltaText, deltaColor = "±0", th.faint
	}
	out = append(out, "Score "+itoa(r.FirstScore)+" (draft 1) → "+itoa(r.PreviousScore)+" → "+
		st.bold.Render(itoa(a.OverallScore))+"   "+tag(deltaText, deltaColor))
	if r.ResolvedCount > 0 {
		out = append(out, st.dim.Render("Resolved "+plural(r.ResolvedCount, "earlier fix", "earlier fixes")+" in this draft."))
	}
	var implemented []string
	for _, advice := range r.Implemented {
		item := strings.TrimSpace(advice.Title)
		if item == "" {
			continue
		}
		if advice.Draft > 0 {
			item += "  " + st.faint.Render("(draft "+itoa(advice.Draft)+")")
		}
		implemented = append(implemented, item)
	}
	if body := bullet(implemented, "✓", th.success, w); body != "" {
		out = append(out, st.dim.Render("Implemented since draft 1:")+"\n"+body)
	}
	return strings.Join(out, "\n")
}

// reportFooter is the honest scope and the permalink. The disclaimer is fixed
// text: it is the product's promise about what this check does not do, and it
// travels with every rendering of a report. The link goes on its own line when
// it would not fit beside its label, because a truncated URL cannot be pasted.
func reportFooter(a *api.Analysis, w int) string {
	const disclaimer = "Text-only check: maps relative attention risk inside the draft; it does not predict published YouTube retention."
	parts := []string{rule(w), fit(st.dim.Render(wrapBlock(disclaimer, w)), w)}
	if link := ReportURL(appURL, a.ID); link != "" {
		const label = "Read it in the browser  "
		if lipgloss.Width(label)+lipgloss.Width(link) <= w {
			parts = append(parts, st.faint.Render(label)+st.key.Render(link))
		} else {
			parts = append(parts, st.faint.Render(strings.TrimSpace(label))+"\n"+st.key.Render(link))
		}
	}
	return strings.Join(parts, "\n")
}

// fieldKeyW is the width of the label column in a report's key/value rows; it
// fits the longest label ("Problem") with a space to spare.
const fieldKeyW = 7

// reportField renders "Key  value" with the value wrapped under its own column.
// An empty value renders nothing at all, so a sparse improvement leaves no
// empty labelled rows behind.
func reportField(key, value string, style lipgloss.Style, indent, w int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	col := indent + fieldKeyW + 1
	wrapped := style.Render(wrapBlock(value, max(w-col, 8)))
	return strings.Repeat(" ", indent) + st.faint.Render(padRight(key, fieldKeyW)) + " " +
		strings.ReplaceAll(wrapped, "\n", "\n"+strings.Repeat(" ", col))
}

// reportHighlight is reportField for the rewrite that replaces the quote. It
// gets a coloured bar so the two blocks are never confused at a glance, which
// is the one mistake a reader of a report can actually make.
func reportHighlight(key, value string, indent, w int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	col := indent + fieldKeyW + 1
	lines := wrap(value, max(w-col-2, 8))
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		prefix := strings.Repeat(" ", col)
		if i == 0 {
			prefix = strings.Repeat(" ", indent) + st.faint.Render(padRight(key, fieldKeyW)) + " "
		}
		out = append(out, prefix+st.accent.Render("▌")+" "+st.improved.Render(l))
	}
	return strings.Join(out, "\n")
}

// flagList renders matched policy passages, numbered so a reader can point at
// one. The quote, the reason and the suggested rewrite are only present when
// the response is unlocked, which is exactly what the locked marker says.
func flagList(p *api.PolicyPreflight, w int) []string {
	if len(p.Flags) == 0 {
		return nil
	}
	idx := policyCategoryIndex(p.Categories)
	var out []string
	for n, f := range p.Flags {
		if f.CategoryID == "" && f.Severity == "" && f.Quote == "" {
			// Nothing identifies this flag: there is no line to render, and an
			// empty numbered row would read like a rendering failure.
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		var head []string
		head = append(head, st.faint.Render(itoa(n+1)))
		if f.Severity != "" {
			head = append(head, badge(strings.ToUpper(strings.TrimSpace(f.Severity)), severityColor(f.Severity)))
		}
		label := f.CategoryID
		if c, ok := idx[f.CategoryID]; ok {
			label = c.Label
		}
		head = append(head, st.bold.Render(truncate(label, max(w/2, 10))))
		if f.InTitle {
			head = append(head, chip("in title", th.warning))
		}
		if f.Locked {
			head = append(head, lockedChip())
		}
		out = append(out, " "+strings.Join(head, " "))
		if f.Quote != "" {
			out = append(out, reportField("Now", quote(f.Quote), st.quote, 3, w))
		}
		if f.Why != "" {
			out = append(out, reportField("Why", f.Why, st.body, 3, w))
		}
		if f.SuggestedRewrite != "" {
			out = append(out, reportHighlight("Try", f.SuggestedRewrite, 3, w))
		}
	}
	return out
}

// policyCategoryIndex maps a flag's category id onto the rubric entry the
// response carries, so a rendering never keeps its own copy of the rubric and
// never has to guess at a label.
func policyCategoryIndex(cats []api.PolicyCategory) map[string]api.PolicyCategory {
	idx := make(map[string]api.PolicyCategory, len(cats))
	for _, c := range cats {
		idx[c.ID] = c
	}
	return idx
}

// stageList is the worker's pipeline. Two stage names are translated for
// display (pending is "queued", completed is "done") because the checklist is
// read as a queue of work, not as API status values.
//
// Transcription only appears while it is the current stage, because a snapshot
// cannot tell a pasted script (which never transcribes) from a file whose
// transcription has already finished. Listing a step nobody watched happen would
// be worse than leaving it out.
func stageList(status string, progress int, w int) string {
	stages := []string{api.StatusPending}
	if status == api.StatusTranscribing {
		stages = append(stages, api.StatusTranscribing)
	}
	stages = append(stages, api.StatusAnalyzing, api.StatusComputingSaliency, api.StatusCompleted)

	current := 0
	for i, s := range stages {
		if s == status {
			current = i
			break
		}
	}
	var out []string
	for i, s := range stages {
		label := stageLabel(s)
		switch {
		case i < current:
			out = append(out, "  "+st.good.Render("✓")+" "+st.faint.Render(label))
		case i == current:
			line := "  " + st.accent.Render("▸") + " " + st.bold.Render(label)
			if s == api.StatusAnalyzing || s == api.StatusComputingSaliency {
				line += "  " + bar(progress, clampInt(w-26, 6, 30)) + " " + st.dim.Render(itoa(progress)+"%")
			}
			out = append(out, line)
		default:
			out = append(out, "  "+st.faint.Render("· "+label))
		}
	}
	return strings.Join(out, "\n")
}

func stageLabel(status string) string {
	switch status {
	case api.StatusPending:
		return "queued"
	case api.StatusComputingSaliency:
		return "computing saliency"
	case api.StatusCompleted:
		return "done"
	default:
		return strings.ReplaceAll(status, "_", " ")
	}
}

func statusLabel(status string) string {
	if status == "" {
		return "unknown"
	}
	return strings.ReplaceAll(status, "_", " ")
}

func statusColor(status string) color.Color {
	switch status {
	case api.StatusCompleted:
		return th.success
	case api.StatusFailed:
		return th.danger
	default:
		return th.warning
	}
}

func verdictLabel(v string) string {
	switch v {
	case "no_matches":
		return "no matches"
	case "review_suggested":
		return "review suggested"
	case "high_matches":
		return "high matches"
	case "":
		return "not checked"
	default:
		return strings.ReplaceAll(v, "_", " ")
	}
}

func quote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return "“" + s + "”"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func percentList(values []int) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, itoa(v)+"%")
	}
	return strings.Join(parts, ", ")
}

// fit is the last step of every renderer, and it does two things.
//
// It trims the padding lipgloss adds when it renders a multi-line string: a
// styled block is padded to the width of its longest line, which would otherwise
// leave a trail of invisible spaces after every short line of a section. The
// trim stops at the last escape sequence, because the padding that follows one
// is part of the thing being styled — a pill's own right-hand space — and only
// the spaces after the closing reset are lipgloss's.
//
// It then truncates any line still wider than w. No layout above should produce
// one, but a degenerate width or a run of unbreakable text should not be able to
// break the package's promise either; ansi.Truncate counts cells, so this is
// safe on a styled line.
func fit(s string, w int) string {
	parts := strings.Split(s, "\n")
	changed := false
	for i, line := range parts {
		if trimmed := trimPadding(line); trimmed != line {
			parts[i], line, changed = trimmed, trimmed, true
		}
		if w >= 1 && lipgloss.Width(line) > w {
			parts[i], changed = ansi.Truncate(line, w, "…"), true
		}
	}
	if !changed {
		return s
	}
	return strings.Join(parts, "\n")
}

// trimPadding removes the trailing spaces lipgloss pads a rendered block with,
// leaving the spaces that belong to a style's own padding alone.
func trimPadding(line string) string {
	body := strings.TrimRight(line, " \t")
	if body == line {
		return line
	}
	switch {
	case body == "":
		return ""
	case !strings.Contains(body, "\x1b"):
		return body
	case strings.HasSuffix(body, "\x1b[m"), strings.HasSuffix(body, "\x1b[0m"):
		return body
	default:
		// The trailing spaces are inside a styled run.
		return line
	}
}
