package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/auth"
)

// RunLogin runs the browser device-authorisation flow and drives its screen: the
// big user code, the page to approve it at, whether the browser opened, a
// spinner while the CLI polls, and `o` to reopen the browser (or `c` to copy the
// code) when the first attempt did not take.
//
// RunLogin opens the browser itself, once, when the flow reports its codes; the
// caller does not have to. Quitting with ctrl+c cancels the flow and returns
// context.Canceled.
func RunLogin(ctx context.Context, login func(ctx context.Context, events chan<- auth.LoginEvent) (*api.DeviceToken, error)) (*api.DeviceToken, error) {
	if login == nil {
		return nil, context.Canceled
	}
	m, cancelFlow := newLoginSession(ctx, login)
	defer cancelFlow()

	final, err := run(ctx, m)
	cancelFlow()
	if err != nil {
		return nil, err
	}
	lm, ok := final.(loginModel)
	if !ok || lm.cancelled {
		return nil, context.Canceled
	}
	return lm.token, lm.err
}

// loginFlow is the shape of the callback RunLogin is given; naming it keeps the
// session builder readable without changing the exported signature.
type loginFlow func(ctx context.Context, events chan<- auth.LoginEvent) (*api.DeviceToken, error)

// newLoginSession starts the flow on its own goroutine and returns the model
// that presents it, plus the cancel func that stops it. RunLogin uses it, and so
// does the regression test that drives the same wiring with input disabled.
//
// The flow gets a context of its own: quitting the screen must stop the polling,
// not leave a goroutine talking to the API for the rest of the code's ten-minute
// life.
func newLoginSession(ctx context.Context, login loginFlow) (loginModel, func()) {
	flowCtx, cancelFlow := context.WithCancel(ctx)
	events := make(chan auth.LoginEvent, 4)
	results := make(chan loginResult, 1)
	go func() {
		defer close(events)
		token, err := login(flowCtx, events)
		results <- loginResult{token: token, err: err}
	}()
	return loginModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		events:  events,
		results: results,
		started: time.Now(),
	}, cancelFlow
}

// loginResult is the flow's outcome, delivered on its own channel because the
// event channel carries only what the flow reports *during* the run.
type loginResult struct {
	token *api.DeviceToken
	err   error
}

type loginModel struct {
	spinner spinner.Model

	events  <-chan auth.LoginEvent
	results <-chan loginResult
	start   *api.DeviceStart
	started time.Time

	browserErr   error
	browserTried bool
	note         string
	token        *api.DeviceToken
	err          error
	cancelled    bool
	width        int
}

// loginEventMsg carries one flow event from the channel into Update.
type loginEventMsg auth.LoginEvent

// loginEventsDoneMsg says the flow stopped reporting codes; the result channel
// still owes the screen an outcome.
type loginEventsDoneMsg struct{}

// loginResultMsg is the flow's final answer.
type loginResultMsg loginResult

// browserOpenedMsg reports the result of the browser launch attempt.
type browserOpenedMsg struct{ err error }

// copiedMsg reports the result of writing the user code to the clipboard.
type copiedMsg struct{ err error }

func (m loginModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitLoginEvent(m.events), waitLoginResult(m.results))
}

// waitLoginEvent blocks on the flow's channel inside a tea.Cmd, which Bubble Tea
// runs on its own goroutine; that is what keeps Update free.
func waitLoginEvent(events <-chan auth.LoginEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return loginEventsDoneMsg{}
		}
		return loginEventMsg(ev)
	}
}

func waitLoginResult(results <-chan loginResult) tea.Cmd {
	return func() tea.Msg { return loginResultMsg(<-results) }
}

// openBrowser is auth.OpenBrowser behind a variable so the login screen can be
// tested without launching a real browser.
var openBrowser = auth.OpenBrowser

// openBrowserCmd launches the browser. It runs as a command so the launch, which
// shells out, never blocks Update.
func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg { return browserOpenedMsg{err: openBrowser(url)} }
}

// copyCmd writes the user code to the system clipboard. Clipboard support is best
// effort: on a machine without one the error becomes the on-screen note rather
// than a failure.
func copyCmd(code string) tea.Cmd {
	return func() tea.Msg { return copiedMsg{err: clipboard.WriteAll(code)} }
}

func (m loginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case loginEventMsg:
		ev := auth.LoginEvent(msg)
		switch ev.Kind {
		case auth.Started:
			m.start = ev.Start
			// The browser launch and the next event are independent, so both
			// commands go back at once. Returning only the browser command
			// stops the screen reading its channel: the buffer fills, the flow
			// blocks on its next emit, and an approval in the browser is never
			// seen.
			return m, tea.Batch(openBrowserCmd(ev.Start.VerificationURIComplete), waitLoginEvent(m.events))
		default:
			if m.start == nil {
				m.start = ev.Start
			}
			return m, waitLoginEvent(m.events)
		}
	case loginEventsDoneMsg:
		return m, nil
	case loginResultMsg:
		m.token, m.err = msg.token, msg.err
		return m, tea.Quit
	case browserOpenedMsg:
		m.browserTried = true
		m.browserErr = msg.err
		return m, nil
	case copiedMsg:
		switch {
		case msg.err != nil:
			m.note = "no clipboard on this machine — type the code instead"
		default:
			m.note = "code copied"
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		case "o":
			if m.start != nil {
				m.browserTried = false
				m.note = ""
				return m, openBrowserCmd(m.start.VerificationURIComplete)
			}
		case "c":
			if m.start != nil {
				return m, copyCmd(m.start.UserCode)
			}
		}
	}
	return m, nil
}

func (m loginModel) View() tea.View {
	var b strings.Builder
	b.WriteString(styTitle.Render("Sign in to Prepublish") + "\n")
	b.WriteString(styDim.Render("Approve the code in your browser. This screen is only waiting.") + "\n\n")

	if m.start == nil {
		b.WriteString("  " + m.spinner.View() + " " + styDim.Render("requesting a code…") + "\n")
		b.WriteString("\n" + hint("ctrl+c", "cancel") + "\n")
		v := tea.NewView(b.String())
		v.AltScreen = true
		return v
	}

	b.WriteString("  " + spacedCode(m.start.UserCode) + "\n\n")
	b.WriteString("  " + styFaint.Render("approval page") + "\n")
	b.WriteString("  " + styAccent.Render(urlOr(m.start.VerificationURI)) + "\n\n")

	switch {
	case !m.browserTried:
		b.WriteString("  " + m.spinner.View() + " " + styDim.Render("opening your browser…") + "\n")
	case m.browserErr != nil:
		b.WriteString("  " + styWarn.Render("! no browser opened") + " " +
			styDim.Render("— open the page above yourself, then press o to retry") + "\n")
	default:
		b.WriteString("  " + styGood.Render("✔ browser opened") + "\n")
	}
	if m.note != "" {
		b.WriteString("  " + styDim.Render(m.note) + "\n")
	}

	elapsed := time.Since(m.started)
	b.WriteString("\n  " + m.spinner.View() + " " + styDim.Render("waiting for approval") +
		styFaint.Render("  "+elapsed.Round(time.Second).String()) + "\n")
	if m.start.ExpiresIn > 0 {
		left := time.Duration(m.start.ExpiresIn)*time.Second - elapsed
		if left > 0 {
			b.WriteString("  " + styFaint.Render("the code expires in "+left.Round(time.Second).String()+" if nothing is approved") + "\n")
		} else {
			b.WriteString("  " + styWarn.Render("the code has expired — press ctrl+c and run it again") + "\n")
		}
	}

	b.WriteString("\n" + hint("o", "reopen browser", "c", "copy code", "ctrl+c", "cancel") + "\n")
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// spacedCode widens the user code so it can be read off a screen and typed into
// a phone without losing the place: one column per character, a wider gap
// between the halves.
func spacedCode(raw string) string {
	letters := make([]rune, 0, len(raw))
	for _, r := range raw {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			letters = append(letters, r)
		}
	}
	if len(letters) == 0 {
		return styAccent.Render(raw)
	}
	var out strings.Builder
	for i, r := range letters {
		if i > 0 {
			out.WriteString(" ")
			if i == len(letters)/2 {
				out.WriteString(" ")
			}
		}
		out.WriteRune(r)
	}
	return styAccent.Render(out.String())
}

func urlOr(u string) string {
	if strings.TrimSpace(u) == "" {
		return "(the flow did not report an approval page)"
	}
	return u
}
