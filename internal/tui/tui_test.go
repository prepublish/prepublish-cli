package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/auth"
	"github.com/prepublish/prepublish-cli/internal/config"
)

// These tests drive the models the way the program does — messages in, view out
// — rather than starting a terminal. What they hold: the selection contract the
// commands switch on, that an interrupt is reported as context.Canceled, and
// that every screen stays inside the terminal width.

// press builds the key message the runtime would deliver for one keystroke.
func press(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	default:
		r := []rune(key)[0]
		return tea.KeyPressMsg{Code: r, Text: key}
	}
}

// cast asserts the concrete type Update returned. The models have value
// receivers and the tea.Model interface erases that, so a test that keeps
// working with the model has to assert at every step.
func cast[T any](t *testing.T, m tea.Model) T {
	t.Helper()
	v, ok := m.(T)
	if !ok {
		t.Fatalf("Update returned %T", m)
	}
	return v
}

// blockWordmarkRow is the top row of the block wordmark, which is the cheapest
// way for a test to assert that the large mark was drawn rather than the
// one-line fallback.
func blockWordmarkRow(row int) string {
	mark, ok := wordmark("prepublish", 100)
	if !ok {
		panic("the block wordmark should fit in 100 columns")
	}
	return ansi.Strip(strings.Split(mark, "\n")[row])
}

// spinnerForTest builds a real spinner model. A zero spinner panics when it is
// asked to render, which is why every model under test gets one.
func spinnerForTest() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.MiniDot))
}

func sized(m tea.Model, w, h int) tea.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next
}

func assertViewFits(t *testing.T, label string, content string, w int) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if got := ansi.StringWidth(ansi.Strip(line)); got > w {
			t.Errorf("%s: line %d is %d columns wide, want <= %d\n%s", label, i+1, got, w, line)
		}
	}
}

// TestHomeConditionsTheMenuOnTheCredential: the menu must not offer a sign-in to
// someone already signed in, nor a sign-out to someone who is not.
func TestHomeConditionsTheMenuOnTheCredential(t *testing.T) {
	anon := newHomeModel(HomeState{
		Width: 80, APIURL: "https://api.prepublish.ai", Version: "v1",
		Free: &api.CheckFree{Remaining: 2, Limit: 3, Tier: api.TierAnonymous},
	})
	labels := map[HomeChoice]string{}
	for _, it := range anon.items {
		labels[it.choice] = it.label
	}
	if _, ok := labels[HomeLogin]; !ok {
		t.Error("an anonymous caller must be offered sign in")
	}
	if _, ok := labels[HomeLogout]; ok {
		t.Error("an anonymous caller must not be offered sign out")
	}

	signedIn := newHomeModel(HomeState{
		Width: 80, User: &api.User{Email: "ada@example.com"},
		Usage:  &api.Usage{Tier: api.TierPaid, AuditsLimit: 50, AuditsRemaining: 47},
		Source: config.SourceFile, Version: "v1",
	})
	labels = map[HomeChoice]string{}
	for _, it := range signedIn.items {
		labels[it.choice] = it.label
	}
	if _, ok := labels[HomeLogout]; !ok {
		t.Error("a signed-in caller must be offered sign out")
	}
	if _, ok := labels[HomeLogin]; ok {
		t.Error("a signed-in caller must not be offered sign in")
	}

	for _, w := range []int{100, 80, 60} {
		content := sized(anon, w, 24).(homeModel).View().Content
		assertViewFits(t, "home", content, w)
		plain := ansi.Strip(content)
		for _, want := range []string{
			"Anonymous", "2 of 3 audits left today",
			"Audit a script", "Sign in", "Quit",
		} {
			if !strings.Contains(plain, want) {
				t.Errorf("home at %d columns should show %q:\n%s", w, want, plain)
			}
		}
		// A wide screen gets the block wordmark; a signed-out caller gets one
		// styled identity, not a bare "not signed in" beside a pill that
		// already says Anonymous.
		if !strings.Contains(plain, blockWordmarkRow(0)) {
			t.Errorf("home at %d columns should show the block wordmark:\n%s", w, plain)
		}
		if strings.Contains(plain, "not signed in") {
			t.Errorf("home at %d columns should not repeat the anonymous state in prose:\n%s", w, plain)
		}
	}

	// Narrow screens fall back to the one-line mark rather than squeezing the
	// block letters into a width they do not fit.
	narrow := ansi.Strip(sized(anon, 40, 24).(homeModel).View().Content)
	if !strings.Contains(narrow, "prepublish") {
		t.Errorf("a narrow home should fall back to the one-line wordmark:\n%s", narrow)
	}
	if strings.Contains(narrow, blockWordmarkRow(0)) {
		t.Errorf("a narrow home must not draw the block wordmark:\n%s", narrow)
	}
}

// TestHomeOffersTheNextPlanNotTheCurrentOne: a paid caller must not be sold the
// plan they already have, and a Studio caller has nothing left to buy here.
func TestHomeOffersTheNextPlanNotTheCurrentOne(t *testing.T) {
	free := newHomeModel(HomeState{Width: 100, User: &api.User{Email: "ada@example.com"},
		Usage: &api.Usage{Tier: api.TierFreeAccount, AuditsLimit: 3, AuditsRemaining: 2}})
	if label, desc := itemFor(free, HomeUpgrade); label != "Upgrade" || !strings.Contains(desc, "Creator") {
		t.Errorf("a free account should be offered Creator, got %q / %q", label, desc)
	}

	paid := newHomeModel(HomeState{Width: 100, User: &api.User{Email: "ada@example.com"},
		Usage: &api.Usage{Tier: api.TierPaid, AuditsLimit: 50, AuditsRemaining: 47}})
	if label, desc := itemFor(paid, HomeUpgrade); label != "Upgrade to Studio" || !strings.Contains(desc, "Studio") {
		t.Errorf("a Creator should be offered Studio, got %q / %q", label, desc)
	}
	if strings.Contains(itemDesc(paid, HomeUpgrade), "Creator:") {
		t.Errorf("a Creator must not be sold Creator: %q", itemDesc(paid, HomeUpgrade))
	}

	studio := newHomeModel(HomeState{Width: 100, User: &api.User{Email: "studio@agency.example"},
		Usage: &api.Usage{Tier: api.TierStudio, AuditsLimit: 50, AuditsRemaining: 24}})
	if _, ok := findItem(studio, HomeUpgrade); ok {
		t.Error("a Studio subscriber has nothing left to buy: the upgrade item should be hidden")
	}
	if _, ok := findItem(studio, HomeQuit); !ok {
		t.Error("hiding the upgrade item must not disturb the rest of the menu")
	}
}

// columnOf is the display column a substring starts in. Byte offsets are wrong
// here: the cursor marker is three bytes and one column.
func columnOf(line, sub string) int {
	i := strings.Index(line, sub)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(line[:i])
}

func findItem(m homeModel, want HomeChoice) (homeItem, bool) {
	for _, it := range m.items {
		if it.choice == want {
			return it, true
		}
	}
	return homeItem{}, false
}

func itemFor(m homeModel, want HomeChoice) (label, desc string) {
	if it, ok := findItem(m, want); ok {
		return it.label, it.desc
	}
	return "", ""
}

func itemDesc(m homeModel, want HomeChoice) string {
	_, desc := itemFor(m, want)
	return desc
}

// TestHistoryColumnsAlignWithTheirHeader: the labels and the values must be laid
// out by the same arithmetic, or the header reads "ScoreStatus".
func TestHistoryColumnsAlignWithTheirHeader(t *testing.T) {
	page := &api.AnalysisPage{
		Analyses: []*api.Analysis{
			{ID: "a1", VideoTitle: "First draft", Status: api.StatusCompleted, OverallScore: 81, CreatedAt: time.Date(2026, 9, 29, 11, 12, 0, 0, time.Local)},
			{ID: "a2", VideoTitle: "Second draft", Status: api.StatusFailed, CreatedAt: time.Date(2026, 9, 28, 11, 12, 0, 0, time.Local)},
		},
		Total: 2, Page: 1, PerPage: 10, TotalPages: 1,
	}
	for _, w := range []int{100, 80, 60, 40} {
		m := historyModel{ctx: context.Background(), spinner: spinnerForTest(), items: page.Analyses}
		m = cast[historyModel](t, sized(m, w, 24))
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		var header, row string
		for _, line := range lines {
			if strings.Contains(line, "Title") && strings.Contains(line, "Score") {
				header = line
			} else if row == "" && strings.Contains(line, "First draft") {
				row = line
			}
		}
		if header == "" || row == "" {
			t.Fatalf("at %d columns the table is missing a header or a row:\n%s", w, strings.Join(lines, "\n"))
		}
		if strings.Contains(header, "ScoreStatus") {
			t.Errorf("at %d columns the header columns have no gap:\n%s", w, header)
		}
		// Every label starts in the same display column as the value under it.
		// Columns the width could not seat are absent from both, and are skipped.
		for _, col := range []struct{ label, value string }{
			{"Title", "First draft"},
			{"Score", "81"},
			{"Status", "completed"},
			{"Created", "29 Sep 11:12"},
		} {
			if strings.Contains(header, col.label) == false {
				continue
			}
			labelAt, valueAt := columnOf(header, col.label), columnOf(row, col.value)
			if labelAt < 0 || valueAt < 0 {
				t.Fatalf("at %d columns %q is in the header but %q is not in the row:\n%s\n%s", w, col.label, col.value, header, row)
			}
			if labelAt != valueAt {
				t.Errorf("at %d columns %q starts at column %d but its value starts at %d:\n%s\n%s", w, col.label, labelAt, valueAt, header, row)
			}
		}
		if lipgloss.Width(header) > w || lipgloss.Width(row) > w {
			t.Errorf("at %d columns the table overflows: %d / %d", w, lipgloss.Width(header), lipgloss.Width(row))
		}
	}
}

// TestHomeReturnsTheChosenItem is the contract the commands depend on: arrows
// move, enter selects, q quits, and the returned choice names the item.
func TestHomeReturnsTheChosenItem(t *testing.T) {
	m := tea.Model(newHomeModel(HomeState{Width: 80}))
	m = sized(m, 80, 24)
	if got := m.(homeModel).cursor; got != 0 {
		t.Fatalf("the menu should start on the first item, got %d", got)
	}
	for range 2 {
		next, _ := m.Update(press("down"))
		m = next
	}
	if got := cast[homeModel](t, m).cursor; got != 2 {
		t.Fatalf("two downs should move two items, got %d", got)
	}
	for range 3 {
		next, _ := m.Update(press("up"))
		m = next
	}
	if got, want := cast[homeModel](t, m).cursor, len(cast[homeModel](t, m).items)-1; got != want {
		t.Errorf("moving up from the first item must wrap to the last, got %d want %d", got, want)
	}

	selected, cmd := m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("selecting an item must quit the program")
	}
	if choice := cast[homeModel](t, selected).choice; choice != HomeQuit {
		t.Errorf("want the last item's choice, got %q", choice)
	}

	// ctrl+c quits without selecting, even with an item highlighted.
	fresh := tea.Model(newHomeModel(HomeState{Width: 80}))
	fresh = sized(fresh, 80, 24)
	if fresh.(homeModel).items[0].choice != HomeAudit {
		t.Fatal("the fixture should start on the audit item")
	}
	nextCtrl, cmd := fresh.Update(press("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c must quit the program")
	}
	if got := cast[homeModel](t, nextCtrl).choice; got != HomeQuit {
		t.Errorf("ctrl+c must quit rather than select the highlighted item, got %q", got)
	}
}

// TestWorkRunnerDeliversAndCancels covers the plumbing every long call depends
// on: events arrive in order, the final result arrives once, and a cancelled
// runner reports its context instead of hanging.
func TestWorkRunnerDeliversAndCancels(t *testing.T) {
	runner := newWorkRunner(context.Background())
	go func() {
		runner.send(workEvent{analysis: &api.Analysis{Status: api.StatusAnalyzing, Progress: 40}})
		runner.send(workEvent{done: true, result: &api.Analysis{Status: api.StatusCompleted, Progress: 100}})
	}()
	first, ok := runner.wait()().(workEvent)
	if !ok || first.analysis == nil || first.analysis.Progress != 40 {
		t.Fatalf("first event should be the progress snapshot, got %#v", first)
	}
	second := runner.wait()().(workEvent)
	if !second.done || second.result == nil || second.result.Status != api.StatusCompleted {
		t.Fatalf("second event should be the completed result, got %#v", second)
	}
	runner.stop()

	cancelled := newWorkRunner(context.Background())
	cancelled.stop()
	ev := cancelled.wait()().(workEvent)
	if !ev.done || !errors.Is(ev.err, context.Canceled) {
		t.Fatalf("a stopped runner must report cancellation, got %#v", ev)
	}
}

// TestAuditProgressReportsCancellation: interrupting the screen must reach the
// caller as context.Canceled, which is what the commands test for.
func TestAuditProgressReportsCancellation(t *testing.T) {
	m := tea.Model(auditModel{
		spinner: spinnerForTest(),
		runner:  newWorkRunner(context.Background()),
		started: time.Now(),
	})
	m = sized(m, 100, 24)
	next, cmd := m.Update(press("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c must quit the program")
	}
	am := cast[auditModel](t, next)
	if !am.cancelled {
		t.Error("ctrl+c must mark the model as cancelled so RunAuditProgress returns context.Canceled")
	}
	content := am.View().Content
	assertViewFits(t, "audit", content, 100)
	if !strings.Contains(ansi.Strip(content), "queued") {
		t.Error("the stage checklist should be on screen")
	}
}

// TestAuditChecklistOmitsTranscribingUntilSeen: a pasted script never
// transcribes, so the step only appears once the worker has said it is
// transcribing.
func TestAuditChecklistOmitsTranscribingUntilSeen(t *testing.T) {
	pasted := auditModel{spinner: spinnerForTest(), runner: newWorkRunner(context.Background()), started: time.Now()}
	pasted.observe(&api.Analysis{Status: api.StatusPending, Progress: 5})
	pasted.observe(&api.Analysis{Status: api.StatusAnalyzing, Progress: 45})
	content := ansi.Strip(pasted.View().Content)
	if strings.Contains(content, "transcribing") {
		t.Errorf("an analyzing audit must not show a transcription step:\n%s", content)
	}
	for _, want := range []string{"queued", "analyzing", "computing saliency", "done"} {
		if !strings.Contains(content, want) {
			t.Errorf("the checklist should list %q:\n%s", want, content)
		}
	}

	uploaded := auditModel{spinner: spinnerForTest(), runner: newWorkRunner(context.Background()), started: time.Now()}
	uploaded.observe(&api.Analysis{Status: api.StatusTranscribing, Progress: 20})
	uploaded.observe(&api.Analysis{Status: api.StatusAnalyzing, Progress: 60})
	content = ansi.Strip(uploaded.View().Content)
	if !strings.Contains(content, "transcribing") {
		t.Errorf("a file audit that transcribed must keep the step on screen:\n%s", content)
	}
	assertViewFits(t, "audit checklist", content, 100)
}

// TestRunnerEntryPointsRejectNothingToDo keeps the guards honest: a nil callback
// is a programming error, and must come back as an error rather than a hang.
func TestRunnerEntryPointsRejectNothingToDo(t *testing.T) {
	ctx := context.Background()
	if _, err := RunAuditProgress(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("nil wait should report cancellation, got %v", err)
	}
	if _, err := RunUploadProgress(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("nil upload should report cancellation, got %v", err)
	}
	if _, err := RunLogin(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("nil login should report cancellation, got %v", err)
	}
}

// TestLoginFlowDrainsEveryEvent is the regression test for the login deadlock:
// the screen must keep reading its event channel after it starts the browser,
// because the flow's sends are blocking. If it stops reading, the channel's
// buffer fills, the flow blocks on its next emit and an approval in the browser
// is never seen — the flow then reports its own stall, which is what this test
// asserts against.
func TestLoginFlowDrainsEveryEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var opened []string
	restore := openBrowser
	openBrowser = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	t.Cleanup(func() { openBrowser = restore })

	const polls = 8
	var stalled error
	login := func(ctx context.Context, events chan<- auth.LoginEvent) (*api.DeviceToken, error) {
		start := &api.DeviceStart{
			UserCode: "BCDF-GHJK", VerificationURI: "https://prepublish.ai/cli/login",
			VerificationURIComplete: "https://prepublish.ai/cli/login?code=BCDF-GHJK",
			ExpiresIn:               600, Interval: 2,
		}
		// Every send is blocking, with a deadline: a reader that stops reading
		// turns the flow into a stall instead of a hang.
		deadline := time.After(10 * time.Second)
		emit := func(ev auth.LoginEvent) error {
			select {
			case events <- ev:
				return nil
			case <-deadline:
				stalled = errors.New("the login screen stopped reading its events")
				return stalled
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := emit(auth.LoginEvent{Kind: auth.Started, Start: start}); err != nil {
			return nil, err
		}
		for range polls {
			if err := emit(auth.LoginEvent{Kind: auth.Polling, Start: start}); err != nil {
				return nil, err
			}
		}
		return &api.DeviceToken{APIKey: "pp_live_test", KeyPrefix: "pp_live_test"}, nil
	}

	m, cancelFlow := newLoginSession(ctx, login)
	defer cancelFlow()

	var out bytes.Buffer
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(nil),
		tea.WithOutput(&out),
		tea.WithWindowSize(100, 30),
	)
	final, err := p.Run()
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	if stalled != nil {
		t.Fatal(stalled)
	}
	lm, ok := final.(loginModel)
	if !ok || lm.token == nil {
		t.Fatalf("the issued key must come back out of the login screen: %#v", final)
	}
	if lm.token.APIKey != "pp_live_test" {
		t.Errorf("wrong token: %q", lm.token.APIKey)
	}
	if len(opened) != 1 || !strings.Contains(opened[0], "code=BCDF-GHJK") {
		t.Errorf("the screen must open the approval page once, got %v", opened)
	}
	if !strings.Contains(out.String(), "B C D F  G H J K") {
		t.Errorf("the rendered frame should show the user code:\n%s", out.String())
	}
}

// TestLoginScreenShowsTheCode: the screen owes the user a readable code, the
// page to approve it at, and an honest statement that the browser may not have
// opened.
func TestLoginScreenShowsTheCode(t *testing.T) {
	start := &api.DeviceStart{
		UserCode: "BCDF-GHJK", VerificationURI: "https://prepublish.ai/cli/login",
		VerificationURIComplete: "https://prepublish.ai/cli/login?code=BCDF-GHJK",
		ExpiresIn:               600, Interval: 2,
	}
	m := loginModel{spinner: spinnerForTest(), started: time.Now()}
	next, _ := m.Update(loginEventMsg{Kind: auth.Started, Start: start})
	m = cast[loginModel](t, next)
	next, _ = m.Update(browserOpenedMsg{err: errors.New("no browser")})
	m = cast[loginModel](t, next)
	next, _ = m.Update(copiedMsg{err: nil})
	m = cast[loginModel](t, next)

	content := ansi.Strip(sized(m, 100, 24).(loginModel).View().Content)
	for _, want := range []string{"B C D F  G H J K", "https://prepublish.ai/cli/login", "no browser opened", "code copied"} {
		if !strings.Contains(content, want) {
			t.Errorf("login screen should contain %q:\n%s", want, content)
		}
	}
	assertViewFits(t, "login", content, 100)

	// The terminal event ends the screen and carries the key out.
	nextDone, cmd := m.Update(loginResultMsg{token: &api.DeviceToken{APIKey: "pp_live_x"}})
	if cmd == nil {
		t.Fatal("the flow finishing must quit the screen")
	}
	done := cast[loginModel](t, nextDone)
	if done.token == nil || done.token.APIKey != "pp_live_x" {
		t.Error("the issued key must be carried out of the screen")
	}
}

// TestHistoryNavigatesAndOpens covers the two callbacks: fetch on init and page
// changes, open on enter, and the viewer's esc returning to the list.
func TestHistoryNavigatesAndOpens(t *testing.T) {
	page := &api.AnalysisPage{
		Analyses: []*api.Analysis{
			{ID: "a1", VideoTitle: "First draft", Status: api.StatusCompleted, OverallScore: 81},
			{ID: "a2", VideoTitle: "Second draft", Status: api.StatusCompleted, OverallScore: 42},
			{ID: "a3", VideoTitle: "Third draft", Status: api.StatusAnalyzing, Progress: 20},
		},
		Total: 3, Page: 1, PerPage: 10, TotalPages: 2,
	}
	var opened, fetched []string
	m := historyModel{
		ctx: context.Background(),
		fetch: func(_ context.Context, p int) (*api.AnalysisPage, error) {
			fetched = append(fetched, itoa(p))
			return page, nil
		},
		open: func(_ context.Context, id string) (string, error) {
			opened = append(opened, id)
			return "REPORT FOR " + id, nil
		},
		spinner: spinnerForTest(),
		loading: true,
	}
	m = cast[historyModel](t, sized(m, 100, 24))

	next, _ := m.Update(historyPageMsg{page: 1, result: page})
	m = cast[historyModel](t, next)
	content := ansi.Strip(m.View().Content)
	for _, want := range []string{"History", "First draft", "81", "completed", "page 1 of 2"} {
		if !strings.Contains(content, want) {
			t.Errorf("history should show %q:\n%s", want, content)
		}
	}

	nextMove, _ := m.Update(press("down"))
	m = cast[historyModel](t, nextMove)
	nextEnter, cmd := m.Update(press("enter"))
	if cmd == nil {
		t.Fatal("enter must start opening the selected audit")
	}
	m = cast[historyModel](t, nextEnter)
	nextOpen, _ := m.Update(cmd())
	m = cast[historyModel](t, nextOpen)
	if len(opened) != 1 || opened[0] != "a2" {
		t.Fatalf("want the second row opened, got %v", opened)
	}
	if m.viewer == nil {
		t.Fatal("opening a report must show the pager")
	}
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "REPORT FOR a2") {
		t.Errorf("the pager should show the report:\n%s", got)
	}

	// esc goes back to the list, and the page keys ask the fetch callback.
	back, _ := m.Update(press("esc"))
	m = cast[historyModel](t, back)
	if m.viewer != nil {
		t.Error("esc must return to the list")
	}
	nextPage, pageCmd := m.Update(press("n"))
	m = cast[historyModel](t, nextPage)
	if pageCmd == nil {
		t.Fatal("n must ask for the next page")
	}
	nextFetched, _ := m.Update(pageCmd())
	m = cast[historyModel](t, nextFetched)
	if len(fetched) == 0 || fetched[len(fetched)-1] != "2" {
		t.Errorf("n should fetch the next page, fetched=%v", fetched)
	}

	assertViewFits(t, "history", ansi.Strip(m.View().Content), 100)
}

// TestHistoryReportsAFatalFetchAsAnError: when the first page cannot be fetched
// there is no list to show, so the command has to hear about it.
func TestHistoryReportsAFatalFetchAsAnError(t *testing.T) {
	want := &api.APIError{Status: 401, Code: api.CodeUnauthorized}
	m := historyModel{ctx: context.Background(), fetch: func(context.Context, int) (*api.AnalysisPage, error) {
		return nil, want
	}, spinner: spinnerForTest(), loading: true}
	m = cast[historyModel](t, sized(m, 80, 24))
	nextFetch, cmd := m.Update(historyPageMsg{page: 1, err: want})
	if cmd == nil {
		t.Fatal("a fatal fetch must quit the screen")
	}
	if got := cast[historyModel](t, nextFetch); !errors.Is(got.err, want) {
		t.Errorf("the fetch error must be carried out, got %v", got.err)
	}
}

func TestViewerPresentsTheReport(t *testing.T) {
	report := strings.Join([]string{
		"1 Scores", strings.Repeat("─", 40), "Overall 78", "", "2 Attention",
		strings.Repeat("line\n", 40),
	}, "\n")
	m := tea.Model(newViewer(report, nil))
	m = sized(m, 80, 12)
	content := m.View().Content
	if !strings.Contains(ansi.Strip(content), "Scores") {
		t.Errorf("the viewer should show the top of the report:\n%s", content)
	}
	if _, cmd := m.Update(press("q")); cmd == nil {
		t.Error("q must quit the pager")
	}
	// The footer names the keys it answers to, once each: a footer that says
	// "quit" twice and never mentions q is worse than no footer.
	footer := ansi.Strip(m.View().Content)
	if !strings.Contains(footer, "q/esc quit") {
		t.Errorf("a standalone pager must name q and esc once each:\n%s", footer)
	}
	if strings.Count(footer, "quit") != 1 {
		t.Errorf("the footer must not repeat its own labels:\n%s", footer)
	}

	// The pager knows where it is, and scrolling to the bottom says so.
	m, _ = m.Update(press("G"))
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "100%") {
		t.Errorf("the footer should report the scroll position:\n%s", got)
	}
	assertViewFits(t, "viewer", ansi.Strip(m.View().Content), 80)
}

// TestCancelledMapsInterrupts pins the error normalisation every entry point
// relies on.
func TestCancelledMapsInterrupts(t *testing.T) {
	ctx := context.Background()
	if err := cancelled(ctx, nil); err != nil {
		t.Errorf("a clean exit must stay clean, got %v", err)
	}
	if err := cancelled(ctx, tea.ErrInterrupted); !errors.Is(err, context.Canceled) {
		t.Errorf("an interrupt must read as cancellation, got %v", err)
	}
	if err := cancelled(ctx, tea.ErrProgramKilled); !errors.Is(err, context.Canceled) {
		t.Errorf("a killed program must read as cancellation, got %v", err)
	}
	boom := errors.New("boom")
	if err := cancelled(ctx, boom); !errors.Is(err, boom) {
		t.Errorf("a real failure must pass through, got %v", err)
	}
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if err := cancelled(dead, boom); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context must win over a later error, got %v", err)
	}
}

func TestSpacedCodeWidensEvenly(t *testing.T) {
	if got := ansi.Strip(spacedCode("BCDF-GHJK")); got != "B C D F  G H J K" {
		t.Errorf("spacedCode: got %q", got)
	}
	if got := ansi.Strip(spacedCode("abc123")); got != "a b c  1 2 3" {
		t.Errorf("spacedCode without a separator: got %q", got)
	}
}

func TestBytesFormatting(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"}, {512, "512 B"}, {1024, "1 KB"}, {1536, "1.5 KB"},
		{8 * 1024 * 1024, "8 MB"}, {5 * 1024 * 1024 * 1024, "5 GB"},
	}
	for _, tc := range cases {
		if got := byteSize(tc.in); got != tc.want {
			t.Errorf("byteSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
