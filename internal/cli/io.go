package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/ui"
)

// Layout defaults. Width is capped because a report is prose and a 200-column
// line is unreadable; 60 columns is the narrowest terminal the renderers are
// expected to survive, and below that they wrap rather than truncate.
const (
	defaultWidth  = 80
	maxWidth      = 100
	defaultHeight = 24

	// stdinLimit caps how much of stdin a command will read. The API accepts
	// scripts up to 250k characters, so anything larger is a mistake (a
	// redirected file that is not a script) rather than a request to forward.
	stdinLimit = 8 << 20
)

// stdoutIsTTY and stdinIsTTY are the two ends of the terminal question. They
// are separate because a pipe on stdin (a heredoc, a `cat |`) is a reason not
// to prompt, while stdout being a pipe is a reason not to draw.
func stdoutIsTTY() bool { return term.IsTerminal(os.Stdout.Fd()) }

func stdinIsTTY() bool { return term.IsTerminal(os.Stdin.Fd()) }

// terminalSize returns the terminal's size when there is one to measure.
func terminalSize() (width, height int, ok bool) {
	w, h, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 0, 0, false
	}
	if h <= 0 {
		h = defaultHeight
	}
	return w, h, true
}

// hostName names this machine in the login client name. It is best-effort: a
// container may not have one, and "this machine" is still a useful label in the
// dashboard's key list.
func hostName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "this machine"
	}
	return strings.TrimSpace(name)
}

// result prints a command's payload. It goes to stdout, because that is what a
// pipe captures.
//
// Printing goes through lipgloss so NO_COLOR, a pipe and a 256-colour terminal
// all get the right escape sequences without every command checking.
func (a *app) result(s string) {
	_, _ = lipgloss.Fprintln(os.Stdout, s)
}

// say prints progress and confirmations: text a human reads while the command
// works. In machine mode it moves to stderr so stdout stays parseable JSON.
func (a *app) say(s string) {
	if a.jsonMode {
		_, _ = lipgloss.Fprintln(os.Stderr, s)
		return
	}
	_, _ = lipgloss.Fprintln(os.Stdout, s)
}

// warn prints something the user should notice even when stdout is being
// captured. It always goes to stderr.
func (a *app) warn(s string) {
	_, _ = lipgloss.Fprintln(os.Stderr, s)
}

// printJSON writes a value as the machine-readable result: stdout, two-space
// indent, one object. The API's own structs are printed unchanged, so a script
// sees exactly what the API returned.
func (a *app) printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

// readScript reads a script from path, where "-" means stdin.
//
// Failure messages name the path, because "no such file or directory" without
// one is the least useful error a CLI can print.
func readScript(path string, stdin io.Reader) (string, error) {
	if path == "-" {
		data, err := io.ReadAll(io.LimitReader(stdin, stdinLimit))
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%s: no such file", path)
		}
		if errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("%s: permission denied", path)
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// mediaExtensions are the containers that hold a recording rather than a
// script. A path with one of these is uploaded for transcription instead of
// being read as text.
//
// The list is deliberately wider than what the API can transcribe: .mkv, .m4a
// and .flac are common exports, and telling the user "this is a video, and the
// API cannot take that container" is a better answer than reading binary as a
// script.
var mediaExtensions = map[string]bool{
	".mp4": true, ".mov": true, ".m4v": true, ".webm": true,
	".mkv": true, ".avi": true,
	".mp3": true, ".wav": true, ".m4a": true, ".aac": true,
	".ogg": true, ".flac": true,
}

// isMediaFile reports whether path names a video or audio file. "-" (stdin) is
// never media: it is text by definition.
func isMediaFile(path string) bool {
	if path == "" || path == "-" {
		return false
	}
	return mediaExtensions[strings.ToLower(filepath.Ext(path))]
}

// pickEmail resolves an address from the three places one can come from, in
// precedence order: the flag, then the config file, then the last login.
//
// It is pure so the precedence can be tested without a terminal: the prompting
// wrapper is emailFor.
func pickEmail(flagEmail, cfgEmail, credsEmail string) string {
	for _, candidate := range []string{flagEmail, cfgEmail, credsEmail} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return ""
}

// tallerThanScreen reports whether s would scroll past the terminal, which is
// the cue to open the pager instead of dumping it.
func tallerThanScreen(s string, height int) bool {
	if height <= 4 {
		height = defaultHeight
	}
	// One line is left for the prompt, the viewer chrome takes the rest.
	return strings.Count(s, "\n")+1 > height-2
}

// firstNonEmpty returns the first value with something in it, for the several
// places a name or address can come from.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// argsWantJSON reads --json out of a raw argument list, before cobra has parsed
// anything.
//
// It exists for the error handler: a mistyped flag fails parsing, and the
// failure still has to be reported in the format the user asked for.
func argsWantJSON(args []string) bool {
	want := false
	for _, arg := range args {
		switch {
		case arg == "--json" || arg == "--json=true":
			want = true
		case arg == "--json=false":
			want = false
		}
	}
	return want
}

// statusLabel names a status for a plain progress line. The API's statuses are
// machine words; these are what the stage means to someone waiting.
func statusLabel(status string) string {
	switch status {
	case "pending":
		return "queued"
	case "transcribing":
		return "transcribing the recording"
	case "analyzing":
		return "analyzing the script"
	case "computing_saliency":
		return "mapping attention risk"
	case "completed":
		return "done"
	case "failed":
		return "failed"
	default:
		return status
	}
}

// analysisProgress returns a callback that prints one plain line per change of
// status or percentage. It is the non-interactive twin of the audit progress
// TUI: the same information, no redraws, one line each, on stderr so a piped
// report on stdout stays clean.
func (a *app) analysisProgress() func(*api.Analysis) {
	last := ""
	return func(analysis *api.Analysis) {
		line := statusLabel(analysis.Status)
		if analysis.Progress > 0 {
			line = fmt.Sprintf("%s · %d%%", line, analysis.Progress)
		}
		if line == last {
			return
		}
		last = line
		a.warn(ui.Info(line))
	}
}

// uploadProgress is the same idea for a resumable upload, throttled to whole
// percentages so a fast local transfer does not print hundreds of lines.
func (a *app) uploadProgress(name string) func(sent, total int64) {
	last := -1
	return func(sent, total int64) {
		percent := 0
		if total > 0 {
			percent = int(sent * 100 / total)
		}
		if percent == last {
			return
		}
		last = percent
		a.warn(ui.Info(fmt.Sprintf("uploading %s · %d%%", name, percent)))
	}
}
