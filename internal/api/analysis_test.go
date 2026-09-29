package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestWaitAnalysisPollsToCompletion(t *testing.T) {
	statuses := []string{
		`{"id":"a-1","status":"pending","progress":0,"overall_score":0,"retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-01-01T00:00:00Z"}`,
		`{"id":"a-1","status":"analyzing","progress":40,"overall_score":0,"retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-01-01T00:00:00Z"}`,
		`{"id":"a-1","status":"completed","progress":100,"overall_score":78,"retention_curve":[],"improvements":[],"one_key_improvement":"Cut the intro.","ready_to_record":true,"created_at":"2026-01-01T00:00:00Z"}`,
	}

	var (
		mu    sync.Mutex
		calls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		body := statuses[len(statuses)-1]
		if calls < len(statuses) {
			body = statuses[calls]
		}
		calls++
		io.WriteString(w, body)
	}))
	defer srv.Close()

	var updates []int
	analysis, err := New(srv.URL, "", "").WaitAnalysis(context.Background(), "a-1", time.Millisecond, func(a *Analysis) {
		updates = append(updates, a.Progress)
	})
	if err != nil {
		t.Fatalf("WaitAnalysis: %v", err)
	}

	if analysis.Status != StatusCompleted || analysis.OverallScore != 78 {
		t.Errorf("analysis = %+v, want a completed report", analysis)
	}
	if !analysis.Terminal() {
		t.Error("a completed analysis should be terminal")
	}
	// The callback fires on every poll including the last, so a UI can render
	// the finished state without a second fetch.
	want := []int{0, 40, 100}
	if fmt.Sprint(updates) != fmt.Sprint(want) {
		t.Errorf("updates = %v, want %v", updates, want)
	}
	if calls != 3 {
		t.Errorf("server saw %d polls, want 3", calls)
	}
}

func TestWaitAnalysisReturnsFailedRunWithoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"a-2","status":"failed","progress":28,"error_message":"transcription failed: unsupported codec","retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	analysis, err := New(srv.URL, "", "").WaitAnalysis(context.Background(), "a-2", time.Millisecond, nil)
	if err != nil {
		// The request succeeded; the audit is what failed. Returning the
		// report lets the caller show the server's explanation instead of a
		// generic "the audit failed".
		t.Fatalf("WaitAnalysis returned an error for a failed audit: %v", err)
	}
	if analysis.Status != StatusFailed {
		t.Errorf("Status = %q, want failed", analysis.Status)
	}
	if analysis.ErrorMessage == nil || *analysis.ErrorMessage == "" {
		t.Error("ErrorMessage was dropped; it is the only explanation of the failure")
	}
}

func TestWaitAnalysisStopsOnContextCancel(t *testing.T) {
	var mu sync.Mutex
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		io.WriteString(w, `{"id":"a-3","status":"analyzing","progress":40,"retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-01-01T00:00:00Z"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := New(srv.URL, "", "").WaitAnalysis(ctx, "a-3", time.Millisecond, func(*Analysis) {
		// Cancel from inside the first update, which is where a Ctrl-C in a
		// progress UI actually lands.
		cancel()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("server saw %d polls, want 1: cancellation must stop the loop", calls)
	}
}

func TestGetAnalysisSurfacesNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error":{"code":"NOT_FOUND","message":"Analysis not found"}}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "", "").GetAnalysis(context.Background(), "missing")
	if !IsCode(err, "NOT_FOUND") {
		t.Fatalf("err = %v, want NOT_FOUND", err)
	}
	if StatusOf(err) != http.StatusNotFound {
		t.Errorf("StatusOf = %d, want 404", StatusOf(err))
	}
}

func TestWaitAnalysisStopsWhenContextAlreadyCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the server was called with a cancelled context")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(srv.URL, "", "").WaitAnalysis(ctx, "a-4", time.Millisecond, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
