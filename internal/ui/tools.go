package ui

import (
	"image/color"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// The three free tools: the hook analyzer, the inauthentic-content check and
// the policy pre-flight. They share the report's vocabulary — gauges, badges,
// quoted passages, rewrites — so a creator who runs the free check and then
// buys an audit is not asked to learn a second layout.

// RenderHook renders one hook analysis. The order is the order the reader
// argues with it in: the grade, then the verdict in one sentence, then the
// per-sentence evidence, then the problems, then the rewrites.
func RenderHook(r *api.HookResult, width int) string {
	if r == nil {
		return ""
	}
	w := contentWidth(width)

	var head []string
	head = append(head, badge(strings.ToUpper(strings.TrimSpace(r.Grade))+" grade", gradeColor(r.Grade))+"  "+
		st.dim.Render("scored on attention pull and curiosity gap"))
	if hook := strings.TrimSpace(r.HookText); hook != "" {
		head = append(head, "", st.quote.Render(wrapBlock(quote(hook), boxInner(w))))
	}
	if v := strings.TrimSpace(r.OneSentenceVerdict); v != "" {
		head = append(head, "", st.body.Render(wrapBlock(v, boxInner(w))))
	}
	header := box("Hook analysis", head, w)

	parts := []string{header}
	if meta := hookMeta(r, w); meta != "" {
		parts = append(parts, meta)
	}
	parts = append(parts,
		gauge("Overall", r.OverallScore, w, w >= narrowAt),
		sentencesBlock(r.Sentences, w),
		issuesBlock(r.TopIssues, w),
		rewritesBlock(r.Rewrites, w),
		hookFooter(r, w),
	)
	return fit(stack("\n\n", parts...), w)
}

func gradeColor(grade string) color.Color {
	switch strings.ToUpper(strings.TrimSpace(grade)) {
	case "A", "A+", "A-":
		return th.success
	case "B", "B+", "B-":
		return th.info
	case "C", "C+", "C-":
		return th.warning
	case "":
		return th.faint
	default:
		return th.danger
	}
}

func hookMeta(r *api.HookResult, w int) string {
	var parts []string
	if r.Niche != "" {
		parts = append(parts, "niche "+r.Niche)
	}
	if n := len(r.Sentences); n > 0 {
		parts = append(parts, plural(n, "sentence", "sentences"))
	}
	if !r.CreatedAt.IsZero() {
		parts = append(parts, r.CreatedAt.Local().Format("2 Jan 2006 15:04"))
	}
	if r.EvaluationID != "" {
		parts = append(parts, r.EvaluationID)
	}
	if len(parts) == 0 {
		return ""
	}
	return st.faint.Render(truncate(strings.Join(parts, " · "), w))
}

// sentencesBlock is the evidence: what each sentence asks of the viewer. Both
// numbers are 0-100 pulls, so they read on the same bar as everything else.
func sentencesBlock(sentences []api.HookSentence, w int) string {
	if len(sentences) == 0 {
		return ""
	}
	var out []string
	for i, s := range sentences {
		head := " " + st.faint.Render(itoa(i+1)) + " "
		out = append(out, hangStyled(head, s.Quote, w))
		pull := metric("Attention pull", s.AttentionPull, 14, 8)
		gap := metric("Curiosity gap", s.CuriosityGap, 13, 8)
		if w >= 58 {
			out = append(out, "   "+pull+"   "+gap)
		} else {
			out = append(out, "   "+pull, "   "+gap)
		}
		tail := []string{}
		if s.PayoffDistance != "" {
			tail = append(tail, "payoff "+s.PayoffDistance)
		}
		if s.Note != "" {
			tail = append(tail, s.Note)
		}
		if len(tail) > 0 {
			out = append(out, st.dim.Render(hang(wrapBlock(strings.Join(tail, " · "), max(w-3, 8)), 3, 3, w)))
		}
	}
	return strings.Join(out, "\n")
}

// metric is a compact label/bar/value row for the tool views, where two of them
// share a line.
func metric(label string, value int, labelW, barW int) string {
	return st.dim.Render(padRight(label, labelW)) + " " + bar(value, barW) + " " +
		st.bold.Render(padLeft(strconv.Itoa(value), 3))
}

// issuesBlock is the headline problem per sentence, which is the shortest path
// from "this scored badly" to "this is why".
func issuesBlock(issues []api.HookIssue, w int) string {
	if len(issues) == 0 {
		return ""
	}
	var out []string
	for n, iss := range issues {
		head := " " + st.faint.Render(itoa(n+1)) + " " + st.bold.Render(truncate(iss.Headline, max(w-6, 8)))
		block := []string{head}
		if iss.Quote != "" {
			block = append(block, st.quote.Render(hang(wrapBlock(quote(iss.Quote), max(w-3, 8)), 3, 3, w)))
		}
		if iss.Why != "" {
			block = append(block, st.dim.Render(hang(wrapBlock(iss.Why, max(w-3, 8)), 3, 3, w)))
		}
		out = append(out, strings.Join(block, "\n"))
	}
	return sectionHead(0, "What is holding it back", plural(len(issues), "issue", "issues"), w) + "\n" + strings.Join(out, "\n\n")
}

// rewritesBlock offers replacements. Each one is shown with the bar treatment
// the report uses for a rewrite, so "Try" means the same thing in both.
func rewritesBlock(rewrites []api.HookRewrite, w int) string {
	if len(rewrites) == 0 {
		return ""
	}
	var out []string
	for _, rw := range rewrites {
		style := strings.TrimSpace(rw.Style)
		if style == "" {
			style = "rewrite"
		}
		out = append(out, tag(style, th.accent))
		if rw.Hook != "" {
			out = append(out, reportHighlight("Try", rw.Hook, 2, w))
		}
		if rw.WhyWorks != "" {
			out = append(out, reportField("Why", rw.WhyWorks, st.body, 2, w))
		}
	}
	return sectionHead(0, "Rewrites", plural(len(rewrites), "version", "versions"), w) + "\n" + strings.Join(out, "\n\n")
}

// hookFooter carries the two things a hook result is obliged to say: which
// model produced it, and how far it can be trusted.
func hookFooter(r *api.HookResult, w int) string {
	parts := []string{}
	if r.DegradedFromPro {
		parts = append(parts, st.body.Render(wrapBlock(
			"Free model result. The Pro model reads the same hook more deeply, and needs an email address.",
			w)))
	}
	meta := []string{}
	if r.ModelUsed != "" {
		meta = append(meta, "model "+r.ModelUsed)
	}
	if r.PromptVersion > 0 {
		meta = append(meta, "prompt v"+itoa(r.PromptVersion))
	}
	if len(meta) > 0 {
		parts = append(parts, st.faint.Render(strings.Join(meta, " · ")))
	}
	parts = append(parts, rule(w), st.dim.Render(wrapBlock(
		"Attention pull and curiosity gap are relative scores for the words in the hook. They do not predict published retention.",
		w)))
	return strings.Join(parts, "\n")
}

// RenderAuthenticity renders the standalone inauthentic-content check.
func RenderAuthenticity(r *api.AuthenticityResult, width int) string {
	if r == nil {
		return ""
	}
	w := contentWidth(width)

	var head []string
	head = append(head, badge(strings.ToLower(strings.TrimSpace(r.RiskLevel))+" risk", riskColor(r.RiskLevel))+"  "+
		st.dim.Render("inauthentic or reused content"))
	if v := strings.TrimSpace(r.Verdict); v != "" {
		head = append(head, "", st.body.Render(wrapBlock(v, boxInner(w))))
	}
	parts := []string{
		box("Authenticity check", head, w),
		gauge("Authenticity", r.Score, w, false),
		st.faint.Render(wrapBlock("100 is fully authentic. The score is about reused or formulaic material, not about originality of topic.", w)),
		signalsBlock(r.Signals, w),
		remediationBlock(r.Remediation, w),
	}
	if note := strings.TrimSpace(r.OverrideNote); note != "" {
		parts = append(parts, st.dim.Render(wrapBlock(note, w)))
	}
	parts = append(parts, authenticityFooter(r, w))
	return fit(stack("\n\n", parts...), w)
}

// signalsBlock is the per-signal breakdown: which check fired, how serious it
// is, and the evidence it found.
func signalsBlock(signals []api.AuthenticitySignal, w int) string {
	if len(signals) == 0 {
		return ""
	}
	var out []string
	for _, s := range signals {
		var head []string
		if s.Severity != "" {
			head = append(head, badge(strings.ToUpper(strings.TrimSpace(s.Severity)), severityColor(s.Severity)))
		}
		head = append(head, st.bold.Render(truncate(s.Name, max(w/2, 10))))
		out = append(out, " "+strings.Join(head, " "))
		if s.Reason != "" {
			out = append(out, st.dim.Render(hang(wrapBlock(s.Reason, max(w-3, 8)), 3, 3, w)))
		}
		if s.Quote != "" {
			out = append(out, st.quote.Render(hang(wrapBlock(quote(s.Quote), max(w-3, 8)), 3, 3, w)))
		}
	}
	return sectionHead(0, "Signals", plural(len(signals), "signal", "signals"), w) + "\n" + strings.Join(out, "\n\n")
}

// remediationBlock is the fix list, in the same Now/Try shape the report uses.
func remediationBlock(items []api.AuthenticityRemediation, w int) string {
	if len(items) == 0 {
		return ""
	}
	var out []string
	for _, it := range items {
		out = append(out, tag(strings.TrimSpace(it.Title), th.fg))
		out = append(out,
			reportField("Now", it.Current, st.quote, 2, w),
			reportHighlight("Try", it.Fix, 2, w),
			reportField("Why", it.Why, st.body, 2, w),
		)
	}
	return sectionHead(0, "What to change", plural(len(items), "fix", "fixes"), w) + "\n" + strings.Join(out, "\n\n")
}

func authenticityFooter(r *api.AuthenticityResult, w int) string {
	var meta []string
	if !r.CreatedAt.IsZero() {
		meta = append(meta, r.CreatedAt.Local().Format("2 Jan 2006 15:04"))
	}
	if r.AnalysisID != "" {
		meta = append(meta, r.AnalysisID)
	}
	parts := []string{rule(w)}
	if len(meta) > 0 {
		parts = append(parts, st.faint.Render(truncate(strings.Join(meta, " · "), w)))
	}
	parts = append(parts, st.dim.Render(wrapBlock(
		"This checks the draft against the Partner Program's inauthentic-content rules. It is a pre-flight read, not a decision by YouTube.", w)))
	return strings.Join(parts, "\n")
}

// RenderPolicy renders a policy pre-flight. The scope statement the server
// sends travels with the result and is printed with it: this checker says what
// matched a published guideline, never what YouTube will decide.
func RenderPolicy(r *api.PolicyResult, width int) string {
	if r == nil {
		return ""
	}
	w := contentWidth(width)
	p := &r.Preflight

	var head []string
	if title := strings.TrimSpace(r.Title); title != "" {
		head = append(head, st.h1.Render(wrapBlock(title, boxInner(w))), "")
	}
	line := badge(verdictLabel(p.Verdict), severityColor(p.Verdict))
	if p.TotalFlags > 0 {
		line += "  " + st.dim.Render(plural(p.TotalFlags, "matched passage", "matched passages"))
	}
	if r.Cached {
		line += "  " + chip("cached", th.faint)
	}
	head = append(head, line)
	if scope := strings.TrimSpace(p.ScopeStatement); scope != "" {
		head = append(head, "", st.dim.Render(wrapBlock(scope, boxInner(w))))
	}
	parts := []string{box("Policy pre-flight", head, w)}

	if p.ScriptTruncated {
		parts = append(parts, st.dim.Render(wrapBlock(
			"Only the first part of the script was checked: it was longer than the checker's window.", w)))
	}
	if flags := flagList(p, w); len(flags) > 0 {
		parts = append(parts, sectionHead(0, "Matched passages", categoryCountMeta(p), w)+"\n"+strings.Join(flags, "\n"))
		if p.Locked {
			parts = append(parts, st.dim.Render(wrapBlock(
				"The matched passages are reduced on this plan. Creator adds the quote, the reason and a suggested rewrite for each one.", w)))
		}
	} else if p.Verdict != "" {
		parts = append(parts, st.good.Render(wrapBlock("No passage matched a category in the rubric.", w)))
	}
	parts = append(parts, matchedCategories(p, w), policyFooter(r, w))
	return fit(stack("\n\n", parts...), w)
}

func categoryCountMeta(p *api.PolicyPreflight) string {
	if len(p.CountsByCategory) == 0 {
		return ""
	}
	var parts []string
	for id, n := range p.CountsByCategory {
		if n == 0 {
			continue
		}
		label := id
		for _, c := range p.Categories {
			if c.ID == id {
				label = c.Label
				break
			}
		}
		parts = append(parts, label+" ×"+itoa(n))
	}
	if len(parts) == 0 {
		return ""
	}
	sortStrings(parts)
	return strings.Join(parts, " · ")
}

// matchedCategories prints the rubric entry for every category that matched,
// because the consequence of a match depends on which family it belongs to:
// advertiser-friendly guidelines and community guidelines are not the same
// thing at all.
func matchedCategories(p *api.PolicyPreflight, w int) string {
	ids := map[string]bool{}
	for _, f := range p.Flags {
		ids[f.CategoryID] = true
	}
	for id, n := range p.CountsByCategory {
		if n > 0 {
			ids[id] = true
		}
	}
	if len(ids) == 0 {
		return ""
	}
	var out []string
	for _, c := range p.Categories {
		if !ids[c.ID] {
			continue
		}
		out = append(out, categoryBlock(c, w))
	}
	if len(out) == 0 {
		return ""
	}
	return sectionHead(0, "What matched means here", "", w) + "\n" + strings.Join(out, "\n\n")
}

func categoryBlock(c api.PolicyCategory, w int) string {
	name := strings.TrimSpace(c.Label)
	if name == "" {
		name = c.ID
	}
	head := tag(name, th.fg)
	if c.FamilyLabel != "" {
		head += "  " + chip(c.FamilyLabel, th.faint)
	} else if c.Family != "" {
		head += "  " + chip(strings.ReplaceAll(c.Family, "_", " "), th.faint)
	}
	out := []string{head}
	if c.OfficialName != "" {
		out = append(out, st.dim.Render(hang(wrapBlock(c.OfficialName, max(w-2, 8)), 2, 2, w)))
	}
	if c.Summary != "" {
		out = append(out, st.body.Render(hang(wrapBlock(c.Summary, max(w-2, 8)), 2, 2, w)))
	}
	if c.FamilyConsequence != "" {
		out = append(out, st.dim.Render(hang(wrapBlock("Consequence: "+c.FamilyConsequence, max(w-2, 8)), 2, 2, w)))
	}
	if c.DetectabilityNote != "" {
		out = append(out, st.faint.Render(hang(wrapBlock(c.DetectabilityNote, max(w-2, 8)), 2, 2, w)))
	}
	if c.SourceURL != "" {
		label := strings.TrimSpace(c.SourceLabel)
		if label == "" {
			label = "Source"
		}
		if lipgloss.Width(label)+2+lipgloss.Width(c.SourceURL) <= w-2 {
			out = append(out, "  "+st.faint.Render(label+"  ")+st.key.Render(c.SourceURL))
		} else {
			out = append(out, "  "+st.faint.Render(label), "  "+st.key.Render(c.SourceURL))
		}
	}
	return strings.Join(out, "\n")
}

func policyFooter(r *api.PolicyResult, w int) string {
	p := &r.Preflight
	var meta []string
	if p.RubricVersion > 0 {
		meta = append(meta, "rubric v"+itoa(p.RubricVersion))
	}
	if !p.RubricPublishedAt.IsZero() {
		meta = append(meta, "guidelines as published "+p.RubricPublishedAt.Local().Format("2 Jan 2006"))
	}
	if p.ModelUsed != "" {
		meta = append(meta, "model "+p.ModelUsed)
	}
	if !p.GeneratedAt.IsZero() {
		meta = append(meta, p.GeneratedAt.Local().Format("2 Jan 2006 15:04"))
	}
	parts := []string{rule(w)}
	if len(meta) > 0 {
		parts = append(parts, st.faint.Render(truncate(strings.Join(meta, " · "), w)))
	}
	if scope := strings.TrimSpace(p.ScopeStatement); scope != "" {
		parts = append(parts, st.dim.Render(wrapBlock(scope, w)))
	}
	return strings.Join(parts, "\n")
}

// sortStrings is the tiny insertion sort used for the two or three labels in a
// category count line. Its size is bounded by the rubric, and importing
// "sort" for that would be the larger dependency.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
