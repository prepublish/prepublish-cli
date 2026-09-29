# Prepublish CLI

`prepublish` audits a YouTube script before it is recorded. One command returns
the full report the site shows: an overall score with hook, structure and pacing
breakdowns, the attention-risk curve, prioritized rewrites with quotes and
reasons, a title rewrite, the inauthentic-content check, and a YouTube policy
pre-flight. A video or audio file can be uploaded instead of a script and is
transcribed first.

It is a single Go binary with no runtime dependencies. It works with no account
(free, three audits a day, an email required, the report's rewrites gated) and
with one (fifty a day, the whole report, uploads and thumbnails).

- Documentation: <https://prepublish.ai/cli>
- Issues and pull requests: <https://github.com/prepublish/prepublish-cli>

## Install

### macOS and Linux

```sh
curl -fsSL https://prepublish.ai/install.sh | sh
```

The installer picks the archive for your OS and architecture, verifies its
sha256 against the release's `checksums.txt` *before* unpacking anything, and
installs a single binary to `$HOME/.local/bin`. It needs no sudo, and if that
directory is not on your `PATH` it prints the one line to add. Set
`PREPUBLISH_INSTALL_DIR` to install somewhere else.

| Variable | Effect |
| --- | --- |
| `PREPUBLISH_VERSION` | Tag to install, e.g. `v0.1.0`. Default: the latest release. |
| `PREPUBLISH_INSTALL_DIR` | Install directory. Default: `$HOME/.local/bin`. |
| `PREPUBLISH_DOWNLOAD_BASE` | Release base URL. Default: this repository's Releases page; point it at a mirror if you host one. |

Only macOS and Linux on amd64 and arm64 are covered by the installer. On any
other platform it says so and points you at the archives below rather than
guessing.

### With Go

```sh
go install github.com/prepublish/prepublish-cli/cmd/prepublish@latest
```

Installs to `$(go env GOPATH)/bin`. Requires Go 1.26 or newer.

### Windows

Download `prepublish_windows_amd64.zip` (or `_arm64`) from the
[latest release](https://github.com/prepublish/prepublish-cli/releases/latest),
unzip it, and put `prepublish.exe` somewhere on your `PATH`. The zip contains the
binary, the license and this file.

### Pinning a version

```sh
# Installer: the tag goes to the shell, not to curl.
curl -fsSL https://prepublish.ai/install.sh | PREPUBLISH_VERSION=v0.1.0 sh

# Go.
go install github.com/prepublish/prepublish-cli/cmd/prepublish@v0.1.0
```

### Verifying a download yourself

Every release publishes `checksums.txt` next to the archives. To check one by
hand rather than trusting the installer:

```sh
sha256sum --ignore-missing -c checksums.txt      # Linux
shasum -a 256 -c checksums.txt                   # macOS
```

## Quick start

### Free, with no account

```sh
# The first free audit asks for an email and remembers it.
prepublish audit script.md --title "Why Roman concrete lasts"
```

No login, no card, three audits a day, counted per installation rather than per
address so an office network does not spend a colleague's allowance. The free
report includes the scores, the attention-risk curve and the first improvement;
the rest of the rewrites are gated, and signing in later moves your audits onto
the account rather than losing them.

### Signed in

```sh
prepublish login
prepublish audit script.md --title "Why Roman concrete lasts"
prepublish audit interview.mp4 --title "The whole story"   # transcribe a recording
```

Signing in raises the daily limit to fifty, opens the whole report, and enables
video, audio and thumbnail uploads.

Run `prepublish` with no arguments in a terminal and you get the home screen: a
banner, the account and today's quota, and a menu for an audit, the three
standalone tools, history, sign-in and upgrade. Every action returns to the menu
until you quit, so one session can audit a script, check a hook and read a past
report without starting the binary again. Piped or in a script, the bare command
prints help instead and every command works without prompting.

```sh
prepublish runtime script.md    # word count and speaking time, no network at all
```

## Commands

Global flags, valid on every command:

| Flag | Effect |
| --- | --- |
| `--json` | Print the API's own struct as JSON on stdout, never prompt, never open a TUI. Errors become `{"error":{"status","code","message"}}` on stderr. |
| `--api-url URL` | API base URL for this run. Outranks `$PREPUBLISH_API_URL` and the config file. `https` only, or `http` on a loopback address. |
| `--no-input` | Never prompt. A command that would have asked now fails with exit 2. |

### Auditing

| Command | What it does |
| --- | --- |
| `audit [FILE\|-]` | The full audit. `FILE` is a script, or a video/audio file to transcribe (paid); `-` reads stdin; with no argument a terminal is asked for a path. |
| `report ID` | Print a stored report. The id is the share key, so this needs no credential; a full report URL works in place of the id. |
| `history` | List your audits; a terminal gets a browsable list, a pipe gets one line per audit. |
| `runtime [FILE\|-]` | Local word count and speaking time. No network. |

```sh
prepublish audit script.md --title "..."            # script file
cat script.md | prepublish audit - --title "..."    # stdin, for pipelines
prepublish audit script.md --title "..." --open     # open the report in a browser
prepublish audit script.md --title "..." --pager    # open it in the pager regardless
prepublish audit script.md --title "..." --no-wait  # queue it, print id and URL
prepublish audit script.md --title "..." --timeout 5m # stop waiting sooner
prepublish audit interview.mp4 --title "..."        # transcribe a recording (paid)
prepublish audit script.md --title "..." --thumbnail thumb.png --duration 612
prepublish audit script.md --title "..." --json | jq .overall_score
prepublish report 6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f --open
prepublish report https://prepublish.ai/analysis/6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f
prepublish report 6f1c0f5e-6a1e-4a5f-9a5f-3f5b0c1d2e3f --json | jq '.improvements[0].improved'
prepublish history --page 2
prepublish history --json | jq -r '.analyses[] | "\(.id) \(.overall_score)"'
prepublish runtime script.md
prepublish runtime --minutes 10                     # words a 10-minute video needs
```

The report is rendered to the width of the terminal, capped at 100 columns. When
it is taller than the terminal it opens in a scrollable pager (`q` or `esc`
leaves); `--pager` forces that, and piping or `--json` always prints instead.

### The standalone tools

Free, no account needed, each with its own daily allowance — the hook analyzer is
one a day anonymously and three with an email, the other two are three a day. A
full audit carries the authenticity and policy checks inside its report.

| Command | What it does |
| --- | --- |
| `hook [FILE\|-]` | Score a hook, sentence by sentence, with rewrites. |
| `policy [FILE\|-]` | Pre-flight a script against YouTube's advertiser and community guidelines. |
| `authenticity [FILE\|-]` | Check the inauthentic or reused-content risk. |

```sh
prepublish hook --text "Everyone gets this wrong." --niche history
prepublish hook hook.md --email me@example.com
prepublish policy script.md --title "Why Roman concrete lasts"
prepublish authenticity script.md --title "Why Roman concrete lasts"
```

Each takes `--text` instead of a file, and a file, `-` for stdin, or a prompt in
a terminal.

### Account

| Command | What it does |
| --- | --- |
| `login` | Browser sign-in, or `--with-token` to pipe in an API key. |
| `logout` | Revoke the stored key on the server, then delete it locally. |
| `whoami` (alias `status`) | Who the CLI is acting as, the plan tier and today's quota. |
| `upgrade` | Open the pricing page. |
| `billing` | Open the billing portal for the signed-in account. |
| `version` | The version, commit, build date and User-Agent. |
| `config get\|set\|unset\|path` | Read and write the settings file. |

```sh
prepublish login                      # opens the browser, shows a code
prepublish login --no-browser         # prints the code and URL instead
printf '%s' "$KEY" | prepublish login --with-token
prepublish logout
prepublish whoami
prepublish whoami --json
prepublish config set api-url http://localhost:8080
prepublish config get api-url
prepublish config path
```

## Authentication and credential storage

Two ways in, one credential out. Every credential is an API key sent as
`Authorization: Bearer <key>`; there is no JWT, cookie or session file.

**Browser sign-in (`prepublish login`)** is a device-authorization flow, the same
shape as `gh auth login`:

1. The CLI asks the API for a login request and prints a short code such as
   `BCDF-GHJK`, then opens `prepublish.ai/cli/login` with the code already in the
   URL. `--no-browser` and a non-terminal print the URL instead of opening it,
   which is what a remote shell needs.
2. You approve the machine on that page. It requires a signed-in session there,
   so an unknown browser is sent through the sign-in flow first and returned to
   the approval page afterwards.
3. The CLI, which has been polling, receives a new API key, names it
   `CLI · prepublish-cli on <hostname>` so it is identifiable in the dashboard,
   and stores it.

The approval page is the one URL the CLI opens from something a server said, so
it is not trusted blindly: it is only opened when it is `https` and on the same
origin as the configured app URL. Otherwise the CLI builds
`<app-url>/cli/login?code=<code>` itself, and if the code the server sent is not
in the expected shape it says so rather than sending you to a page it cannot
vouch for. Nothing a server sends is printed to the terminal without its control
and bidi characters stripped first.

Polling is patient: the request lives for ten minutes, and a rate limit or a
dropped connection while you are finding your mail or signing in is waited out
rather than treated as a failed login.

The request expires after ten minutes and is single-use. Audits this machine ran
anonymously are moved onto the account at the end of the flow (the API reports
how many), so nothing is stranded under the anonymous id.

**API keys** can also be created by hand in the dashboard
(`prepublish.ai/dashboard`) and piped in with `prepublish login --with-token`, or
supplied per-run through the environment. That is the CI path, and the reason the
CLI never needs a browser to be useful.

**Where it is stored.** `$PREPUBLISH_CONFIG_DIR`, or the user config directory
(`~/.config/prepublish` on Linux, `~/Library/Application Support/prepublish` on
macOS), as two files:

| File | Mode | Contents |
| --- | --- | --- |
| `config.json` | 0600 | `api_url`, `app_url`, `email`, `anonymous_id`. Safe to print or paste into a bug report. |
| `credentials.json` | 0600 | the API key, its id and prefix, the API it belongs to (`api_url`), the account email, how it was obtained, when. |

The directory is created 0700 and both files are written 0600 and atomically
(write, then rename), so an interrupted run cannot leave a half-written file the
next run would parse as garbage. They are separate files on purpose:
`prepublish config path` hands out the settings file, and neither that command
nor a bug report should be able to leak a key.

**The key belongs to the API that issued it.** `credentials.json` records that
API, and the stored key is only sent back to the same origin. `--api-url` and
`$PREPUBLISH_API_URL` decide where a command talks without also deciding where
the key goes: pointed anywhere else, the CLI runs signed out and says so on
stderr, rather than handing your production key to whatever host the flag, a
shell profile or a project's `.envrc` named. `$PREPUBLISH_API_KEY` is the
exception — exporting a key is an explicit pairing with whatever API this command
is aimed at, which is what makes the CLI usable against a staging API in CI.

For the same reason both URLs must be `https`, or `http` on a loopback address
(`localhost`, `127.0.0.1`, `::1`) for a server on this machine. A key sent over
plain http can be read on the way. The check applies to `--api-url`,
`$PREPUBLISH_API_URL`, `$PREPUBLISH_APP_URL`, `config set` and the config file,
so a bad value cannot get in by any of the doors.

The directory itself is checked before it is used: it must be a directory this
user owns, because whoever owns it can replace `credentials.json` inside it. When
no home directory can be determined — no `$HOME` and no `$XDG_CONFIG_HOME`, as in
some cron jobs and containers — the CLI refuses to guess and asks for
`$PREPUBLISH_CONFIG_DIR` rather than falling back to a predictable path under
`/tmp` that another user of the machine could have created first.

**Signed out**, the CLI is a first-class citizen: it generates
`cli:<uuid-v4>` once, stores it as `anonymous_id`, and sends it with every free
audit. The API counts anonymous usage per id as well as per IP, so a shared office
NAT does not spend a colleague's allowance by accident, and a later sign-in can
claim those audits. A free audit requires an email for the follow-up; it is taken
from `--email`, then the config file, then the last sign-in, and asked for once
in a terminal before being saved.

## JSON, exit codes and scripting

`--json` prints the API's own response struct, two-space indented, on stdout, and
suppresses everything interactive: no prompts, no pager, no TUI. Progress lines
and warnings move to stderr, so stdout is always exactly one JSON document. That
includes errors:

```sh
$ prepublish audit script.md --title "..." --json | jq .overall_score
78

$ cat script.md | prepublish audit - --json ; echo "exit $?"
{
  "error": {
    "status": 402,
    "code": "SUBSCRIPTION_REQUIRED",
    "message": "Active subscription required to access this resource"
  }
}
exit 4
```

| Exit code | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | A failure with no more specific code (a missing file, a 404, an unreachable API, an audit that outran `--timeout`). |
| 2 | The command line was wrong: an unknown flag, a missing value, an unknown config key, a report id that is not an id. The message names the flag or the value that fixes it; usage text follows only for a genuine syntax error, where the flag list *is* the answer. |
| 3 | The API rejected the credential or the account: 401, 403, `ACCESS_DENIED`, a denied or expired login. Run `prepublish login`. |
| 4 | A quota or plan limit: 429 `FREE_ANALYSIS_USED`, 402, `NO_SUBSCRIPTION`, `SUBSCRIPTION_REQUIRED`, `email_required`. Upgrade, or wait for the daily reset. |
| 130 | Ctrl-C. The convention shells use for a process stopped by SIGINT; the CLI reports it so a wrapper can tell "the user stopped this" from a real failure. |

Notes for scripts:

- Every command is non-interactive without a terminal: it fails with exit 2 and
  says which flag it wanted, rather than hanging on a prompt.
- `--no-wait` returns as soon as the audit is queued, printing the id and the
  report URL (or the pending struct as JSON). Poll it later with `report ID`.
- `audit` waits fifteen minutes for the report by default (`--timeout`, `0` waits
  forever). A timeout is not a lost audit: it prints the id and the URL and exits
  1, because the work keeps going server-side and `report ID` picks it up.
- `history` prints the full id, so a row can be pasted straight into `report`; the
  title is the column that gives way on a narrow terminal.
- `prepublish audit` re-renders the whole report, so `--json` is the only stable
  parse target. Field names are the API's, mirrored in `internal/api/types.go`.
- A failed audit exits 1 with the server's own explanation, after a `--json` run
  prints the report struct that carries it.

## Configuration and environment variables

Settings live in `config.json` and are read and written with
`prepublish config`; the environment outranks the file, and a flag outranks both.

| Variable | Effect |
| --- | --- |
| `PREPUBLISH_API_KEY` | The credential. Outranks the stored file. `logout` will not touch it: unset it instead. |
| `PREPUBLISH_API_URL` | API base URL. Outranks the config file. `https` only, or `http` on a loopback address. |
| `PREPUBLISH_APP_URL` | Web app base URL, used for every link the CLI opens. `https` only, or `http` on a loopback address. |
| `PREPUBLISH_CONFIG_DIR` | Moves both files, for a sandboxed install or a test. |
| `NO_COLOR` | No ANSI colour, honoured by the whole render path. |

```sh
prepublish config set email me@example.com     # api-url, app-url, email
prepublish config get                          # everything in effect
prepublish config unset email
prepublish config path                         # where the settings file is
```

`config get` prints the environment override when it is the reason the value
disagrees with the file.

## Building from source

```sh
make build          # bin/prepublish, with the version, commit and date stamped in
make test           # go test ./...
make vet            # go vet ./...
make lint           # golangci-lint if installed, otherwise instructions
make release        # a GoReleaser snapshot: dist/, the release's own asset names
make clean          # remove bin/ and dist/
```

`make build` and `make install` stamp the version, commit and build date into the
binary with `-ldflags`; `make release` runs GoReleaser in snapshot mode, producing
the same six archives and `checksums.txt` that a tagged release publishes.
`prepublish version` prints all of it, and a plain `go build` reports `dev`, which
is what a development binary should say.

Run it against a local API and web app:

```sh
PREPUBLISH_API_URL=http://localhost:8080 PREPUBLISH_APP_URL=http://localhost:3000 \
  go run ./cmd/prepublish whoami

# Or pin both for a shell, instead of repeating them.
go run ./cmd/prepublish config set api-url http://localhost:8080
go run ./cmd/prepublish config set app-url http://localhost:3000
go run ./cmd/prepublish config get
```

`internal/auth` reads `PREPUBLISH_APP_URL` for the device-login page, so a local
`/cli/login` page is used when one is configured; against a stack without those
endpoints, `--with-token` and `$PREPUBLISH_API_KEY` are the working sign-in paths.

### Layout

```
cmd/prepublish/main.go     signals, fang, exit code — nothing else
internal/version/          Version, Commit, Date, String(), UserAgent()
internal/config/           settings, credentials, atomic 0600 writes
internal/api/              HTTP client, wire types, typed errors, retries
internal/auth/             device login flow, opening a browser
internal/scriptinfo/       word count and speaking-time estimates
internal/ui/               lipgloss theme and pure, width-aware renderers
internal/tui/              bubbletea programs (progress, login, pager, history, home)
internal/cli/              cobra commands, flags, prompts, exit codes
```

Three layers, each with one job, and the boundaries are load-bearing:

- **`internal/cli` never formats a report and `internal/ui` never does I/O.**
  Renderers take a struct and a width and return a string; the command decides
  whether that string goes to stdout, a pager or nowhere. That is what makes
  `--json`, the pager and the 60-column case one code path each instead of a
  condition sprinkled through the renderers.
- **`internal/api` knows nothing about terminals.** It returns decoded structs
  and `*APIError`, which carries the status, the machine-readable code, the
  message, the `details.field` and any `Retry-After`. Retries live there too:
  GETs retry transport failures and 502/503/504 with 1, 2 and 4 second backoff,
  anything with a body retries only a refused connection or an unresolvable host,
  and a 4xx is never repeated.
- **`internal/tui` is only reachable from a terminal.** Every entry point is
  behind the same check (`stdout` and `stdin` are TTYs, no `--no-input`, no
  `--json`), so bubbletea can never take over a screen a script is reading.

Output goes through lipgloss's colour-profile-aware writers, so `NO_COLOR`, a
pipe, a 16-colour terminal and truecolor are all handled by downsampling in one
place rather than by the commands.

Tests live beside what they cover. The core packages have their own suites
(wire-format fidelity, error parsing, resumable upload, credentials permissions);
`internal/cli` tests the logic a user can be wrong about — the exit-code mapping,
email resolution precedence, media detection, and the "this is a usage error"
cases. `docs/TESTING.md` is the manual checklist for what a terminal shows a
person, and `docs/RELEASING.md` is how a release is cut.

## Contributing

Issues and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
`make test` is the gate. The hosted API and web app are not open source; this
repository is the CLI, the installer and the release tooling.

## Security

Report a vulnerability through <https://prepublish.ai/contact> rather than in a
public issue. [SECURITY.md](SECURITY.md) says what is in scope.

## License

MIT — see [LICENSE](LICENSE).
