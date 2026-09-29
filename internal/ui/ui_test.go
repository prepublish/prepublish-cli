package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
	"github.com/prepublish/prepublish-cli/internal/scriptinfo"
)

// These tests hold the package's two promises: no renderer emits a line wider
// than the width it was given, and a gated report does not leak the content the
// server stripped. Everything else here is a fixture for those two.

// widths the renderers are exercised at: a 100-column window, the 80 columns a
// default terminal gives, and 60, which is where the layouts have to start
// dropping side-by-side detail.
var testWidths = []int{100, 80, 60}

// assertFits fails when any rendered line is wider than w. It measures both the
// styled line and its stripped form, because a layout bug that only shows up in
// display cells (a wide rune counted as one column) would be invisible to a byte
// count.
func assertFits(t *testing.T, label string, out string, w int) {
	t.Helper()
	for i, line := range strings.Split(out, "\n") {
		if got := lipgloss.Width(line); got > w {
			t.Errorf("%s: line %d is %d columns wide, want <= %d\n%s", label, i+1, got, w, line)
		}
		if got := ansi.StringWidth(ansi.Strip(line)); got > w {
			t.Errorf("%s: stripped line %d is %d columns wide, want <= %d\n%s", label, i+1, got, w, ansi.Strip(line))
		}
	}
}

func TestRenderReportFitsWidth(t *testing.T) {
	for _, w := range testWidths {
		assertFits(t, "paid report", RenderReport(paidFixture(), w), w)
		assertFits(t, "locked report", RenderReport(lockedFixture(), w), w)
		assertFits(t, "pending report", RenderReport(pendingFixture(), w), w)
		assertFits(t, "failed report", RenderReport(failedFixture(), w), w)
	}
}

// TestRenderersEmitNoTrailingWhitespace guards a defect that is invisible in a
// terminal but visible in every copy-paste: lipgloss pads a multi-line styled
// block to its longest line, which leaves trailing spaces after short lines
// unless the renderer trims them.
func TestRenderersEmitNoTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
	}{
		{"paid", RenderReport(paidFixture(), 100)},
		{"locked", RenderReport(lockedFixture(), 80)},
		{"pending", RenderReport(pendingFixture(), 100)},
		{"failed", RenderReport(failedFixture(), 100)},
		{"hook", RenderHook(hookFixture(), 100)},
		{"authenticity", RenderAuthenticity(authenticityFixture(), 100)},
		{"policy", RenderPolicy(policyFixture(false), 100)},
		{"account", RenderAccount(user("a@b.example", "active"), &api.Usage{Tier: api.TierPaid, AuditsUsedToday: 1, AuditsLimit: 50, AuditsRemaining: 49}, nil, config.SourceFile, 100)},
		{"runtime", RenderRuntime(500, scriptinfo.Runtime(500), 100)},
	} {
		for i, line := range strings.Split(tc.out, "\n") {
			plain := ansi.Strip(line)
			if trimmed := strings.TrimRight(plain, " \t"); trimmed != plain {
				t.Errorf("%s: line %d has trailing whitespace: %q", tc.name, i+1, plain)
			}
		}
	}
}

func TestToolRenderersFitWidth(t *testing.T) {
	for _, w := range testWidths {
		assertFits(t, "hook", RenderHook(hookFixture(), w), w)
		assertFits(t, "authenticity", RenderAuthenticity(authenticityFixture(), w), w)
		assertFits(t, "policy", RenderPolicy(policyFixture(false), w), w)
		assertFits(t, "policy locked", RenderPolicy(policyFixture(true), w), w)
	}
}

func TestAccountAndRuntimeFitWidth(t *testing.T) {
	for _, w := range testWidths {
		assertFits(t, "anonymous", RenderAccount(nil, nil, &api.CheckFree{Remaining: 2, Limit: 3, Tier: api.TierAnonymous, MaxFileSizeMB: 5000}, config.SourceNone, w), w)
		assertFits(t, "free", RenderAccount(user("free@example.com", "inactive"), &api.Usage{Tier: api.TierFreeAccount, AuditsUsedToday: 1, AuditsLimit: 3, AuditsRemaining: 2}, nil, config.SourceEnv, w), w)
		assertFits(t, "studio", RenderAccount(user("studio@agency.example", "past_due"), &api.Usage{Tier: api.TierStudio, AuditsUsedToday: 26, AuditsLimit: 50, AuditsRemaining: 24}, nil, config.SourceFile, w), w)
		assertFits(t, "runtime", RenderRuntime(1842, scriptinfo.Runtime(1842), w), w)
		assertFits(t, "runtime empty", RenderRuntime(0, scriptinfo.Estimate{}, w), w)
	}
}

func TestRenderErrorFitsWidth(t *testing.T) {
	for _, err := range errorFixtures() {
		assertFits(t, "error", RenderError(err), errorWidth)
		assertFits(t, "error narrow", RenderErrorWidth(err, 60), 60)
	}
}

// TestLockedReportWithholdsRewrites is the gating contract: the content the
// server stripped must not appear anywhere in the rendering, and the marker that
// explains why must.
func TestLockedReportWithholdsRewrites(t *testing.T) {
	a := lockedFixture()
	out := ansi.Strip(RenderReport(a, 100))

	if !strings.Contains(out, "locked") {
		t.Error("a locked report must say which parts are locked")
	}
	if !strings.Contains(out, "prepublish login") {
		t.Error("a signup_required report must offer the free account path")
	}
	// improvements[0] is complete, so its rewrite must be visible.
	if !strings.Contains(out, a.Improvements[0].Improved) {
		t.Errorf("the unlocked improvement's rewrite is missing from the report:\n%s", a.Improvements[0].Improved)
	}
	// Every locked improvement's rewrite, quote and reason are server-side
	// stripped content, and none of them may be echoed back.
	for i, imp := range a.Improvements[1:] {
		if strings.Contains(out, imp.Improved) {
			t.Errorf("locked improvement %d leaked its rewrite: %q", i+1, imp.Improved)
		}
		if strings.Contains(out, imp.ProblemSummary) {
			t.Errorf("locked improvement %d leaked its problem: %q", i+1, imp.ProblemSummary)
		}
	}
	if strings.Contains(out, "2 Worked, 3 Were a Waste of Time") {
		t.Error("a locked report leaked the title rewrite")
	}
	if strings.Contains(out, "I slept better in week two") {
		t.Error("a locked report leaked a policy passage rewrite")
	}
	// The locked fixes are still described, at their own level of detail.
	for _, imp := range a.Improvements[1:] {
		if !strings.Contains(out, imp.Title) {
			t.Errorf("locked improvement title %q should still be listed", imp.Title)
		}
	}
}

// TestUpgradeHintFollowsLockReason pins the difference between the two locked
// states: a free account has a plan to buy, an anonymous caller an account to
// create. Getting this backwards sends a paying customer to a signup page.
func TestUpgradeHintFollowsLockReason(t *testing.T) {
	up := lockedFixture()
	up.LockReason = api.LockReasonUpgradeRequired
	out := ansi.Strip(RenderReport(up, 100))
	if !strings.Contains(out, "prepublish upgrade") {
		t.Error("an upgrade_required report must offer prepublish upgrade")
	}
	if strings.Contains(out, "prepublish login") {
		t.Error("an upgrade_required report must not offer the free account path")
	}

	signup := lockedFixture()
	out = ansi.Strip(RenderReport(signup, 100))
	if !strings.Contains(out, "prepublish login") {
		t.Error("a signup_required report must offer prepublish login")
	}
}

// TestRenderErrorActions covers the mapping a user actually acts on. The codes
// are the server's; the commands are ours, and every one of them has to be the
// right next step.
func TestRenderErrorActions(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"unauthorized", &api.APIError{Status: 401, Code: api.CodeUnauthorized, Message: "no"}, "prepublish login"},
		{"forbidden-invalid-token", &api.APIError{Status: 401, Code: "INVALID_TOKEN", Message: "bad key"}, "prepublish login"},
		{"quota", &api.APIError{Status: 429, Code: api.CodeFreeAnalysisUsed, Message: "Daily analysis limit reached."}, "prepublish upgrade"},
		{"no subscription", &api.APIError{Status: 403, Code: api.CodeNoSubscription, Message: "uploads are paid"}, "prepublish upgrade"},
		{"subscription required", &api.APIError{Status: 402, Code: api.CodeSubscriptionRequired}, "prepublish upgrade"},
		{"email field", &api.APIError{Status: 400, Code: api.CodeValidation, Field: "email", Message: "valid email required"}, "--email"},
		{"hook email gate", &api.APIError{Status: 402, Code: api.CodeEmailRequired, Message: "drop your email"}, "--email"},
		{"not found", &api.APIError{Status: 404, Code: api.CodeNotFound}, "prepublish history"},
		{"rate limited", &api.APIError{Status: 429, Code: api.CodeRateLimited, RetryAfter: 42 * time.Second}, "42s"},
		{"expired login", &api.APIError{Status: 410, Code: api.CodeExpiredToken}, "prepublish login"},
		{"wrapped api error", fmt.Errorf("auditing: %w", &api.APIError{Status: 401, Code: api.CodeUnauthorized}), "prepublish login"},
		{"transport", &url.Error{Op: "Post", URL: "https://api.prepublish.ai/api/analyze", Err: errors.New("dial tcp: no such host")}, "--api-url"},
		{"cancelled", context.Canceled, "Canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ansi.Strip(RenderError(tc.err))
			if out == "" {
				t.Fatal("a non-nil error must render something")
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("want the hint %q in:\n%s", tc.want, out)
			}
		})
	}

	if got := RenderError(nil); got != "" {
		t.Errorf("a nil error must render nothing, got %q", got)
	}
	// Any error at all still renders: a CLI that prints an empty error block is
	// worse than one that prints the message verbatim.
	plain := errors.New("configuration file is not valid JSON")
	if out := ansi.Strip(RenderError(plain)); !strings.Contains(out, "configuration file is not valid JSON") {
		t.Errorf("an unclassified error must show its message, got:\n%s", out)
	}
}

// TestErrorSurvivesUnprintableCodes guards the classification against the shapes
// a server is free to send: no code, no message, nothing but a status.
func TestErrorSurvivesUnprintableCodes(t *testing.T) {
	for _, err := range []error{
		errors.New(""),
		&api.APIError{},
		&api.APIError{Status: 500},
		&api.APIError{Status: 0, Code: api.CodeNetworkError, Cause: nil},
		&api.APIError{Status: 400, Code: api.CodeValidation, Field: "video_title"},
	} {
		out := RenderError(err)
		if strings.TrimSpace(ansi.Strip(out)) == "" {
			t.Errorf("%#v rendered nothing", err)
		}
	}
	var nilAPI *api.APIError
	var err error = nilAPI
	if out := RenderError(err); strings.TrimSpace(ansi.Strip(out)) == "" {
		t.Error("a nil *api.APIError must still render a generic failure")
	}
}

// TestCurveHandlesDegenerateInput: an empty curve says so, a single point draws
// without pretending to be a trend, and neither panics or overflows.
// hasLine reports whether the rendered block contains a line that says exactly
// this, which is how a test asks "did a heading survive" without depending on
// the spacing a heading happens to use.
func hasLine(block, want string) bool {
	for _, line := range strings.Split(ansi.Strip(block), "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func TestCurveHandlesDegenerateInput(t *testing.T) {
	for _, w := range testWidths {
		empty := RenderReport(&api.Analysis{Status: api.StatusCompleted, ID: "x", VideoTitle: "t"}, w)
		assertFits(t, "empty curve", empty, w)
		if !strings.Contains(ansi.Strip(empty), "unavailable") {
			t.Error("an empty curve should say the risk map is unavailable")
		}

		single := &api.Analysis{
			Status:         api.StatusCompleted,
			ID:             "y",
			VideoTitle:     "one point",
			RetentionCurve: []api.RetentionPoint{{Timestamp: 50, Retention: 63, Event: "midpoint"}},
		}
		out := RenderReport(single, w)
		assertFits(t, "single point curve", out, w)
		if !strings.Contains(ansi.Strip(out), "midpoint") {
			t.Error("a single point's event should still be listed")
		}
	}

	// The chart's own function, exercised directly so a panic here is a
	// failure that names the chart rather than the report around it.
	for _, pts := range [][]api.RetentionPoint{
		nil,
		{{Timestamp: 0, Retention: 100}},
		{{Timestamp: 0, Retention: 0}, {Timestamp: 100, Retention: 100}},
		{{Timestamp: 100, Retention: 90}, {Timestamp: 0, Retention: 10}}, // unsorted
		{{Timestamp: 50, Retention: 150}, {Timestamp: 200, Retention: -40}},
	} {
		out := curve(pts, new(612), "interpolated", 72)
		assertFits(t, "curve", out, 72)
		if strings.TrimSpace(ansi.Strip(out)) == "" {
			t.Errorf("curve(%v) rendered nothing", pts)
		}
	}
}

// TestChartPlotsTheData covers the one structural claim the chart makes: the
// y axis runs 100 at the top to 0 at the bottom. A flipped chart would still
// look like a chart, so this is the test that would catch it.
func TestChartPlotsTheData(t *testing.T) {
	plotRows := func(retention float64, event string) []string {
		out := ansi.Strip(curve([]api.RetentionPoint{
			{Timestamp: 0, Retention: retention},
			{Timestamp: 50, Retention: retention, Event: event},
			{Timestamp: 100, Retention: retention},
		}, nil, "", 80))
		var rows []string
		for _, line := range strings.Split(out, "\n") {
			// The plot rows are the ones with a y-axis label; everything else is
			// the marker ruler, the axis or the legend.
			if strings.Contains(line, "┤") {
				rows = append(rows, line)
			}
		}
		return rows
	}
	hasCurve := func(line string) bool {
		return strings.IndexFunc(line, func(r rune) bool { return r >= 0x2800 && r <= 0x28FF }) >= 0
	}

	high := plotRows(95, "")
	low := plotRows(5, "")
	if len(high) < 3 || len(high) != len(low) {
		t.Fatalf("expected at least three plot rows, got %d and %d", len(high), len(low))
	}
	if !hasCurve(high[0]) || hasCurve(high[len(high)-1]) {
		t.Errorf("a 95 point should be plotted at the top:\n%s", strings.Join(high, "\n"))
	}
	if hasCurve(low[0]) || !hasCurve(low[len(low)-1]) {
		t.Errorf("a 5 point should be plotted at the bottom:\n%s", strings.Join(low, "\n"))
	}

	// Every row of the plot has to end at the same column. Rows that stop at
	// their last drawn cell leave a ragged right edge, which reads as a
	// rendering bug in a terminal even though no line is too wide.
	for _, w := range testWidths {
		rows := plotRows(70, "an event")
		widths := map[int]bool{}
		for _, r := range rows {
			widths[lipgloss.Width(r)] = true
		}
		if len(widths) != 1 {
			t.Errorf("chart rows at %d columns end at different widths %v:\n%s", w, widths, strings.Join(rows, "\n"))
		}
	}

	// The x axis is a fixed 0%..100%, and an event lands in both the plot (as a
	// hairline) and the legend (numbered).
	out := ansi.Strip(curve([]api.RetentionPoint{
		{Timestamp: 0, Retention: 90},
		{Timestamp: 60, Retention: 20, Event: "the stall"},
		{Timestamp: 100, Retention: 70},
	}, nil, "", 80))
	if !strings.Contains(out, "the stall") {
		t.Error("an event on the curve must appear in the key-moment legend")
	}
	if !strings.Contains(out, "0%") || !strings.Contains(out, "100%") {
		t.Error("the x axis must be labelled 0%..100%")
	}
	if !strings.Contains(out, "│") {
		t.Error("an event must be marked in the plot as well as the legend")
	}
	if !strings.Contains(ansi.Strip(curve([]api.RetentionPoint{{Timestamp: 0, Retention: 50}}, nil, "interpolated", 80)), "interpolated") {
		t.Error("an interpolated curve must say so")
	}
}

// TestKeyFixCardWrapsItsTitle: the card is about the top fix, so its title is
// wrapped inside the card rather than cut off with an ellipsis.
func TestKeyFixCardWrapsItsTitle(t *testing.T) {
	a := paidFixture()
	const long = "Drop the movie summary from the opening and lead with the concrete result in the first sentence"
	a.Improvements[0].Title = long
	a.OneKeyImprovement = "Open on the result."

	for _, w := range []int{100, 60} {
		card := ansi.Strip(keyFix(a, w))
		flat := strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╰", " ").Replace(card)), " ")
		if !strings.Contains(flat, long) {
			t.Errorf("the key-fix card at %d columns lost part of the title:\n%s", w, card)
		}
		if strings.Contains(card, "…") {
			t.Errorf("the key-fix card at %d columns truncated the title:\n%s", w, card)
		}
		assertFits(t, "key fix", card, w)
	}
}

// TestEmptyComponentsAreOmitted: a gated or thin payload must not leave bare
// headings standing over nothing.
func TestEmptyComponentsAreOmitted(t *testing.T) {
	a := paidFixture()
	a.HookAnalysis = &api.HookAnalysis{Score: 64}
	a.StructureAnalysis = &api.StructureAnalysis{Score: 85}
	a.PacingAnalysis = &api.PacingAnalysis{Score: 71}
	out := ansi.Strip(RenderReport(a, 100))
	if strings.Contains(out, "Hook, structure and pacing") {
		t.Errorf("a report with no component prose must omit the section:\n%s", out)
	}
	for _, name := range []string{"Hook", "Structure", "Pacing"} {
		if hasLine(out, name) {
			t.Errorf("an empty component must not leave a %q heading behind:\n%s", name, out)
		}
	}

	// One component with prose keeps the section, and only its own heading.
	a.HookAnalysis.Strengths = []string{"names the payoff before the credentials"}
	out = ansi.Strip(RenderReport(a, 100))
	if !strings.Contains(out, "Hook, structure and pacing") || !hasLine(out, "Hook") {
		t.Errorf("a component with prose must keep its heading:\n%s", out)
	}
	for _, name := range []string{"Structure", "Pacing"} {
		if hasLine(out, name) {
			t.Errorf("empty components must stay out, found %q:\n%s", name, out)
		}
	}

	// A structure list with no strengths keeps the section as a list.
	a.HookAnalysis.Strengths = nil
	a.StructureAnalysis.Sections = []api.ContentSection{{Title: "Cold open", StartPercentage: 0, EndPercentage: 8, Quality: "strong"}}
	out = ansi.Strip(RenderReport(a, 100))
	if !strings.Contains(out, "Hook, structure and pacing") || !strings.Contains(out, "Cold open") {
		t.Errorf("a section list must survive without prose:\n%s", out)
	}
}

// TestImprovementsDoNotRepeatThemselves: the API regularly sends the same
// sentence as the problem and the reason; a report that prints it twice under
// two labels reads like a bug.
func TestImprovementsDoNotRepeatThemselves(t *testing.T) {
	const shared = "Stops the first sentence from stalling before the premise lands."
	a := paidFixture()
	a.Improvements[0].ProblemSummary = shared
	a.Improvements[0].Why = shared
	item := ansi.Strip(improvementItem(1, a.Improvements[0], 100))
	if !strings.Contains(item, shared) {
		t.Fatalf("the problem text should still be printed:\n%s", item)
	}
	if strings.Contains(item, "Why") {
		t.Errorf("the duplicate Why row should be gone:\n%s", item)
	}

	a.Improvements[0].Why = "It names the payoff in the first sentence."
	item = ansi.Strip(improvementItem(1, a.Improvements[0], 100))
	if !strings.Contains(item, "Why") || !strings.Contains(item, "It names the payoff") {
		t.Errorf("a distinct reason must still be printed:\n%s", item)
	}
}

// TestTranscribingStageOnlyWhenObserved: a pasted script never transcribes, so
// the step only belongs on screen while the worker says it is transcribing.
func TestTranscribingStageOnlyWhenObserved(t *testing.T) {
	analyzing := paidFixture()
	analyzing.Status = api.StatusAnalyzing
	analyzing.Progress = 45
	analyzing.OverallScore = 0
	out := ansi.Strip(RenderReport(analyzing, 100))
	if strings.Contains(out, "transcribing") {
		t.Errorf("an analyzing audit must not claim to be transcribing:\n%s", out)
	}
	for _, want := range []string{"queued", "analyzing", "computing saliency", "done"} {
		if !strings.Contains(out, want) {
			t.Errorf("the checklist should still list %q:\n%s", want, out)
		}
	}

	transcribing := paidFixture()
	transcribing.Status = api.StatusTranscribing
	transcribing.Progress = 15
	transcribing.OverallScore = 0
	if out := ansi.Strip(RenderReport(transcribing, 100)); !strings.Contains(out, "transcribing") {
		t.Errorf("a transcribing audit must show its stage:\n%s", out)
	}
}

func TestReportStatusBranches(t *testing.T) {
	pending := ansi.Strip(RenderReport(pendingFixture(), 100))
	for _, want := range []string{"queued", "analyzing", "62%"} {
		if !strings.Contains(pending, want) {
			t.Errorf("a pending report should show %q:\n%s", want, pending)
		}
	}
	if strings.Contains(pending, "Overall") {
		t.Error("a pending report must not show score gauges for numbers that do not exist yet")
	}

	failed := ansi.Strip(RenderReport(failedFixture(), 100))
	if !strings.Contains(failed, "no speech in the first 60 seconds") {
		t.Error("a failed report must pass the worker's own message through")
	}
	if !strings.Contains(failed, "failed") {
		t.Error("a failed report must say it failed")
	}

	if got := RenderReport(nil, 100); got != "" {
		t.Errorf("a nil analysis must render nothing, got %q", got)
	}
	// An unknown status is not a completed report: it renders the status screen.
	unknown := ansi.Strip(RenderReport(&api.Analysis{ID: "z", VideoTitle: "t"}, 100))
	if strings.Contains(unknown, "Scores") {
		t.Error("an analysis with no status must not render as a finished report")
	}
}

func TestMessagesAndBanner(t *testing.T) {
	if !strings.Contains(ansi.Strip(Banner()), "prepublish") {
		t.Error("the banner should carry the wordmark")
	}
	for _, tc := range []struct{ got, want string }{
		{Success("saved"), "✔ saved"},
		{Info("queued"), "• queued"},
		{Warn("no email"), "! no email"},
		{Success("one\ntwo"), "✔ one\n  two"},
		{Success(""), ""},
	} {
		if got := ansi.Strip(tc.got); got != tc.want {
			t.Errorf("message: got %q, want %q", got, tc.want)
		}
	}
}

func TestReportURL(t *testing.T) {
	cases := []struct{ app, id, want string }{
		{"https://prepublish.ai", "abc", "https://prepublish.ai/analysis/abc"},
		{"https://prepublish.ai/", "abc", "https://prepublish.ai/analysis/abc"},
		{"", "abc", "https://prepublish.ai/analysis/abc"},
		{"http://localhost:3000", "", ""},
	}
	for _, tc := range cases {
		if got := ReportURL(tc.app, tc.id); got != tc.want {
			t.Errorf("ReportURL(%q, %q) = %q, want %q", tc.app, tc.id, got, tc.want)
		}
	}
}

// TestSetAppURL proves the one mutable piece of this package does what a
// --app-url caller needs, and that it is restored afterwards.
func TestSetAppURL(t *testing.T) {
	defer SetAppURL("")
	SetAppURL("http://localhost:3000/")
	out := ansi.Strip(RenderReport(paidFixture(), 100))
	if !strings.Contains(out, "http://localhost:3000/analysis/") {
		t.Errorf("the footer should use the configured app URL:\n%s", out)
	}
}

func TestRuntimeRenderer(t *testing.T) {
	out := ansi.Strip(RenderRuntime(1842, scriptinfo.Runtime(1842), 100))
	for _, want := range []string{"1,842 words", "Typical", "Slow", "Fast", "181 wpm"} {
		if !strings.Contains(out, want) {
			t.Errorf("runtime output should contain %q:\n%s", want, out)
		}
	}
	empty := ansi.Strip(RenderRuntime(0, scriptinfo.Estimate{}, 100))
	if !strings.Contains(empty, "0 words") {
		t.Errorf("an empty script should still report zero:\n%s", empty)
	}
	// A caller that forgot to derive the estimate still gets real numbers.
	derived := ansi.Strip(RenderRuntime(181, scriptinfo.Estimate{}, 100))
	if !strings.Contains(derived, "1:00") {
		t.Errorf("a missing estimate should be derived from the word count:\n%s", derived)
	}
}

func TestAccountRenderer(t *testing.T) {
	free := ansi.Strip(RenderAccount(user("ada@example.com", "active"), &api.Usage{Tier: api.TierPaid, AuditsUsedToday: 3, AuditsLimit: 50, AuditsRemaining: 47}, nil, config.SourceEnv, 100))
	for _, want := range []string{"ada@example.com", "Creator", "47 of 50 audits left today", "PREPUBLISH_API_KEY"} {
		if !strings.Contains(free, want) {
			t.Errorf("account card should contain %q:\n%s", want, free)
		}
	}
	anon := ansi.Strip(RenderAccount(nil, nil, &api.CheckFree{Remaining: 2, Limit: 3, Tier: api.TierAnonymous}, config.SourceNone, 100))
	for _, want := range []string{"Anonymous", "2 of 3 audits left today", "prepublish login"} {
		if !strings.Contains(anon, want) {
			t.Errorf("anonymous card should contain %q:\n%s", want, anon)
		}
	}

	// The provider's subscription id is internal plumbing, not something a user
	// can act on.
	subscribed := user("ada@example.com", "active")
	subscribed.SubscriptionID = "sub_01J8ZK4M2QX7"
	paid := ansi.Strip(RenderAccount(subscribed, &api.Usage{Tier: api.TierPaid, AuditsUsedToday: 3, AuditsLimit: 50, AuditsRemaining: 47}, nil, config.SourceFile, 100))
	if strings.Contains(paid, "sub_01J8ZK4M2QX7") {
		t.Errorf("the account card must not print the subscription id:\n%s", paid)
	}
	if !strings.Contains(paid, "active") {
		t.Errorf("the subscription status should still be visible:\n%s", paid)
	}
}

func TestToolRenderersShowTheirVerdicts(t *testing.T) {
	hook := ansi.Strip(RenderHook(hookFixture(), 100))
	for _, want := range []string{"C-", "46", "Rewrites", "cold open", "payoff the whole video"} {
		if !strings.Contains(hook, want) {
			t.Errorf("hook output should contain %q:\n%s", want, hook)
		}
	}
	pol := ansi.Strip(RenderPolicy(policyFixture(false), 100))
	for _, want := range []string{"review suggested", "Medical claims", "I slept better in week two", "rubric v3", "not a YouTube decision"} {
		if !strings.Contains(pol, want) {
			t.Errorf("policy output should contain %q:\n%s", want, pol)
		}
	}
	locked := ansi.Strip(RenderPolicy(policyFixture(true), 100))
	if strings.Contains(locked, "I slept better in week two") {
		t.Error("a locked policy result must not leak the matched passage")
	}
	if !strings.Contains(locked, "locked") {
		t.Error("a locked policy result must mark the withheld passages")
	}
	auth := ansi.Strip(RenderAuthenticity(authenticityFixture(), 100))
	for _, want := range []string{"medium risk", "72", "Signals", "Recycled stock phrasing", "What to change"} {
		if !strings.Contains(auth, want) {
			t.Errorf("authenticity output should contain %q:\n%s", want, auth)
		}
	}
}

// --- fixtures ---------------------------------------------------------------

func user(email, status string) *api.User {
	return &api.User{ID: "u_1", Email: email, SubscriptionStatus: status, AnalysisCount: 12,
		CreatedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}
}

func longScript() string {
	return strings.Repeat("So the reason I started this experiment was that I had read a lot about habit stacking and I wanted to see whether the research held up. ", 40)
}

func paidFixture() *api.Analysis {
	return &api.Analysis{
		ID:             "7c1f9a2e-0b4d-4a6f-9a1c-2f0b7d3e5a91",
		VideoTitle:     "I Tried 5 Morning Habits for 30 Days — Here's What Actually Worked",
		ScriptText:     longScript(),
		VideoDuration:  new(612),
		Status:         api.StatusCompleted,
		Progress:       100,
		OverallScore:   78,
		HookScore:      new(64),
		StructureScore: new(85),
		PacingScore:    new(71),
		HookAnalysis: &api.HookAnalysis{
			Score: 64, HookDuration: 22, AttentionGrabber: "a concrete result in the first sentence",
			Strengths:  []string{"names the payoff before the credentials"},
			Weaknesses: []string{"spends the first six seconds on a greeting"},
		},
		StructureAnalysis: &api.StructureAnalysis{
			Score: 85, FlowRating: "clear",
			Sections: []api.ContentSection{
				{Title: "Cold open: the result", StartPercentage: 0, EndPercentage: 8, Quality: "strong"},
				{Title: "What I changed and why", StartPercentage: 8, EndPercentage: 34, Quality: "fair"},
			},
			Strengths: []string{"every section pays off the title promise"},
		},
		PacingAnalysis: &api.PacingAnalysis{
			Score: 71, OverallPace: "steady", EnergyVariation: "low in the middle third",
			DropoffRiskPoints: []int{14, 38},
			Weaknesses:        []string{"three explanations with no example"},
		},
		RetentionCurve: []api.RetentionPoint{
			{Timestamp: 0, Retention: 92, Sentiment: "positive", Event: "cold open states the result"},
			{Timestamp: 8, Retention: 81},
			{Timestamp: 14, Retention: 64, Event: "greeting and channel business"},
			{Timestamp: 38, Retention: 49, Event: "third explanation without an example"},
			{Timestamp: 61, Retention: 52},
			{Timestamp: 94, Retention: 76, Sentiment: "positive", Event: "the honest failure section"},
			{Timestamp: 100, Retention: 74},
		},
		RetentionCurveSource: "model",
		ReadyToRecord:        true,
		Tier:                 api.TierPaid,
		Improvements: []api.Improvement{
			{
				Priority: "CRITICAL", Timestamp: "0:00–0:22", Category: "hook",
				Title:           "Open on the result, not the greeting",
				ProblemSummary:  "The first twenty-two seconds spend the hook on a greeting.",
				OriginalQuote:   "Hey everyone, welcome back to the channel.",
				Improved:        "Thirty days ago I was sleeping through my alarm.",
				Why:             "It names the payoff in the first sentence.",
				RetentionImpact: "The highest-risk window in the draft.",
			},
			{Priority: "HIGH", Timestamp: "0:22–1:40", Category: "structure", Title: "Cut the context block"},
			{Priority: "MEDIUM", Timestamp: "2:30–4:10", Title: "Break the habit list with the failure"},
		},
		OneKeyImprovement: "Open on the result. The draft loses 28 points of the attention-risk index before it says anything the title promised.",
		TitleRewrite: &api.TitleRewrite{
			Original: "I Tried 5 Morning Habits for 30 Days — Here's What Actually Worked",
			Improved: "I Tried 5 Morning Habits for 30 Days — 2 Worked, 3 Were a Waste of Time",
			Why:      "A specific outcome is a stronger promise than a reveal held back.",
		},
		Authenticity: &api.Authenticity{
			Score: 84, RiskLevel: "low",
			Issues:      []string{"paraphrases a study without naming it"},
			Suggestions: []string{"name the study"},
		},
		PolicyPreflight: &api.PolicyPreflight{
			Verdict: "review_suggested", TotalFlags: 1,
			CountsByCategory: map[string]int{"medical_claims": 1},
			Flags: []api.PolicyFlag{{
				CategoryID: "medical_claims", Severity: "medium",
				Quote: "I slept better in week two.", Why: "A health outcome with no qualification.",
				SuggestedRewrite: "I slept better in week two, though I changed three things at once.",
			}},
			RubricVersion:  3,
			ScopeStatement: "This compares the draft against YouTube's published guidelines. It is not a YouTube decision.",
			Categories: []api.PolicyCategory{{
				ID: "medical_claims", Label: "Medical claims", OfficialName: "Health and medical claims",
				Family: "ad_suitability", FamilyLabel: "advertiser-friendly",
				FamilyConsequence: "Limited or no ads.", SourceURL: "https://support.google.com/youtube/answer/6162278",
				SourceLabel: "Advertiser-friendly content guidelines", Summary: "Claims need evidence.",
			}},
		},
		ScoreExplanations: &api.ScoreExplanations{
			Hook:      "The promise arrives at 0:22, after a greeting and a channel update; the index falls 28 points before the first substantive line.",
			Structure: "Five sections, each paying off the title promise.",
			Pacing:    "Sentence length is consistent but the middle third has no example.",
		},
		RevisionSummary: &api.RevisionSummary{
			DraftNumber: 3, FirstScore: 58, PreviousScore: 71, ResolvedCount: 4,
			Implemented: []api.ImplementedAdvice{{Title: "Cut the two-minute intro", Priority: "CRITICAL", Draft: 2}},
		},
		ProcessingTimeMs: new(18400),
		CreatedAt:        time.Date(2026, 9, 29, 8, 12, 0, 0, time.UTC),
	}
}

// lockedFixture mirrors what the API actually sends an anonymous caller: the
// first improvement complete, the rest stripped to title and position, no title
// rewrite, policy passages removed.
func lockedFixture() *api.Analysis {
	a := paidFixture()
	a.Tier = api.TierAnonymous
	a.Locked = true
	a.LockReason = api.LockReasonSignupRequired
	a.ReadyToRecord = false
	a.TitleRewrite = nil
	a.Authenticity = nil
	a.ScoreExplanations = nil
	a.PolicyPreflight = &api.PolicyPreflight{
		Verdict: "review_suggested", TotalFlags: 2, Locked: true,
		CountsByCategory: map[string]int{"medical_claims": 1, "ad_suitability_violence": 1},
		Flags: []api.PolicyFlag{
			{CategoryID: "medical_claims", Severity: "medium", Locked: true},
			{CategoryID: "ad_suitability_violence", Severity: "low", Locked: true, InTitle: true},
		},
		RubricVersion:  3,
		ScopeStatement: "This compares the draft against YouTube's published guidelines. It is not a YouTube decision.",
		Categories: []api.PolicyCategory{
			{ID: "medical_claims", Label: "Medical claims", Family: "ad_suitability", FamilyLabel: "advertiser-friendly"},
			{ID: "ad_suitability_violence", Label: "Violence (mild)", Family: "ad_suitability", FamilyLabel: "advertiser-friendly"},
		},
	}
	a.Improvements = []api.Improvement{
		a.Improvements[0],
		{Priority: "HIGH", Timestamp: "0:22–1:40", Title: "Cut the context block", Locked: true,
			Improved: "SERVER-STRIPPED-REWRITE-1", ProblemSummary: "SERVER-STRIPPED-PROBLEM-1"},
		{Priority: "MEDIUM", Timestamp: "2:30–4:10", Title: "Break the habit list", Locked: true,
			Improved: "SERVER-STRIPPED-REWRITE-2", ProblemSummary: "SERVER-STRIPPED-PROBLEM-2"},
	}
	return a
}

func pendingFixture() *api.Analysis {
	return &api.Analysis{
		ID: "b41d8a70-9c2e-4d55-8f7b-1e0a3c9d6f22", VideoTitle: "Why Your Script Dies at Minute Four",
		Status: api.StatusAnalyzing, Progress: 62, Tier: api.TierPaid,
		CreatedAt: time.Date(2026, 9, 29, 9, 40, 0, 0, time.UTC),
	}
}

func failedFixture() *api.Analysis {
	return &api.Analysis{
		ID: "c9f0e2b4-11a7-4c3d-9b6e-8a2f5d0c7e13", VideoTitle: "The 10-Minute Method, Explained",
		Status: api.StatusFailed, Progress: 85,
		ErrorMessage: new("The transcription worker rejected the audio track: the file has no speech in the first 60 seconds."),
	}
}

func hookFixture() *api.HookResult {
	return &api.HookResult{
		EvaluationID: "e1a4c8f2-3b6d-4e8a-9c1f-2d5b7a0e4c36",
		HookText:     "In this video I'm going to show you the five morning habits that changed my life.",
		Niche:        "productivity", OverallScore: 46, Grade: "C-",
		OneSentenceVerdict: "The hook promises a video and then asks the viewer to wait.",
		Sentences: []api.HookSentence{
			{Quote: "In this video I'm going to show you the five morning habits that changed my life.", AttentionPull: 58, CuriosityGap: 34, PayoffDistance: "the whole video", Note: "Meta opening."},
			{Quote: "But first, a quick word about the channel.", AttentionPull: 12, CuriosityGap: 6, PayoffDistance: "never"},
		},
		TopIssues: []api.HookIssue{{Headline: "The payoff is promised, then postponed", Quote: "But first, a quick word about the channel.", Why: "A deferral in the first ten seconds causes early exits."}},
		Rewrites: []api.HookRewrite{
			{Style: "cold open", Hook: "I changed five things about my mornings. Two of them did nothing.", WhyWorks: "Names the failure first."},
			{Style: "contrarian", Hook: "Every morning routine video says wake up at 5am. I tried it for thirty days.", WhyWorks: "It disagrees with the default advice."},
		},
		DegradedFromPro: true, ModelUsed: "gpt-5.2-mini", PromptVersion: 4,
		CreatedAt: time.Date(2026, 9, 29, 9, 2, 0, 0, time.UTC),
	}
}

func authenticityFixture() *api.AuthenticityResult {
	return &api.AuthenticityResult{
		AnalysisID: "9d2b7c41-6e0a-4f8b-8d3c-7a1e5b9f0c24",
		Score:      72, RiskLevel: "medium",
		Verdict: "The draft is original in structure but leans on two stock phrases.",
		Signals: []api.AuthenticitySignal{
			{ID: 1, Name: "Recycled stock phrasing", Severity: "medium", Reason: "Two sentences match common stock phrasings.", Quote: "In today's video, we're going to dive deep into..."},
		},
		Remediation: []api.AuthenticityRemediation{
			{Title: "Name the source", Current: "studies show that habit stacking increases follow-through by 40%", Fix: "In a 2019 study at UCL, follow-through rose 40% when the habit was attached to an existing one.", Why: "It converts an unattributed claim into your own summary."},
		},
		OverrideNote: "A human reviewer confirmed the second signal is a false positive.",
		CreatedAt:    time.Date(2026, 9, 29, 9, 5, 0, 0, time.UTC),
	}
}

func policyFixture(locked bool) *api.PolicyResult {
	base := paidFixture().PolicyPreflight
	p := *base
	p.Locked = locked
	if locked {
		p.Flags = []api.PolicyFlag{{CategoryID: "medical_claims", Severity: "medium", Locked: true}}
	}
	return &api.PolicyResult{
		PreflightID: "f0c2a8e6-5d14-4b93-8a7e-6c0f3b1d9a52",
		Title:       "I Tried 5 Morning Habits for 30 Days",
		Preflight:   p,
		Cached:      true,
		CreatedAt:   time.Date(2026, 9, 29, 9, 8, 0, 0, time.UTC),
	}
}

func errorFixtures() []error {
	return []error{
		&api.APIError{Status: 401, Code: api.CodeUnauthorized, Message: "You must be logged in to access this resource."},
		&api.APIError{Status: 429, Code: api.CodeFreeAnalysisUsed, Message: "Daily analysis limit reached. Upgrade for 50 analyses a day."},
		&api.APIError{Status: 400, Code: api.CodeValidation, Field: "email", Message: "A valid email is required to run a free audit"},
		&api.APIError{Status: 429, Code: api.CodeRateLimited, RetryAfter: 42 * time.Second},
		&api.APIError{Status: 403, Code: api.CodeNoSubscription, Message: "Video and audio uploads are part of the subscription."},
		&api.APIError{Status: 409, Code: api.CodeLimitExceeded},
		&api.APIError{Status: 404, Code: api.CodeNotFound},
		&api.APIError{Status: 500, Message: "Internal server error with a very long sentence that has to wrap somewhere sensible in a narrow terminal without overflowing the width it was given."},
		&url.Error{Op: "Post", URL: "https://api.prepublish.ai/api/analyze", Err: errors.New("dial tcp: lookup api.prepublish.ai: no such host")},
		&api.APIError{Status: 0, Code: api.CodeNetworkError, Cause: errors.New("connection refused")},
		context.Canceled,
		context.DeadlineExceeded,
		errors.New("EOF while reading the script from stdin"),
	}
}
