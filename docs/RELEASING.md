# Releasing the Prepublish CLI

The CLI is developed in the monorepo at `prepublish-cli/` and published to
`github.com/prepublish/prepublish-cli`, which is a mirror of that directory
produced by a subtree split. Tags are cut *in the public repository*, and a tag
is what triggers the release workflow.

Nothing here is public-facing; the install instructions users read are in
`README.md`.

## Before you tag

1. `gofmt -l .`, `go vet ./...`, `go test ./...` and `go build ./...` all clean.
2. Run `docs/TESTING.md` end to end against production, including the paid path
   on a subscribed account.
3. Add the dated entry to `docs/CHANGELOG.md`. The heading version is the tag.

## 1. Push the code to the public repository

```sh
# In the monorepo, on the release commit, with a clean tree.
git subtree split --prefix=prepublish-cli -b cli-release
git push git@github.com:prepublish/prepublish-cli.git cli-release:main --force
git branch -D cli-release
```

The split branch is rebuilt from scratch on every release, so the push is forced:
the public `main` is a mirror of this directory and nothing else. Everything in
`prepublish-cli/` ships, including `.github/workflows/`, `install.sh` and
`LICENSE` — check `git status --porcelain prepublish-cli` before the split if you
are unsure whether something new is tracked (the repository root ignores `*.md`,
and `prepublish-cli/.gitignore` re-includes the files that are meant to be
published).

A tag is not carried across by the split, so it is cut on the other side.

## 2. Tag

```sh
git clone git@github.com:prepublish/prepublish-cli.git
cd prepublish-cli
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

The tag version and the changelog heading are the same string.

## 3. What the release workflow produces

`.github/workflows/release.yml` runs on any `v*` tag with `contents: write` and
calls GoReleaser. It publishes, on the release:

| Asset | Contents |
| --- | --- |
| `prepublish_darwin_amd64.tar.gz`, `prepublish_darwin_arm64.tar.gz`, `prepublish_linux_amd64.tar.gz`, `prepublish_linux_arm64.tar.gz` | the `prepublish` binary, `LICENSE`, `README.md` |
| `prepublish_windows_amd64.zip`, `prepublish_windows_arm64.zip` | the same, with `prepublish.exe` |
| `checksums.txt` | sha256 of every archive, `<hex>  <filename>` |
| `install.sh` | the installer, attached so a release is self-contained |

The names carry no version on purpose:
`.../releases/latest/download/<name>` has to keep working, and
`https://prepublish.ai/install.sh` is a redirect to
`.../releases/latest/download/install.sh`.

Local dry run, no tag and no upload:

```sh
cd prepublish-cli
make release-check     # validates .goreleaser.yaml
make release           # dist/, the same archives and checksums.txt
```

Both the Makefile and the workflow pin GoReleaser `v2.18.2`; they have to move
together, or a local snapshot and a published release stop agreeing about asset
names.

## 4. Smoke-test the installer

Against the published release:

```sh
curl -fsSL https://prepublish.ai/install.sh | PREPUBLISH_INSTALL_DIR=/tmp/pp-latest sh
/tmp/pp-latest/prepublish version                  # the new tag

curl -fsSL https://prepublish.ai/install.sh | \
  PREPUBLISH_VERSION=v0.2.0 PREPUBLISH_INSTALL_DIR=/tmp/pp-pinned sh
/tmp/pp-pinned/prepublish version
```

Both are worth running: the first exercises the `latest` path and the website
redirect, the second the pinned path, and they are different URLs.

### Testing it without a release

`PREPUBLISH_DOWNLOAD_BASE` makes the installer read from anywhere that has the
release layout, which is how it is tested before a tag exists. Serve `dist/` in
the GitHub shape:

```sh
cd prepublish-cli
mkdir -p /tmp/rel/latest/download /tmp/rel/download/v0.0.0-test
cp dist/prepublish_* dist/checksums.txt /tmp/rel/latest/download/
cp dist/prepublish_* dist/checksums.txt install.sh /tmp/rel/download/v0.0.0-test/
python3 -m http.server 8099 --bind 127.0.0.1 --directory /tmp/rel &

PREPUBLISH_DOWNLOAD_BASE=http://127.0.0.1:8099 PREPUBLISH_INSTALL_DIR=/tmp/pp-test sh install.sh
PREPUBLISH_DOWNLOAD_BASE=http://127.0.0.1:8099 PREPUBLISH_INSTALL_DIR=/tmp/pp-test \
  PREPUBLISH_VERSION=v0.0.0-test bash install.sh
cat install.sh | PREPUBLISH_DOWNLOAD_BASE=http://127.0.0.1:8099 PREPUBLISH_INSTALL_DIR=/tmp/pp-test-test dash
```

Worth repeating after any change to `install.sh`: tamper with one byte of an
archive in the served directory and check the installer refuses it with a
checksum error and installs nothing, and that a `uname` shim reporting FreeBSD
prints the unsupported-platform message instead of a partial install.

## 5. Afterwards

- Confirm the asset list on the release page and that `checksums.txt` covers every
  archive.
- Confirm `go install github.com/prepublish/prepublish-cli/cmd/prepublish@v0.2.0`
  resolves.
- If a release is wrong, delete the release **and** the tag and cut it again
  rather than moving a tag: `go install` and the installer both cache by tag.
