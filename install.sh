#!/bin/sh
# Install docker-sops, a Docker CLI plugin, into the Docker CLI plugins
# directory. Usage:
#
#   curl -fsSL https://raw.githubusercontent.com/mohsen0/docker-sops/main/install.sh | sh
#
# Environment variables:
#   DOCKER_SOPS_VERSION  Release tag to install (default: latest).
#   DOCKER_CLI_PLUGIN_DIR Install directory (default: $HOME/.docker/cli-plugins).
set -eu

REPO="mohsen0/docker-sops"
BINARY_NAME="docker-sops"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
DOWNLOAD_BASE="https://github.com/${REPO}/releases/download"

log() {
	printf '%s\n' "$*" >&2
}

die() {
	log "error: $*"
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || die "required command '$1' not found on PATH"
}

need_cmd curl
need_cmd tar
need_cmd sed

detect_os() {
	uname_os=$(uname -s)
	case "$uname_os" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "unsupported OS: $uname_os (docker-sops only ships linux and darwin binaries)" ;;
	esac
}

detect_arch() {
	uname_arch=$(uname -m)
	case "$uname_arch" in
	x86_64 | amd64) echo amd64 ;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported architecture: $uname_arch (docker-sops only ships amd64 and arm64 binaries)" ;;
	esac
}

OS=$(detect_os)
ARCH=$(detect_arch)

if [ -n "${DOCKER_SOPS_VERSION:-}" ]; then
	VERSION="$DOCKER_SOPS_VERSION"
else
	log "resolving latest release of ${REPO}..."
	VERSION=$(curl -fsSL "$API_URL" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
	[ -n "$VERSION" ] || die "could not resolve the latest release tag from $API_URL"
fi

log "installing docker-sops ${VERSION} (${OS}/${ARCH})"

# Version in the archive/checksum filenames has the leading 'v' stripped.
VERSION_NUM=$(echo "$VERSION" | sed 's/^v//')
ARCHIVE="docker-sops_${VERSION_NUM}_${OS}_${ARCH}.tar.gz"
ARCHIVE_URL="${DOWNLOAD_BASE}/${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="${DOWNLOAD_BASE}/${VERSION}/checksums.txt"

TMPDIR=$(mktemp -d 2>/dev/null || mktemp -d -t docker-sops)
cleanup() {
	rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

log "downloading ${ARCHIVE_URL}"
curl -fsSL -o "${TMPDIR}/${ARCHIVE}" "$ARCHIVE_URL" ||
	die "failed to download ${ARCHIVE_URL} (does version ${VERSION} ship a ${OS}/${ARCH} build?)"

log "downloading ${CHECKSUMS_URL}"
curl -fsSL -o "${TMPDIR}/checksums.txt" "$CHECKSUMS_URL" ||
	die "failed to download ${CHECKSUMS_URL}"

log "verifying checksum"
EXPECTED=$(grep " ${ARCHIVE}\$" "${TMPDIR}/checksums.txt" | awk '{print $1}')
[ -n "$EXPECTED" ] || die "no checksum entry for ${ARCHIVE} in checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
	ACTUAL=$(sha256sum "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	ACTUAL=$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')
else
	die "neither sha256sum nor shasum is available to verify the download"
fi

[ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch for ${ARCHIVE}: expected ${EXPECTED}, got ${ACTUAL}"

log "extracting ${ARCHIVE}"
tar -xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR" "$BINARY_NAME" ||
	die "failed to extract ${BINARY_NAME} from ${ARCHIVE}"

PLUGIN_DIR="${DOCKER_CLI_PLUGIN_DIR:-$HOME/.docker/cli-plugins}"
mkdir -p "$PLUGIN_DIR"

install_bin() {
	if command -v install >/dev/null 2>&1; then
		install -m 0755 "${TMPDIR}/${BINARY_NAME}" "${PLUGIN_DIR}/${BINARY_NAME}"
	else
		cp "${TMPDIR}/${BINARY_NAME}" "${PLUGIN_DIR}/${BINARY_NAME}"
		chmod 0755 "${PLUGIN_DIR}/${BINARY_NAME}"
	fi
}
install_bin

log "installed ${PLUGIN_DIR}/${BINARY_NAME}"

if command -v docker >/dev/null 2>&1; then
	if docker sops version >/dev/null 2>&1; then
		log "docker sops is ready:"
		docker sops version >&2
	else
		log "docker-sops was installed, but 'docker sops version' failed to run;"
		log "make sure docker is on your PATH and try again"
	fi
else
	log "docker was not found on PATH; install docker to use the plugin"
fi

log "docker-sops ${VERSION} installed successfully"
