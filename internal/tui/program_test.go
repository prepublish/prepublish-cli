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

	"github.com/prepublish/prepublish-cli/internal/api"
)

// These tests run the real programs — renderer, event loop and interrupt
// handling included — with input disabled and output captured, which is the only
// way to cover the wiring the model tests cannot reach. Nothing here sleeps to
// wait for a frame: Bubble Tea renders the final view on a graceful shutdown, so
// the assertions are on the end state, which is deterministic.
func runProgram(t *testing.T, m tea.Model) (tea.Model, error, string) {
	t.Helper()
	var out bytes.Buffer
	p := tea.NewProgram(m,
		tea.WithContext(context.Background()),
		tea.WithInput(nil),
		tea.WithOutput(&out),
		tea.WithWindowSize(100, 30),
	)
	final, err := p.Run()
	return final, err, out.String()
}

func TestProgramRunsAnAuditToCompletion(t *testing.T) {
	runner := newWorkRunner(context.Background())
	m := auditModel{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), runner: runner, started: time.Now()}
	result := &api.Analysis{VideoTitle: "smoke draft", Status: api.StatusCompleted, Progress: 100, OverallScore: 74}

	// The events are queued before the program starts; it drains them in order
	// and quits on the terminal one.
	runner.send(workEvent{analysis: &api.Analysis{VideoTitle: "smoke draft", Status: api.StatusAnalyzing, Progress: 40}})
	runner.send(workEvent{done: true, result: result})

	final, err, frame := runProgram(t, m)
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	got, ok := final.(auditModel)
	if !ok || got.result == nil || got.result.OverallScore != 74 {
		t.Fatalf("the finished analysis must come back out of the program: %#v", final)
	}
	for _, want := range []string{"smoke draft", "done", "100%", "queued"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the rendered frame should contain %q:\n%s", want, frame)
		}
	}
	if !strings.Contains(frame, "\x1b[?1049h") {
		t.Error("the audit screen should use the alternate screen")
	}
}

func TestProgramRunsAnUploadToCompletion(t *testing.T) {
	runner := newWorkRunner(context.Background())
	m := uploadModel{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), runner: runner, started: time.Now()}
	const total = 8 << 20
	runner.send(workEvent{upload: &uploadEvent{sent: total / 2, total: total}})
	runner.send(workEvent{done: true, uploadID: "up_smoke"})

	final, err, frame := runProgram(t, m)
	if err != nil {
		t.Fatalf("program: %v", err)
	}
	got, ok := final.(uploadModel)
	if !ok || got.uploadID != "up_smoke" {
		t.Fatalf("the upload id must come back out of the program: %#v", final)
	}
	for _, want := range []string{"uploading", "8 MB", "100%"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the rendered frame should contain %q:\n%s", want, frame)
		}
	}
}

// TestProgramInterruptIsCancellation is the promise every Run… makes: quitting
// is not a failure, and a command can tell it apart from an API error.
func TestProgramInterruptIsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out bytes.Buffer
	m := auditModel{spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)), runner: newWorkRunner(ctx), started: time.Now()}
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(nil),
		tea.WithOutput(&out),
		tea.WithWindowSize(100, 30),
	)
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.InterruptMsg{})
	}()

	_, err := p.Run()
	if err == nil {
		t.Fatal("an interrupted program must return an error")
	}
	if mapped := cancelled(ctx, err); !errors.Is(mapped, context.Canceled) {
		t.Fatalf("an interrupt must map to context.Canceled, got %v", mapped)
	}
}
