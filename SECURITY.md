# Security policy

## Reporting a vulnerability

Report it through <https://prepublish.ai/contact>, not in a public issue. Please
include what you found, how to reproduce it, the output of `prepublish version`
and the platform you ran it on. We will acknowledge the report, tell you what we
think, and keep you posted until it is fixed or explained. Give us a reasonable
window to fix it before publishing anything.

## What is in scope

- **Credential handling in the CLI.** The config and credentials files, their
  permissions, the atomic writes, and the rule that a stored key is only ever
  sent back to the API that issued it.
- **The sign-in flow.** How the approval URL from the API is validated before it
  is opened or printed, how the device code is handled, and what reaches a
  terminal from a server.
- **The HTTP client.** URL validation, the loopback rule for plain `http`, and
  which requests are retried.
- **The installer.** `install.sh`: checksum verification, what it downloads and
  where it writes.
- **Release artifacts.** The archives, `checksums.txt` and the installer attached
  to a release — a mismatch between them is a security bug, not a packaging one.
- **The hosted service** (`api.prepublish.ai`, `prepublish.ai`) where it affects
  the CLI. Its code is not open source, but a report about it is still welcome
  through the same address.

## What is not in scope

- Anything that requires an attacker to already have access to the machine or the
  user account: a local attacker who can read `credentials.json` can also replace
  the binary.
- `PREPUBLISH_API_KEY` being sent to the API a command is aimed at. Exporting a
  key is an explicit pairing with that API; it is documented, and it is what
  makes the CLI usable against a staging API in CI.
- Running against plain `http` on a loopback address. That case is deliberately
  allowed for a local server.
- Denial of service and rate limiting against the hosted API (report it anyway if
  it looks exploitable, but it is handled as operations).
- Social engineering, phishing, and findings that consist only of missing
  hardening headers on the website.

## Supported versions

The latest release is the supported one, and fixes ship as a new tag. Older tags
are not patched, so upgrading is the fix — `PREPUBLISH_VERSION=<tag>` on the
installer, or `go install ...@<tag>`, pins a version deliberately.
