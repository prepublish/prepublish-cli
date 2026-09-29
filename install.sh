#!/bin/sh
#
# Prepublish CLI installer.
#
#   curl -fsSL https://prepublish.ai/install.sh | sh
#
# Installs the `prepublish` binary on macOS or Linux, amd64 or arm64. The
# archive is verified against the release's checksums.txt before anything is
# unpacked, and nothing is written outside the install directory. No sudo is
# needed unless you choose a directory you cannot write to.
#
# Environment:
#   PREPUBLISH_VERSION        tag to install, e.g. v0.1.0 (default: latest release)
#   PREPUBLISH_INSTALL_DIR    where the binary goes (default: $HOME/.local/bin)
#   PREPUBLISH_DOWNLOAD_BASE  release base URL (default: the GitHub Releases page
#                             of prepublish/prepublish-cli); point it at a mirror,
#                             or use it to test this script against local files
#   NO_COLOR                  set to anything to turn colour off
#
# Everything runs inside main(), and main() is called on the last line, so a
# download that stops halfway cannot execute half an install.

set -eu

BINARY="prepublish"
REPO="prepublish/prepublish-cli"
DEFAULT_DOWNLOAD_BASE="https://github.com/${REPO}/releases"

PLATFORM_OS=""
PLATFORM_ARCH=""
TMP_DIR=""
SHA256_TOOL=""

BOLD=""
RED=""
YELLOW=""
GREEN=""
RESET=""

setup_colors() {
	if [ -t 2 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != "dumb" ]; then
		BOLD="$(printf '\033[1m')"
		RED="$(printf '\033[31m')"
		YELLOW="$(printf '\033[33m')"
		GREEN="$(printf '\033[32m')"
		RESET="$(printf '\033[0m')"
	fi
}

say() {
	printf '%s\n' "$*" >&2
}

detail() {
	printf '  %s\n' "$*" >&2
}

warn() {
	printf '%s\n' "${YELLOW}warning:${RESET} $*" >&2
}

die() {
	printf '%s\n' "${RED}error:${RESET} $*" >&2
	exit 1
}

cleanup() {
	if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
}

unsupported_platform() {
	die "unsupported platform: $(uname -s 2>/dev/null || echo unknown)/$(uname -m 2>/dev/null || echo unknown)
This installer covers macOS and Linux on amd64 (x86_64) and arm64.
For anything else, download a release archive from
  https://github.com/${REPO}/releases
or build the binary from source with
  go install github.com/${REPO}/cmd/prepublish@latest"
}

detect_platform() {
	case "$(uname -s 2>/dev/null || echo unknown)" in
	Darwin) PLATFORM_OS="darwin" ;;
	Linux) PLATFORM_OS="linux" ;;
	*) unsupported_platform ;;
	esac

	case "$(uname -m 2>/dev/null || echo unknown)" in
	x86_64 | amd64) PLATFORM_ARCH="amd64" ;;
	arm64 | aarch64) PLATFORM_ARCH="arm64" ;;
	*) unsupported_platform ;;
	esac
}

resolve_targets() {
	DOWNLOAD_BASE="${PREPUBLISH_DOWNLOAD_BASE:-$DEFAULT_DOWNLOAD_BASE}"
	DOWNLOAD_BASE="${DOWNLOAD_BASE%/}"

	REQUESTED_VERSION="${PREPUBLISH_VERSION:-}"
	case "$REQUESTED_VERSION" in
	"" | latest)
		RELEASE_PATH="latest/download"
		RELEASE_LABEL="latest release"
		;;
	v*)
		RELEASE_PATH="download/${REQUESTED_VERSION}"
		RELEASE_LABEL="$REQUESTED_VERSION"
		;;
	*)
		REQUESTED_VERSION="v${REQUESTED_VERSION}"
		RELEASE_PATH="download/${REQUESTED_VERSION}"
		RELEASE_LABEL="$REQUESTED_VERSION"
		;;
	esac

	INSTALL_DIR="${PREPUBLISH_INSTALL_DIR:-}"
	if [ -z "$INSTALL_DIR" ]; then
		if [ -z "${HOME:-}" ]; then
			die "no install directory: set PREPUBLISH_INSTALL_DIR (or HOME) and run this again."
		fi
		INSTALL_DIR="${HOME}/.local/bin"
	fi
	case "$INSTALL_DIR" in
	*/) INSTALL_DIR="${INSTALL_DIR%/}" ;;
	esac
	if [ -z "$INSTALL_DIR" ]; then
		die "PREPUBLISH_INSTALL_DIR is empty: set it to a directory you can write to."
	fi

	ASSET="${BINARY}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
	ASSET_URL="${DOWNLOAD_BASE}/${RELEASE_PATH}/${ASSET}"
	SUMS_URL="${DOWNLOAD_BASE}/${RELEASE_PATH}/checksums.txt"
	TARGET="${INSTALL_DIR}/${BINARY}"
}

unwritable_hint() {
	say "Cannot write to ${INSTALL_DIR}."
	say ""
	say "Take ownership of it, if you have sudo:"
	say "  sudo mkdir -p ${INSTALL_DIR} && sudo chown \"\$(id -u):\$(id -g)\" ${INSTALL_DIR}"
	say ""
	say "Or install somewhere you own:"
	say "  curl -fsSL https://prepublish.ai/install.sh | PREPUBLISH_INSTALL_DIR=\"\$HOME/.local/bin\" sh"
	exit 1
}

# Kept as a check rather than a mkdir so a run that fails verification leaves
# nothing behind: the directory is created in install_binary, after the archive
# has been checked.
check_install_dir() {
	if [ -d "$INSTALL_DIR" ]; then
		if [ ! -w "$INSTALL_DIR" ]; then
			unwritable_hint
		fi
		return 0
	fi

	probe="$INSTALL_DIR"
	while [ ! -d "$probe" ]; do
		parent="$(dirname "$probe")"
		if [ "$parent" = "$probe" ]; then
			break
		fi
		probe="$parent"
	done
	if [ ! -w "$probe" ]; then
		unwritable_hint
	fi
}

detect_sha256_tool() {
	if command -v sha256sum >/dev/null 2>&1; then
		SHA256_TOOL="sha256sum"
	elif command -v shasum >/dev/null 2>&1; then
		SHA256_TOOL="shasum"
	else
		die "no sha256 tool found: install coreutils (sha256sum) or shasum, then run this again."
	fi
}

sha256_of() {
	if [ "$SHA256_TOOL" = "sha256sum" ]; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

fetch() {
	fetch_url="$1"
	fetch_out="$2"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --retry-delay 2 -o "$fetch_out" "$fetch_url"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$fetch_out" "$fetch_url"
	else
		die "neither curl nor wget is available: install one of them and run this again."
	fi
}

verify_download() {
	expected="$(grep -E "(^|[[:space:]])\*?${ASSET}\$" "${TMP_DIR}/checksums.txt" | cut -d' ' -f1 | head -n 1 || true)"
	if [ -z "$expected" ]; then
		die "checksums.txt has no entry for ${ASSET}: refusing to install an unverified archive."
	fi

	actual="$(sha256_of "${TMP_DIR}/${ASSET}")"
	if [ "$expected" != "$actual" ]; then
		die "checksum mismatch for ${ASSET}:
  expected ${expected}
  actual   ${actual}
Nothing was installed. If this keeps happening, download the archive yourself
from ${DOWNLOAD_BASE} and check it against checksums.txt."
	fi
}

install_binary() {
	if ! tar -xzf "${TMP_DIR}/${ASSET}" -C "$TMP_DIR"; then
		die "could not extract ${ASSET}: the download may be incomplete."
	fi
	if [ ! -f "${TMP_DIR}/${BINARY}" ]; then
		die "${ASSET} does not contain a ${BINARY} binary."
	fi
	if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
		unwritable_hint
	fi
	if ! cp "${TMP_DIR}/${BINARY}" "$TARGET"; then
		unwritable_hint
	fi
	chmod 755 "$TARGET"

	# A file that came through the installer is not quarantined, but a copy made
	# by a browser first can carry the attribute and macOS would refuse to run it.
	if [ "$PLATFORM_OS" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
		xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
	fi
}

report_path() {
	case ":${PATH:-}:" in
	*":${INSTALL_DIR}:"*) return 0 ;;
	esac

	shell_name="$(basename "${SHELL:-sh}" 2>/dev/null || echo sh)"
	case "$shell_name" in
	zsh)
		path_hint="echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ${HOME:-}/.zshrc"
		;;
	bash)
		if [ "$PLATFORM_OS" = "darwin" ]; then
			path_hint="echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ${HOME:-}/.bash_profile"
		else
			path_hint="echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ${HOME:-}/.bashrc"
		fi
		;;
	fish)
		path_hint="fish_add_path ${INSTALL_DIR}"
		;;
	*)
		path_hint="export PATH=\"${INSTALL_DIR}:\$PATH\""
		;;
	esac

	say ""
	say "${INSTALL_DIR} is not on your PATH yet. Add it with:"
	say "  ${path_hint}"
	say "Then start a new shell, or run it directly as ${TARGET}."
}

main() {
	setup_colors
	detect_platform
	resolve_targets
	check_install_dir
	detect_sha256_tool

	say "${BOLD}Installing the prepublish CLI${RESET} (${RELEASE_LABEL}, ${PLATFORM_OS}/${PLATFORM_ARCH})"

	TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t prepublish)"
	trap cleanup EXIT HUP INT TERM

	detail "downloading ${ASSET}"
	fetch "$ASSET_URL" "${TMP_DIR}/${ASSET}" || die "could not download ${ASSET_URL}"
	detail "downloading checksums.txt"
	fetch "$SUMS_URL" "${TMP_DIR}/checksums.txt" || die "could not download ${SUMS_URL}"

	detail "verifying sha256"
	verify_download

	detail "installing to ${TARGET}"
	install_binary

	if ! "$TARGET" version >&2; then
		warn "${TARGET} was installed but would not run."
		die "reinstall from ${DOWNLOAD_BASE}, or build from source with 'go install github.com/${REPO}/cmd/prepublish@latest'."
	fi

	report_path

	say ""
	say "${GREEN}Done.${RESET} ${BINARY} is a single binary with no runtime dependencies."
	say "  ${BINARY} login     sign in in a browser (optional)"
	say "  ${BINARY} --help    every command, and the free tools need no account"
}

main "$@"
