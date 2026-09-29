# Release smoke test

A manual pass over the CLI, run before every release. It takes about 40 minutes,
and it needs a real terminal: most of what can go wrong here is what a person
sees, and half of these steps deliberately check that a pipe sees something
different.

## What you need

- macOS or Linux with Go 1.26 or newer (`make build`).
- A terminal at least 100 columns wide, and a second one you can resize to 60.
- A script of 800 words or more, as a `.md` or `.txt` file.
- A second script file of 200 words, for the tools.
- A free account (sign in at prepublish.ai with any address) and a subscribed
  account, for the tier checks.
- An API key from prepublish.ai/dashboard, for the `--with-token` path.
- A short video (under 30 MB: a screen recording is fine) and a short audio file.
- A working browser where you are signed in to prepublish.ai.
- For section 12, a stack whose worker is *not* running, so an audit is accepted
  and then never finishes. The local stack is the easy way to get one.

Run everything against production unless a step says otherwise. Start from a
clean config directory so nothing is carried over:

```sh
cd prepublish-cli
make build
export PREPUBLISH_CONFIG_DIR=$(mktemp -d)
export PATH="$PWD/bin:$PATH"
prepublish version
```

Expected: it prints `prepublish <tag> (<commit>)` and a `built ...` line, and the
exit status is 0 (`echo $?`). A stale `bin/prepublish` is the commonest cause of
a confusing result later, so rebuild before starting.

## 0. The bare command

1. Run `prepublish` with no arguments in the terminal.
2. Press `q` (or pick Quit) to leave.

Expected: the home screen — a banner, a line with the account or the anonymous
allowance, and the menu. Resize the window and reopen it: the layout still fits,
and nothing is cut off at 60 columns. Exit leaves the terminal's scrollback
intact rather than leaving the alternate screen's contents on it.

3. Run `prepublish < /dev/null | cat` and `prepublish --no-input`.

Expected: both print the help text and exit 0. No menu, no prompt, no escape
sequences in the piped case.

## 1. Anonymous audit

1. Run `prepublish audit script.md --title "Test: the Roman concrete lasts"`.
2. At the email prompt, enter a real address you can receive at.
3. Wait for the report.

Expected: a progress line per stage (`queued`, `analyzing the script`, …) while it
works, then the report: title, id, the score gauges, the attention-risk curve
drawn as a chart, one key improvement, the improvements list with the gated ones
marked `locked`, and the honest-scope line about attention risk. The report ends
with its share URL and the word `locked` appears somewhere in the footer when the
report is gated. Nothing about "recording" is claimed as a prediction.

4. Check the config directory: `prepublish config path`, then `ls -l` it and read
   `config.json`.

Expected: `email` holds the address you typed, `anonymous_id` holds `cli:<uuid>`,
and `config.json` is mode `-rw-------` in a `drwx------` directory. No
`credentials.json` exists yet.

5. Run a second audit and do not pass `--email`.

Expected: no prompt. It uses the saved address.

6. Run three more audits in a row.

Expected: the fourth is refused with `FREE_ANALYSIS_USED` and exit 4
(`echo $?`). Run `prepublish whoami`: it reports the anonymous allowance with
zero remaining for this machine.

## 2. Piping and machine mode

1. `cat script.md | prepublish audit - --title "Test: piped" --json > out.json`
2. `jq .overall_score out.json` and `wc -l out.json`.

Expected: stdout is one JSON document and nothing else — no progress lines, no
colours, no prompt. `--json` with a piped stdin must never ask for anything: pass
`--email you@example.com` if the config has no address yet. The field names are
the API's (`overall_score`, `retention_curve`, `improvements`).

3. `prepublish whoami --json | jq .` and `prepublish version --json | jq .`.

Expected: one object each; `version --json` carries `version`, `commit`, `date`
and `user_agent`.

4. `prepublish audit script.md --json` with no `--title`, and with a `--title` but
   no email and no terminal.

Expected: exit 2, and stderr carries
`{"error":{"status":0,"code":"","message":"a video title is required: pass --title"}}`.
Nothing is written to stdout.

5. `prepublish history --json` while signed out.

Expected: exit 3, and a message naming `prepublish login`.

6. `printf 'not json at all' | prepublish report --json 00000000-0000-0000-0000-000000000000`

Expected: exit 1 with a JSON error envelope whose `code` is `NOT_FOUND`.

7. `prepublish report nope --api-url http://127.0.0.1:9` and `prepublish report 6f1c0f5e --api-url http://127.0.0.1:9`.

Expected: exit 2 in both cases, saying that is not a report id, and it returns
immediately — the point of the port that nothing listens on is that a rejected id
must never become a request. Then `prepublish report
https://prepublish.ai/analysis/<a real id>` prints that report: a pasted report URL
works in place of the id.

## 3. Colour

1. `prepublish whoami` in the terminal.
2. `NO_COLOR=1 prepublish whoami`.
3. `prepublish whoami | cat`.

Expected: the first is coloured and the other two are plain — and steps 2 and 3
produce byte-identical output. Check the report too: `prepublish report <id>`
under each condition. A TUI step (`prepublish audit ...`, home) is unaffected by
`NO_COLOR` in its layout, only in its colours.

## 4. Browser sign-in

1. `prepublish login`.
2. Approve the machine in the browser page it opens.

Expected: the code is shown large and readable, the browser opens on
`prepublish.ai/cli/login?code=...` with the code already filled in, and pressing
`o` in the CLI reopens it. After approval the CLI prints `Signed in as <email>`,
a line about claimed audits when it had some (the anonymous audits from step 1
should be claimed here), and the account card with the tier and today's quota.

3. Check `prepublish.ai/dashboard`: a new key named `CLI · prepublish-cli on
   <hostname>` is listed.
4. Run `prepublish history`.

Expected: the audits from step 1 are there, newest first, with their scores. Press
enter on one: the report opens in the pager, `esc` returns to the list, `q`
leaves. Piped (`prepublish history | cat`), the first column is the **full** id,
not a truncated one: copy one out of the listing and run `prepublish report <id>`
on it — it must print that report. A shortened id would make the listing a dead
end, so this is the check that the column is usable.

5. `prepublish login` again, and answer no at the prompt.

Expected: it says you are already signed in and keeps the existing credentials.

6. `prepublish login --no-browser`.

Expected: exactly this shape, and no browser opening:

```
Open this URL to approve this machine:

  https://prepublish.ai/cli/login?code=BCDF-GHJK

Code: BCDF-GHJK (check it matches the page)

waiting for approval…
```

The URL is the complete one, with the code already in it; the code is printed
once, for the user to compare against the page; and `waiting for approval…`
appears once, not once per poll. It still signs in when you approve the code
manually. This is the path a remote shell uses.

7. `prepublish login` and let the code expire (ten minutes) without approving it.

Expected: exit 3 with a message saying the request expired and to run `prepublish
login` again. It must not hang past the expiry.

8. `prepublish login` and press Deny in the browser.

Expected: exit 3, "the request was denied in the browser". No credential is
stored, and any previous sign-in is untouched.

## 5. Signing in with a key

1. Create a key at prepublish.ai/dashboard and pipe it in:
   `printf '%s' "pp_live_..." | prepublish login --with-token`.
2. `prepublish whoami`.

Expected: `Signed in as <email>` and the account card, and the dashboard shows the
key you pasted, not a new one. `whoami` reports the credential source as the file.

3. `printf 'pp_live_not_a_real_key' | prepublish login --with-token`.

Expected: exit 3, the key is rejected, and the sign-in from step 1 still works
afterwards (`prepublish whoami`). Verification happens before the file is written,
which is the point of the step.

4. `prepublish login --with-token` in a terminal with no pipe.

Expected: exit 2 with a message telling you to pipe the key in, not a hang.

5. `PREPUBLISH_API_KEY=... prepublish whoami`.

Expected: it acts as that key's account and reports the source as the environment.

## 6. Tiers

For each of: signed out, a free account, a subscribed account, a studio account:

1. `prepublish whoami`

Expected: signed out, the anonymous allowance for this machine; otherwise the
account, its tier, `audits used/limit` for today and where the credential came
from (`file` or `env`). Running `whoami` must never prompt.

2. `prepublish audit script.md --title "Test: tier <name>"`.

Expected: a free account gets a gated report and is never asked for an email; a
paid account gets the full report — the rewrite bodies, the title rewrite and the
policy passages are present rather than `locked`. Run `prepublish audit script.md
--title "..." --json | jq .locked` to check: `false` for paid, `true` for free and
anonymous.

## 7. Video and audio upload

Needs the subscribed account.

1. `prepublish audit clip.mp4 --title "Test: uploaded clip"`.

Expected: an upload progress bar, then the audit progress. The stages include
`transcribing the recording`, which a script-only audit never shows, and the
finished report is not gated. The thumbnail, if the file has a frame worth
analyzing, shows up as a `visual_insights` section.

2. Sign out, then repeat with a free account.

Expected: exit 4 with `NO_SUBSCRIPTION` (or `SUBSCRIPTION_REQUIRED`) and a message
saying uploads are part of the subscription. Nothing is uploaded.

3. Repeat signed out.

Expected: exit 3, telling you to run `prepublish login` before anything is sent.

4. `prepublish audit clip.mkv --title "Test: unsupported"` (or any `.flac`).

Expected: exit 2 with the list of extensions the API accepts. This one is checked
because the media list and the API's list are deliberately not the same.

5. `prepublish audit missing.mp4 --title "Test: missing"`.

Expected: exit 1, "no such file", before any request.

## 8. The standalone tools

1. `prepublish hook --text "Everyone gets this wrong, and it costs them years." --niche history`.
2. `prepublish hook hook.md --email you@example.com`.
3. `prepublish policy script.md --title "Test: policy"`.
4. `prepublish authenticity script.md --title "Test: authenticity"`.
5. `prepublish policy script.md | head -5` and each of the three with `--json`.

Expected: a rendered result for each — the hook with per-sentence scores and
rewrites, the policy with its verdict, flags and the scope statement, the
authenticity check with its signals and fixes. `--json` prints the API struct and
nothing else. `--text` wins when both a file and text are given. A missing script
argument in a pipe exits 2 naming the alternatives.

6. Use a fresh config directory with no email, and run `prepublish hook --text "..."`.

Expected: exit 4 with `email_required` and a message pointing at `--email`. The
hook tool must not prompt for an email: it offers to send one instead.

## 9. runtime

1. `prepublish runtime script.md`.
2. `cat script.md | prepublish runtime -`.
3. `prepublish runtime --minutes 10`.
4. `prepublish runtime script.md --minutes 10`.

Expected: (1) and (2) agree: the word count, and three runtimes whose spread is
close to a minute per ten minutes. (3) prints the three word counts for a
ten-minute video with no network call — check with the network off. (4) exits 2,
because both inputs answer different questions.

5. `prepublish runtime clip.mp4`.

Expected: exit 2, saying this counts words in a script. It must not print a word
count for a binary file.

## 10. Signing out

1. `prepublish logout`.
2. Check the dashboard, and `prepublish whoami`.

Expected: `Signed out`, and the key that was used is gone from the dashboard's
list — revoked, not merely forgotten. `whoami` reports the anonymous allowance,
and `credentials.json` no longer exists.

3. Sign in again from a key, while already signed in:
   `printf '%s' "$OTHER_KEY" | prepublish login --with-token`.

Expected: the sign-in succeeds, and the key it replaced is gone from the
dashboard list — it is revoked before the new one is stored, so a re-login does
not leave a live key nobody is watching. Do this twice with two keys made for the
test.

4. `prepublish logout` again, and `prepublish logout` while signed in only through
   `PREPUBLISH_API_KEY`.

Expected: "Not signed in" for the first. The second explains that the environment
variable outranks the file and that nothing was deleted — and the key must still
be in the dashboard afterwards.

5. Sign in, revoke the key in the dashboard by hand, then `prepublish logout`.

Expected: a warning that the key could not be revoked is acceptable here (it is
already gone), and the local file is still deleted. Exit 0 either way.

## 11. Config, URLs and errors

1. `prepublish config get`, `config get api-url`, `config set email
   nobody@example.com`, `config get email`, `config unset email`, `config get email`.
2. `prepublish config set api-url http://localhost:8080`, then `config get api-url`
   with `PREPUBLISH_API_URL` unset and then set. Unset it, then `config unset api-url`.

Expected: `get` prints what is in effect; the env note appears on stderr only when
a variable is the reason; `unset` restores the default; `config set api-url nonsense`
exits 2 rather than writing a URL the next run would die on.

2b. `prepublish config set api-url http://api.example.com`, and
   `PREPUBLISH_API_URL=http://api.example.com prepublish whoami`, and
   `PREPUBLISH_APP_URL=http://prepublish.example.com prepublish whoami`.

Expected: all three exit 2, each naming the flag, variable or setting that set the
value, and nothing is written to `config.json`. `http://localhost:8080` and
`http://127.0.0.1:8080` still work, which is what a local stack needs.

3. `prepublish config` and `prepublish config bogus`.

Expected: help, and exit 2 with the key list, respectively.

4. `prepublish whoami --api-url http://127.0.0.1:9`.

Expected: exit 1 with a message naming the URL — "could not reach" — not a
timeout-shaped hang. GETs retry, so allow a few seconds.

5. Point the CLI at a local API and app (`config set api-url
   http://localhost:8080`, `config set app-url http://localhost:3000`) with a
   local API and web app running, and repeat steps 1, 4 and the `login` flow.

Expected: everything works locally; the device-login page opened is the local
`/cli/login`. Then `config unset api-url && config unset app-url`.

6. Break the config on purpose: append `{` to `config.json`, then run
   `prepublish whoami`.

Expected: a clear parse error naming the file, and exit 1. Delete the file and
confirm the CLI works again from defaults.

## 12. Waiting for a report

The wait is bounded, and the bound has to behave when it is reached. Point both
steps at a stack whose worker is stopped (the local stack with the worker not
running is ideal — the audit is accepted and never progresses).

1. `prepublish audit script.md --title "Test: never finishes" --email you@example.com --timeout 5s`.

Expected: exit 1 after about five seconds, with

```
• still running after 5s — the audit keeps working on the server
• id      <uuid>
• report  https://prepublish.ai/analysis/<uuid>
• pick it up later with `prepublish report <uuid>`
```

and no second, conflicting error beneath it. The id must be real: run
`prepublish report <uuid>` and confirm it says the audit is pending (or prints it
once the worker is back) rather than 404s. Nothing is lost by the timeout: the
audit was accepted, and the id is the point of printing it.

2. The same command with `--json`.

Expected: stdout stays empty apart from nothing at all, stderr carries
`{"error":{"status":0,"code":"","message":"the audit is still running after 5s:
`prepublish report <uuid>` picks it up later"}}`, and the exit status is 1.

3. `prepublish audit script.md --title "Test: no timeout" --timeout 0` with the
   worker stopped, then Ctrl-C.

Expected: it waits without a deadline; Ctrl-C stops it cleanly with exit 130 and
`cancelled`, and the audit keeps its id on the server.

4. With the worker running, run a normal audit and watch it finish.

Expected: the timeout never fires — a working worker finishes well inside fifteen
minutes, and `--timeout 5s` on a *fast* audit still completes if the report lands
in time. The deadline is a floor on patience, not a cap on the audit.

## 13. Credential and URL hardening

Each of these is a change made in response to a security review of the sign-in
path. They are cheap to check by hand and expensive to get wrong.

1. With a stored sign-in against production, run
   `prepublish whoami --api-url https://staging.example.com`.

Expected: one line on **stderr** — `stored credentials are for
https://api.prepublish.ai; running signed out against https://staging.example.com`
— and the command then behaves as anonymous. The stored key must not appear in
the request: watch the API logs, or point the flag at the local stack and read its
request log. `prepublish whoami` with no flag uses the stored key again.

2. `PREPUBLISH_API_KEY=pp_live_... prepublish whoami --api-url https://staging.example.com`.

Expected: the environment key **is** sent. Exporting a key pairs it with whatever
API this command is aimed at; that is deliberate and is what CI relies on.

3. Point the CLI at a local stack (`config set api-url http://localhost:8080`)
   while signed in to production, and run `prepublish whoami`.

Expected: a fresh anonymous `whoami`, no key on the local API's wire, and the
warning line from step 1. `config unset api-url` afterwards restores the stored
sign-in.

4. Sign out, then `env -u HOME -u XDG_CONFIG_HOME prepublish config path` with
   `PREPUBLISH_CONFIG_DIR` unset.

Expected: exit 1 with a message asking for `$PREPUBLISH_CONFIG_DIR`. It must not
print, or create, a path under `/tmp`.

5. `chmod 755` the config directory and run `prepublish whoami`; then, if you have
   a second account on the machine, make that account own the directory and try
   again.

Expected: the wider mode alone is not fatal (`credentials.json` inside is still
0600); a directory owned by someone else is refused with a message naming
`$PREPUBLISH_CONFIG_DIR`.

6. Approve a login and watch the printed URL while the API is answering with a
   bad page (point the CLI at the local stack and edit `/cli/auth/start` to return
   a `verification_uri_complete` on another host, or a `user_code` with a control
   character in it).

Expected: the CLI never opens or prints the foreign URL. When the code is in the
expected shape it prints its **own** `<app-url>/cli/login?code=<code>`; when the
code is not, it prints that the API did not report a usable page. No escape
sequence from the server ever reaches the terminal.

## After the pass

Delete the temporary config directory and any keys created during the pass. Then
confirm the diff carries no debug output, bump the version in the tag, add the
dated entry to `docs/CHANGELOG.md`, run `make release`, and check a built binary
from `dist/` reports the new tag.
