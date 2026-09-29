# Contributing

Issues and pull requests are welcome. This repository is the CLI, the installer
and the release tooling; the hosted API and the web app are not open source, so a
change that needs a new API field has to start as an issue describing it.

## Before opening a pull request

```sh
make test     # go test ./...
make vet      # go vet ./...
gofmt -l .    # must print nothing
```

`make lint` runs golangci-lint when it is installed and prints the command to get
it when it is not. CI runs the formatting check, `go vet`, `go test -race` and
shellcheck over `install.sh`.

## What makes a good change

- **Tests live beside what they cover.** Add one when behaviour could plausibly
  break; the packages carry suites for wire-format fidelity, error parsing,
  resumable uploads and credential permissions.
- **Comments explain why, not what.** The interesting parts of this codebase are
  decisions — why a retry is allowed here and not there, why a key is withheld
  from an unfamiliar API — and those are worth a sentence.
- **A user-visible change gets a `docs/CHANGELOG.md` entry** under `Unreleased`.
- **Commit messages shape the release notes.** `feat:` and `fix:` prefixes become
  the Features and Bug fixes sections of a release; `docs:`, `test:`, `ci:`,
  `style:`, `build:` and `chore` commits are filtered out of them.
- **`install.sh` is POSIX sh**, not bash — it runs under whatever `/bin/sh` a
  user has, and through a pipe, so add nothing that needs bash, and keep
  shellcheck clean.

## Security

Report a vulnerability through the address in [SECURITY.md](SECURITY.md) rather
than in a public issue.

## License

By contributing you agree that your contribution is licensed under the MIT
license in [LICENSE](LICENSE).
