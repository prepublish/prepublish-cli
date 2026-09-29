package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/prepublish/prepublish-cli/internal/api"
)

// RunAuditProgress shows an audit while the server works on it: a spinner, the
// worker's own stage checklist, a progress bar and the elapsed time. wait is
// called once, on its own goroutine, and reports every poll through onUpdate.
//
// It returns the analysis the wait function returned, that function's error, or
// context.Canceled when the user interrupted it.
func RunAuditProgress(ctx context.Context, wait func(ctx context.Context, onUpdate func(*api.Analysis)) (*api.Analysis, error)) (*api.Analysis, error) {
	if wait == nil {
		return nil, context.Canceled
	}
	runner := newWorkRunner(ctx)
	go func() {
		result, err := wait(runner.ctx, func(a *api.Analysis) {
			runner.send(workEvent{analysis: a})
		})
		runner.send(workEvent{done: true, result: result, err: err})
	}()

	m := auditModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		runner:  runner,
		started: time.Now(),
	}
	final, err := run(ctx, m)
	if err != nil {
		runner.stop()
		return nil, err
	}
	runner.stop()
	am, ok := final.(auditModel)
	if !ok || am.cancelled {
		return nil, context.Canceled
	}
	return am.result, am.err
}

// auditModel is the audit progress screen.
type auditModel struct {
	spinner spinner.Model
	runner  *workRunner

	started   time.Time
	elapsed   time.Duration
	stage     string
	progressV int
	title     string
	// sawTranscribing records that the worker really did transcribe, which is
	// the only evidence that a transcription step belongs in the checklist.
	sawTranscribing bool

	result    *api.Analysis
	err       error
	cancelled bool
	width     int
}

// workEvent is one report from a background goroutine: a progress snapshot, or
// the final outcome. The audit and upload screens share it, and each reads the
// fields its own call produces.
type workEvent struct {
	analysis *api.Analysis
	upload   *uploadEvent
	result   *api.Analysis
	uploadID string
	err      error
	done     bool
}

// workRunner owns the goroutine that drives a long call. Its job is to keep
// every blocking operation off the Update path, and to make sure that goroutine
// stops when the program does: send gives up as soon as the context is done, so
// a UI that quits mid-flight cannot leak a blocked producer.
type workRunner struct {
	ctx     context.Context
	cancel  context.CancelFunc
	updates chan workEvent
}

func newWorkRunner(parent context.Context) *workRunner {
	ctx, cancel := context.WithCancel(parent)
	return &workRunner{ctx: ctx, cancel: cancel, updates: make(chan workEvent, 8)}
}

func (w *workRunner) send(ev workEvent) {
	select {
	case w.updates <- ev:
	case <-w.ctx.Done():
	}
}

func (w *workRunner) stop() { w.cancel() }

// wait returns the command that blocks until the next event. Bubble Tea runs
// each command on its own goroutine, which is what makes a plain receive here
// safe: it never touches the Update path.
func (w *workRunner) wait() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-w.updates:
			return ev
		case <-w.ctx.Done():
			return workEvent{done: true, err: w.ctx.Err()}
		}
	}
}

func (m auditModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runner.wait())
}

func (m auditModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case workEvent:
		if msg.analysis != nil {
			m.observe(msg.analysis)
		}
		if msg.done {
			m.result, m.err = msg.result, msg.err
			if m.result != nil {
				m.observe(m.result)
			}
			return m, tea.Quit
		}
		return m, m.runner.wait()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.elapsed = time.Since(m.started)
		return m, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			m.runner.stop()
			return m, tea.Quit
		}
	}
	return m, nil
}

// observe folds one status update into the screen's state.
func (m *auditModel) observe(a *api.Analysis) {
	m.stage = a.Status
	m.progressV = a.Progress
	if a.Status == api.StatusTranscribing {
		m.sawTranscribing = true
	}
	if a.VideoTitle != "" {
		m.title = a.VideoTitle
	}
}

func (m auditModel) View() tea.View {
	stage := m.stage
	if stage == "" {
		stage = api.StatusPending
	}
	elapsed := m.elapsed
	if elapsed == 0 {
		elapsed = time.Since(m.started)
	}

	var b strings.Builder
	if m.title != "" {
		b.WriteString(styTitle.Render(m.title) + "\n")
	}
	b.WriteString(m.spinner.View() + " " + styAccent.Render(stageLabel(stage)) +
		"  " + styDim.Render(elapsed.Round(time.Second).String()) + "\n\n")
	b.WriteString("  " + bar(float64(clampInt(m.progressV, 0, 100))/100, 48) +
		"  " + styDim.Render(itoa(m.progressV)+"%") + "\n\n")
	b.WriteString(stageList(m.stage, m.sawTranscribing) + "\n")
	b.WriteString("\n" + hint("ctrl+c", "cancel"))
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, clampInt(m.progressV, 0, 100))
	return v
}

// stageList draws the worker's pipeline with the current stage marked. The two
// screens that render a checklist are separate on purpose: this one is live and
// can tell what has actually happened, that one is a snapshot and cannot.
//
// Transcription is listed only once it has been seen. A pasted script never
// transcribes, and a step nobody watched happen is worse than an absent one.
type auditStage struct{ key, label string }

func stageList(current string, sawTranscribing bool) string {
	stages := []auditStage{{api.StatusPending, "queued"}}
	if sawTranscribing {
		stages = append(stages, auditStage{api.StatusTranscribing, "transcribing"})
	}
	stages = append(stages,
		auditStage{api.StatusAnalyzing, "analyzing"},
		auditStage{api.StatusComputingSaliency, "computing saliency"},
		auditStage{api.StatusCompleted, "done"},
	)
	if current == "" {
		current = api.StatusPending
	}
	idx := 0
	for i, s := range stages {
		if s.key == current {
			idx = i
		}
	}
	var out []string
	for i, s := range stages {
		switch {
		case i < idx:
			out = append(out, "  "+styGood.Render("✓")+" "+styFaint.Render(s.label))
		case i == idx:
			out = append(out, "  "+styAccent.Render("▸")+" "+styTitle.Render(s.label))
		default:
			out = append(out, "  "+styFaint.Render("· "+s.label))
		}
	}
	return strings.Join(out, "\n")
}

func stageLabel(status string) string {
	switch status {
	case api.StatusPending:
		return "queued"
	case api.StatusTranscribing:
		return "transcribing"
	case api.StatusAnalyzing:
		return "analyzing"
	case api.StatusComputingSaliency:
		return "computing saliency"
	case api.StatusCompleted:
		return "done"
	case api.StatusFailed:
		return "failed"
	default:
		return strings.ReplaceAll(status, "_", " ")
	}
}
