package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// RunUploadProgress shows a resumable upload while it runs: bytes sent against
// the total, a bar, the throughput, and the elapsed time. upload is called once,
// on its own goroutine, and reports progress through onProgress.
//
// It returns the upload id, or context.Canceled when the user interrupted it
// (the server keeps whatever chunks arrived, so a cancelled upload can be
// resumed).
func RunUploadProgress(ctx context.Context, upload func(ctx context.Context, onProgress func(sent, total int64)) (string, error)) (string, error) {
	if upload == nil {
		return "", context.Canceled
	}
	runner := newWorkRunner(ctx)
	go func() {
		id, err := upload(runner.ctx, func(sent, total int64) {
			runner.send(workEvent{upload: &uploadEvent{sent: sent, total: total}})
		})
		runner.send(workEvent{done: true, uploadID: id, err: err})
	}()

	m := uploadModel{
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		runner:  runner,
		started: time.Now(),
	}
	final, err := run(ctx, m)
	if err != nil {
		runner.stop()
		return "", err
	}
	runner.stop()
	um, ok := final.(uploadModel)
	if !ok || um.cancelled {
		return "", context.Canceled
	}
	return um.uploadID, um.err
}

// uploadEvent is the byte counter an upload reports.
type uploadEvent struct {
	sent  int64
	total int64
}

type uploadModel struct {
	spinner spinner.Model
	runner  *workRunner

	started   time.Time
	elapsed   time.Duration
	sent      int64
	total     int64
	uploadID  string
	err       error
	cancelled bool
	width     int
}

func (m uploadModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runner.wait())
}

func (m uploadModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case workEvent:
		if msg.upload != nil {
			m.sent, m.total = msg.upload.sent, msg.upload.total
		}
		if msg.done {
			m.uploadID, m.err = msg.uploadID, msg.err
			if m.total > 0 {
				m.sent = m.total
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

func (m uploadModel) View() tea.View {
	pct := 0.0
	if m.total > 0 {
		pct = float64(m.sent) / float64(m.total)
	}
	elapsed := m.elapsed
	if elapsed == 0 {
		elapsed = time.Since(m.started)
	}

	var b strings.Builder
	b.WriteString(m.spinner.View() + " " + styAccent.Render("uploading") +
		"  " + styDim.Render(elapsed.Round(time.Second).String()) + "\n\n")
	b.WriteString("  " + bar(pct, 48) + "  " + styDim.Render(percent(pct)) + "\n\n")

	line := "  " + styDim.Render(byteSize(m.sent)+" of "+byteSize(m.total))
	if rate := throughput(m.sent, elapsed); rate != "" {
		line += styFaint.Render(" · " + rate)
	}
	b.WriteString(line + "\n")
	b.WriteString("\n" + hint("ctrl+c", "cancel"))
	b.WriteString("\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, int(pct*100))
	return v
}

// byteSize formats a byte count the way a file manager does, which is the unit
// the user picked the file in.
func byteSize(n int64) string {
	const unit = 1024
	if n < unit {
		return itoa(int(n)) + " B"
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB", "TB"} {
		value /= unit
		if value < unit {
			return trimFloat(value) + " " + suffix
		}
	}
	return trimFloat(value) + " PB"
}

// trimFloat keeps one decimal unless the value is whole, so "1 MB" does not
// render as "1.0 MB".
func trimFloat(v float64) string {
	whole := int(v)
	if v == float64(whole) {
		return itoa(whole)
	}
	return itoa(whole) + "." + itoa(int((v-float64(whole))*10))
}

func percent(f float64) string {
	return itoa(int(f*100+0.5)) + "%"
}

// throughput is the average rate since the upload started; an average is the
// honest number here, because the instantaneous rate on a resumable upload
// swings with every chunk boundary.
func throughput(sent int64, d time.Duration) string {
	seconds := d.Seconds()
	if sent <= 0 || seconds < 1 {
		return ""
	}
	perSecond := float64(sent) / seconds
	if perSecond < 1 {
		return ""
	}
	return byteSize(int64(perSecond)) + "/s"
}
