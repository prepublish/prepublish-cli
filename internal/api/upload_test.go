package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeUploadServer is a minimal resumable-upload endpoint: it stores bytes at
// the offset the client declares, reports a chunk size small enough to force
// several requests, and answers an offset mismatch with the 409 body the real
// handler produces.
type fakeUploadServer struct {
	mu        sync.Mutex
	size      int64
	chunkSize int64
	// offset is the server's true byte count. A 409 reports it.
	offset int64
	// received records every accepted chunk as "offset:body".
	received []string
	// jumpAfter simulates a chunk whose response was lost: the server accepted
	// bytes the client was never told about, so its true offset moves past what
	// the client believes without the response saying so.
	jumpAfter map[int64]int64
	// alwaysStale makes every append conflict, with the offset moving each
	// time, which no client can ever satisfy.
	alwaysStale bool
}

func (f *fakeUploadServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/uploads":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"upload_id":"up-1","offset":0,"size":%d,"complete":false,"chunk_size":%d}`, f.size, f.chunkSize)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/uploads/up-1":
			at, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, "bad offset")
				return
			}
			body, _ := io.ReadAll(r.Body)

			if f.alwaysStale || at != f.offset {
				if f.alwaysStale {
					f.offset++
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				fmt.Fprintf(w, `{"success":false,"error":{"code":"VALIDATION_ERROR","message":"Offset does not match the server's current offset","details":{"field":"offset"}},"offset":%d}`, f.offset)
				return
			}

			f.received = append(f.received, fmt.Sprintf("%d:%s", at, body))
			reported := at + int64(len(body))
			if jump, ok := f.jumpAfter[at]; ok {
				// The bytes between reported and jump are on the server from an
				// earlier attempt; this response only describes this chunk.
				f.offset = jump
				delete(f.jumpAfter, at)
			} else {
				f.offset = reported
			}

			fmt.Fprintf(w, `{"upload_id":"up-1","offset":%d,"size":%d,"complete":%t,"chunk_size":%d}`,
				reported, f.size, f.offset >= f.size, f.chunkSize)

		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "not found")
		}
	})
}

func TestUploadChunksAndResumesAfterOffsetConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	content := "0123456789abcdefghij" // 20 bytes, five 4-byte chunks
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fake := &fakeUploadServer{
		size:      int64(len(content)),
		chunkSize: 4,
		// The first chunk lands, and the server also holds bytes 4..8 from an
		// attempt whose response never arrived. The client still believes the
		// offset is 4.
		jumpAfter: map[int64]int64{0: 8},
	}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	var (
		progressSent []int64
		progressTot  []int64
	)
	id, err := New(srv.URL, "pp_live_test", "").Upload(context.Background(), path, func(sent, total int64) {
		progressSent = append(progressSent, sent)
		progressTot = append(progressTot, total)
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if id != "up-1" {
		t.Errorf("upload id = %q, want up-1", id)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()

	// After the 409 the client must re-seek to the server's offset and continue
	// from there: the chunk it would have sent at 4 is already on the server,
	// and resending it would corrupt the file.
	want := []string{"0:0123", "8:89ab", "12:cdef", "16:ghij"}
	if fmt.Sprint(fake.received) != fmt.Sprint(want) {
		t.Errorf("received chunks = %v, want %v", fake.received, want)
	}
	if fake.offset != int64(len(content)) {
		t.Errorf("server offset = %d, want the whole file", fake.offset)
	}

	// Progress is reported from the server's acknowledgements, so it jumps with
	// the resume and still ends exactly at the file size.
	wantProgress := []int64{0, 4, 8, 12, 16, 20}
	if fmt.Sprint(progressSent) != fmt.Sprint(wantProgress) {
		t.Errorf("progress = %v, want %v", progressSent, wantProgress)
	}
	for _, total := range progressTot {
		if total != int64(len(content)) {
			t.Errorf("progress total = %d, want %d", total, len(content))
		}
	}
}

func TestUploadSendsChunksAtTheAdvertisedSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	content := strings.Repeat("x", 10) // 10 bytes, three chunks at 4
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	fake := &fakeUploadServer{size: int64(len(content)), chunkSize: 4}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	if _, err := New(srv.URL, "", "").Upload(context.Background(), path, nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	want := []string{"0:xxxx", "4:xxxx", "8:xx"}
	if fmt.Sprint(fake.received) != fmt.Sprint(want) {
		t.Errorf("received chunks = %v, want %v (the last chunk is the remainder)", fake.received, want)
	}
}

func TestUploadGivesUpAfterRepeatedOffsetConflicts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("0123456789abcdefghij"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// A server that keeps moving its offset can never be satisfied. Resuming is
	// the point of the endpoint, but an unbounded loop is not.
	fake := &fakeUploadServer{size: 20, chunkSize: 4, alwaysStale: true}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	_, err := New(srv.URL, "", "").Upload(context.Background(), path, nil)
	if StatusOf(err) != http.StatusConflict {
		t.Fatalf("err = %v, want the 409 once retrying stops helping", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.received) != 0 {
		t.Errorf("server accepted %v, want nothing", fake.received)
	}
}

func TestUploadRejectsUnsupportedFileTypeBeforeSending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.txt")
	if err := os.WriteFile(path, []byte("words"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	_, err := New(srv.URL, "", "").Upload(context.Background(), path, nil)
	if err == nil {
		t.Fatal("Upload accepted a .txt file")
	}
	if !strings.Contains(err.Error(), ".mp4") {
		t.Errorf("err = %v, want the supported extensions listed", err)
	}
	if calls != 0 {
		t.Errorf("server saw %d calls, want 0: the type must be rejected before a 250 MB transfer starts", calls)
	}
}

func TestUploadRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.mp4")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := New("http://127.0.0.1:1", "", "").Upload(context.Background(), path, nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want an empty-file error", err)
	}
}

func TestUploadContentTypeMirrorsServerAllowList(t *testing.T) {
	cases := map[string]string{
		"a.mp4":  "video/mp4",
		"a.MOV":  "video/quicktime",
		"a.webm": "video/webm",
		"a.mp3":  "audio/mpeg",
		"a.wav":  "audio/wav",
		"a.AAC":  "audio/aac",
	}
	for name, want := range cases {
		got, ok := UploadContentType(name)
		if !ok || got != want {
			t.Errorf("UploadContentType(%q) = (%q, %t), want (%q, true)", name, got, ok, want)
		}
	}
	if _, ok := UploadContentType("a.m4a"); ok {
		// The server's allow list has no audio/mp4, so accepting the extension
		// here would only move the failure to the network.
		t.Error("UploadContentType accepted .m4a, which the server rejects")
	}
}
