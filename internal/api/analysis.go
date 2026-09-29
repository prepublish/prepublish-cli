package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// defaultPollInterval is what WaitAnalysis uses when the caller has no opinion.
// The API's own SSE stream pushes changes as they happen; polling at this rate
// is documented as equivalent, and it is the interval the MCP server uses.
const defaultPollInterval = 3 * time.Second

// Analyze starts an audit (POST /api/analyze) and returns the pending report.
// The response is a 202 with status "pending"; poll it with WaitAnalysis or
// GetAnalysis.
//
// With thumbnailPath empty the request is JSON. With a thumbnail it is
// multipart, because the API takes the image as a file part and rejects a `file`
// part by name: a video or audio file must go through Upload first.
func (c *Client) Analyze(ctx context.Context, req AnalyzeRequest, thumbnailPath string) (*Analysis, error) {
	var (
		raw []byte
		err error
	)
	if thumbnailPath == "" {
		raw, err = c.analyzeJSON(ctx, req)
	} else {
		raw, err = c.analyzeMultipart(ctx, req, thumbnailPath)
	}
	if err != nil {
		return nil, err
	}

	var out Analysis
	if err := decode(raw, "/api/analyze", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) analyzeJSON(ctx context.Context, req AnalyzeRequest) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode analyze request: %w", err)
	}
	return c.do(ctx, http.MethodPost, "/api/analyze", payload, "application/json")
}

// analyzeMultipart sends the same fields as form values plus the thumbnail.
// video_duration is omitted rather than sent as zero: the server distinguishes
// "unknown" from "zero seconds" and a script-only audit should stay unknown.
func (c *Client) analyzeMultipart(ctx context.Context, req AnalyzeRequest, thumbnailPath string) ([]byte, error) {
	f, err := os.Open(thumbnailPath)
	if err != nil {
		return nil, fmt.Errorf("open thumbnail: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fields := []struct{ name, value string }{
		{"video_title", req.VideoTitle},
		{"script_text", req.ScriptText},
		{"anonymous_user_id", req.AnonymousUserID},
		{"category", req.Category},
		{"audience", req.Audience},
		{"email", req.Email},
		{"upload_id", req.UploadID},
	}
	if req.VideoDuration != nil {
		fields = append(fields, struct{ name, value string }{
			"video_duration", strconv.Itoa(*req.VideoDuration),
		})
	}
	for _, fld := range fields {
		if fld.value == "" {
			continue
		}
		if err := w.WriteField(fld.name, fld.value); err != nil {
			return nil, fmt.Errorf("encode analyze form field %s: %w", fld.name, err)
		}
	}

	part, err := w.CreateFormFile("thumbnail", filepath.Base(thumbnailPath))
	if err != nil {
		return nil, fmt.Errorf("encode thumbnail part: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("read thumbnail: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finish analyze request: %w", err)
	}

	return c.do(ctx, http.MethodPost, "/api/analyze", body.Bytes(), w.FormDataContentType())
}

// GetAnalysis fetches one report (GET /api/analysis/:id).
//
// The UUID is the share key, so no credential is needed to read a report.
// The CLI deliberately does not send ?view=1: that stamps a human view and
// fires a conversion, which a poll or a re-read is not.
func (c *Client) GetAnalysis(ctx context.Context, id string) (*Analysis, error) {
	var out Analysis
	if err := c.getJSON(ctx, "/api/analysis/"+id, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitAnalysis polls one audit until it reaches a terminal status, and returns
// the finished report.
//
// A failed audit returns (analysis, nil): the request succeeded and the report
// is the answer, with Status "failed" and ErrorMessage set. Callers that want
// an error should check the status; that keeps a failed audit renderable
// instead of replacing the server's explanation with a generic one. Only
// transport, API and context failures come back as errors.
//
// interval <= 0 means defaultPollInterval. onUpdate, when set, is called after
// every poll, including the last, so a UI can render progress without its own
// timer.
func (c *Client) WaitAnalysis(ctx context.Context, id string, interval time.Duration, onUpdate func(*Analysis)) (*Analysis, error) {
	if interval <= 0 {
		interval = defaultPollInterval
	}

	for {
		analysis, err := c.GetAnalysis(ctx, id)
		if err != nil {
			return nil, err
		}
		if onUpdate != nil {
			onUpdate(analysis)
		}
		if analysis.Terminal() {
			return analysis, nil
		}

		if err := sleepCtx(ctx, interval); err != nil {
			return nil, err
		}
	}
}

// ListAnalyses returns a page of the signed-in account's audits
// (GET /api/user/analyses). perPage is clamped to 50, the server's maximum.
func (c *Client) ListAnalyses(ctx context.Context, page, perPage int) (*AnalysisPage, error) {
	if perPage > 50 {
		perPage = 50
	}
	path := withQuery("/api/user/analyses", map[string]string{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(perPage),
	})

	var out AnalysisPage
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
