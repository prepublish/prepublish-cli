# Changelog

User-visible changes to the Prepublish CLI, newest first. The heading version
matches the `vX.Y.Z` tag the release is built from.

## Unreleased

### New

- **A one-line installer.** `curl -fsSL https://prepublish.ai/install.sh | sh`
  installs the binary for macOS or Linux on amd64 or arm64. It verifies the
  archive against the release's `checksums.txt` before unpacking anything, needs
  no sudo, and prints the one line to add when the install directory is not on
  `PATH`. `PREPUBLISH_VERSION` pins a tag, `PREPUBLISH_INSTALL_DIR` moves the
  destination, and an unsupported platform is told so and pointed at the release
  page or `go install` instead of being handed a binary that cannot run. Windows
  keeps the zip from the release page.
- **Releases are built by GoReleaser** from a tagged commit: the six archives —
  each containing the binary, the license and this README — a `checksums.txt`
  covering all of them, and `install.sh` attached to the release itself. Asset
  names carry no version, which is what keeps
  `.../releases/latest/download/<name>` and the `/install.sh` redirect working.
  `make release` builds the same set locally as a snapshot, so there is one
  packaging path rather than two.
- **The CLI is MIT licensed**, with `LICENSE` included in every archive, plus
  `SECURITY.md` and `CONTRIBUTING.md` for people arriving from GitHub and
  `docs/RELEASING.md` for the release steps.

### Fixed

- **The Windows archives build.** The config directory ownership check reached
  for a Unix-only `syscall.Stat_t`, so the Windows targets did not compile at
  all. The check is now implemented per platform, and is still skipped where the
  platform does not report an owner — there the 0700 directory and 0600 files
  are the whole defence, as before. Behaviour on macOS and Linux is unchanged.

## 0.1.0 (2026-09-29)

The first release. `prepublish` audits a YouTube script before it is recorded,
and does it either anonymously or as a signed-in account.

### New

- **`prepublish audit`.** Send a script and get the report the site shows: the
  overall score with hook, structure and pacing breakdowns, the attention-risk
  curve, one key improvement and the prioritized list, the title rewrite, the
  authenticity section and the policy pre-flight. The report is drawn to the
  terminal — capped at 100 columns, readable at 60 — and opens in a pager when it
  is taller than the screen. `--open` opens the web report instead, `--no-wait`
  queues the audit and prints its id and URL, and `--timeout` (fifteen minutes by
  default, `0` for none) bounds the wait: a timeout prints the id and the URL and
  exits 1, because the audit keeps working server-side and `report` picks it up.
- **`prepublish report`.** Print any stored report — by id or by the report URL
  the CLI and the web app both print — in the terminal or in the pager. The id is
  the share key, so reading a report needs no credential, and a malformed one is
  rejected before any request is made.
- **Video and audio audits.** A `.mp4`, `.mov`, `.mkv`, `.mp3` and friends are
  uploaded resumably and transcribed before the audit runs, with a progress bar
  while the bytes go up. This is the paid path; the CLI says so and names the
  command that fixes it when the account cannot use it.
- **`prepublish` with no arguments is a home screen.** A banner, the account and
  today's quota, and a menu: audit a script, check a hook, run the policy
  pre-flight, check authenticity, browse history, sign in or out, upgrade, quit.
  Every action returns to the menu, so a session can do several things without
  restarting the binary. Without a terminal it prints help and exits 0.
- **Browser sign-in.** `prepublish login` shows a short code, opens the approval
  page and waits; the approval mints an API key named after the machine, which is
  stored at mode 0600. Audits this machine ran anonymously are claimed by the
  account at the end, and the account card shows what changed.
- **`prepublish login --with-token`.** Pipe in an API key from the dashboard. The
  key is verified before it is stored, so a bad paste leaves the previous sign-in
  intact. This is the path for CI and for a machine with no browser.
- **Anonymous use is a real mode.** No account, three audits a day, `cli:<uuid>`
  generated once and stored so usage is counted per installation and stays
  claimable by a later login. Free audits ask for an email once and remember it.
- **`prepublish logout` revokes the key** on the server before deleting it
  locally, so the dashboard does not keep a live key for a machine that is done
  with it. A key that is already revoked is not an error. A credential from
  `$PREPUBLISH_API_KEY` is explained rather than silently ignored.
- **`prepublish whoami`** (alias `status`) reports the account, its plan tier,
  today's audit quota and whether the key came from the environment or the file;
  signed out, it reports the anonymous allowance. `prepublish upgrade` and
  `prepublish billing` open the pricing page and the billing portal.
- **`prepublish history`** lists past audits: a browsable list in a terminal where
  enter opens a report in the pager, one line per audit when piped. The id column
  is the full id, so a row can be pasted straight into `report`.
- **`prepublish runtime`** counts words and speaking time locally, at a measured,
  typical and energetic pace, or with `--minutes` reports the word counts a video
  of that length needs. No network, no account.
- **`prepublish hook`, `policy`, `authenticity`.** The three standalone free
  tools, each taking a file, stdin, `--text`, or a prompt in a terminal.
- **`--json` everywhere.** Prints the API's own struct, two-space indented, with
  progress and warnings moved to stderr, and errors as
  `{"error":{"status","code","message"}}`. Nothing prompts and nothing takes over
  the screen in machine mode.
- **Documented exit codes.** 0 success, 2 usage, 3 sign-in needed or denied,
  4 quota or plan limit, 1 anything else, 130 Ctrl-C — so a script can branch on
  what to do next instead of parsing prose.
- **`prepublish config get|set|unset|path`** for `api-url`, `app-url` and
  `email`, with the environment overrides surfaced when they are the reason a
  `get` disagrees with the file.
- **`--api-url`, `--no-input` and `--json`** as global flags, plus
  `$PREPUBLISH_API_URL`, `$PREPUBLISH_APP_URL`, `$PREPUBLISH_API_KEY` and
  `$PREPUBLISH_CONFIG_DIR`.

### Security

Sign-in hardening, all of it in response to a review of the device-login path:

- **The stored key is bound to the API that issued it.** `credentials.json` now
  records `api_url`, and the key is only sent back to the same origin. Pointed at
  any other API — by `--api-url`, by `$PREPUBLISH_API_URL`, by a shell profile or
  by a project's `.envrc` — the CLI runs signed out and says so on stderr instead
  of handing a production key to that host. Credentials written before this
  version are treated as belonging to `https://api.prepublish.ai`, which is the
  only API that existed when they were saved. `$PREPUBLISH_API_KEY` is unchanged:
  exporting a key pairs it with whatever API the command is aimed at, which is
  what makes the CLI usable against staging in CI.
- **Plain `http` is refused for both base URLs**, except on a loopback address
  (`localhost`, `127.0.0.1`, `::1`) for a server on this machine. The check covers
  `--api-url`, `$PREPUBLISH_API_URL`, `$PREPUBLISH_APP_URL`, `config set` and the
  config file (exit 2, or exit 1 for a hand-edited file), so a key cannot be put
  on the wire in the clear through any of them.
- **A server-supplied approval URL is verified before it is opened or printed.**
  The browser is only sent to a page on the configured app's origin; anything else
  is replaced by `<app-url>/cli/login?code=<code>`, built locally from a code in
  the expected `XXXX-XXXX` shape. A URL carrying a control character, an invisible
  formatting character or whitespace is refused outright rather than cleaned, so
  the address printed is always the address opened. Control and bidi characters
  are also stripped from anything a server sends that reaches a terminal. This
  closes the argument-injection path into `open`/`xdg-open`/`rundll32`, which
  treat a leading `-`, a local path or a UNC share as a valid argument.
- **Polling survives a rate limit and a dropped connection.** A 429 is now waited
  out using its `Retry-After` (or a doubling interval), and a transport failure or
  a gateway answer is retried, both inside the request's ten-minute lifetime. A
  sign-in used to end outright on the first 429, which meant anyone slower than
  about two minutes at approving could not log in at all.
- **Signing in again revokes the key it replaces**, so a re-login (or a lost
  response) does not leave a live key nobody is watching, and does not quietly eat
  one of the account's ten key slots. Failures are reported and never block the
  new sign-in.
- **The config directory is checked before it is used**: it must be a directory
  this user owns, and when no home directory can be determined the CLI asks for
  `$PREPUBLISH_CONFIG_DIR` instead of falling back to `/tmp/prepublish` — a
  predictable path another user of the machine could have created first.

### Notes

- Settings and credentials are two files, both 0600 in a 0700 directory, written
  atomically. `config path` prints the settings file, which never contains a key.
  The credentials file records the API its key belongs to; see Security above.
- Colour follows `NO_COLOR`, and every render path downsamples for a pipe, a
  16-colour terminal or truecolor.
- The API base URL defaults to `https://api.prepublish.ai` and the app to
  `https://prepublish.ai`. Point both at `localhost:8080` and `localhost:3000` for
  local work.
- The device-login endpoints (`/api/cli/auth/*`) and the approval page
  (`/cli/login`) ship alongside this release; until a deployment has them,
  `--with-token` and `$PREPUBLISH_API_KEY` are the working sign-in paths.
