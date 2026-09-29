package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Upload limits, mirroring the server's own constants. defaultChunkSize is what
// the create call advertises; maxChunkSize is the largest body the append route
// accepts, and a client that ignores it gets a 413 partway through a long
// transfer.
const (
	defaultChunkSize int64 = 8 << 20
	maxChunkSize     int64 = 32 << 20
)

// maxUploadConflicts bounds how many times one upload will re-seek after the
// server reports an offset mismatch. Resuming is the point of the endpoint, but
// a server that keeps disagreeing about the same byte would otherwise spin
// forever; after this many tries the 409 is returned to the caller.
const maxUploadConflicts = 5

// uploadContentTypes maps a file extension to the MIME type the API accepts for
// it.
//
// This is a hard-coded mirror of the API's allow list instead of
// mime.TypeByExtension, for two reasons: the system MIME database is absent on
// some hosts and inconsistent across the others, and the API compares the string
// exactly. A guessed type that is "close" — audio/wave instead of audio/wav — is
// a 400 after the user has waited for the request, so the guess is not worth
// making.
var uploadContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mov":  "video/quicktime",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".avi":  "video/x-msvideo",
	".webm": "video/webm",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".aac":  "audio/aac",
}

// UploadContentType returns the MIME type the API expects for path, and whether
// the extension is one the API accepts at all.
func UploadContentType(path string) (string, bool) {
	ct, ok := uploadContentTypes[strings.ToLower(filepath.Ext(path))]
	return ct, ok
}

// SupportedUploadExtensions lists the extensions the API accepts, sorted, for
// an error message that tells the user what to do instead.
func SupportedUploadExtensions() []string {
	exts := make([]string, 0, len(uploadContentTypes))
	for ext := range uploadContentTypes {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

// Upload sends a video or audio file and returns the upload id that
// POST /api/analyze claims.
//
// The transfer is resumable in the way the endpoint is built for: create, then
// append chunks at a byte offset. When the server's offset has moved past ours
// — a chunk that landed before its response was lost — it answers 409 with the
// offset it actually holds, and the transfer resumes from there instead of
// failing. That is why the caller gets a resumable id and not a URL: a dropped
// connection costs one chunk, not the file.
//
// onProgress, when set, is called after every accepted chunk with the bytes
// accepted so far and the total.
//
// Uploads require an active subscription; without one the create call answers
// 402 SUBSCRIPTION_REQUIRED, which is the error the caller should surface as
// "this is the paid feature".
func (c *Client) Upload(ctx context.Context, path string, onProgress func(sent, total int64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	size := info.Size()
	if size <= 0 {
		return "", fmt.Errorf("%s is empty", path)
	}

	contentType, ok := UploadContentType(path)
	if !ok {
		return "", fmt.Errorf("%s: unsupported file type; the API accepts %s",
			filepath.Ext(path), strings.Join(SupportedUploadExtensions(), ", "))
	}

	status, err := c.createUpload(ctx, filepath.Base(path), size, contentType)
	if err != nil {
		return "", err
	}

	uploadID := status.UploadID
	offset := status.Offset
	if offset < 0 || offset > size {
		offset = 0
	}
	if offset > 0 {
		// A create that reports progress means an earlier attempt got partway
		// through under the same id; continue rather than starting over.
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return "", fmt.Errorf("resume %s at %d: %w", path, offset, err)
		}
	}
	if onProgress != nil {
		onProgress(offset, size)
	}
	if status.Complete {
		return uploadID, nil
	}

	chunk := status.ChunkSize
	if chunk <= 0 {
		chunk = defaultChunkSize
	}
	if chunk > maxChunkSize {
		chunk = maxChunkSize
	}
	buf := make([]byte, chunk)

	conflicts := 0
	for offset < size {
		n, readErr := io.ReadFull(f, buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("read %s: %w", path, readErr)
		}
		if n == 0 {
			break
		}

		next, err := c.appendUpload(ctx, uploadID, offset, buf[:n])
		if err != nil {
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
				serverOffset, ok := uploadOffset(apiErr.RawBody)
				if !ok {
					return "", err
				}
				conflicts++
				if conflicts > maxUploadConflicts {
					return "", err
				}
				offset = serverOffset
				if _, err := f.Seek(offset, io.SeekStart); err != nil {
					return "", fmt.Errorf("resume %s at %d: %w", path, offset, err)
				}
				// The server holds more of the file than the last response
				// said, so the progress the user sees should move with it
				// rather than stand still until the next accepted chunk.
				if onProgress != nil {
					onProgress(offset, size)
				}
				continue
			}
			return "", err
		}

		conflicts = 0
		offset = next.Offset
		if onProgress != nil {
			onProgress(offset, size)
		}
		if next.Complete {
			break
		}
	}

	return uploadID, nil
}

// createUpload declares the file and gets an id (POST /api/uploads).
func (c *Client) createUpload(ctx context.Context, filename string, size int64, contentType string) (*UploadStatus, error) {
	body := struct {
		Filename    string `json:"filename"`
		Size        int64  `json:"size"`
		ContentType string `json:"content_type"`
	}{Filename: filename, Size: size, ContentType: contentType}

	raw, err := c.do(ctx, http.MethodPost, "/api/uploads", mustJSON(body), "application/json")
	if err != nil {
		return nil, err
	}
	var out UploadStatus
	if err := decode(raw, "/api/uploads", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// appendUpload sends one chunk at offset (PATCH /api/uploads/:id?offset=N).
func (c *Client) appendUpload(ctx context.Context, id string, offset int64, chunk []byte) (*UploadStatus, error) {
	path := withQuery("/api/uploads/"+id, map[string]string{"offset": strconv.FormatInt(offset, 10)})

	raw, err := c.do(ctx, http.MethodPatch, path, chunk, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	var out UploadStatus
	if err := decode(raw, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// uploadOffset reads the resume offset out of a 409 body.
//
// This response is the one place the API puts data outside the error envelope:
// `{"success":false,"error":{…},"offset":N}`. The offset is the whole point of
// the 409, so a body without it is treated as an unhandled conflict.
func uploadOffset(body []byte) (int64, bool) {
	var out struct {
		Offset *int64 `json:"offset"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Offset == nil {
		return 0, false
	}
	return *out.Offset, true
}

// mustJSON marshals a request body this package built itself. A failure is a
// programming error, not a runtime condition; there is no error to return to a
// user, so it panics rather than being silently dropped.
func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("api: encode request: %v", err))
	}
	return raw
}
