#!/usr/bin/env bash
set -euo pipefail

REPO="Epickitrolaz/czhax"
BIN_NAME="czhax"
VERSION="${CZHAX_VERSION:-latest}"
USER_INSTALL=0
UNINSTALL=0

usage() {
  cat <<EOF
CZHax binary installer & updater for M5Stack Cardputer Zero & Linux

Usage:
  install.sh [options]

Options:
  --version <tag>   Install a specific release tag (e.g. v0.2.0). Default: latest
  --user            Install to \$HOME/.local/bin instead of /usr/local/bin
  --uninstall       Remove installed czhax binary
  -h, --help        Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      if [[ -z "$VERSION" ]]; then
        echo "error: --version requires a tag argument" >&2
        exit 1
      fi
      shift 2
      ;;
    --user)
      USER_INSTALL=1
      shift
      ;;
    --uninstall)
      UNINSTALL=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ "$USER_INSTALL" -eq 1 ]]; then
  INSTALL_DIR="${HOME}/.local/bin"
else
  INSTALL_DIR="/usr/local/bin"
fi
TARGET_PATH="${INSTALL_DIR}/${BIN_NAME}"

if [[ "$UNINSTALL" -eq 1 ]]; then
  for p in "/usr/local/bin/${BIN_NAME}" "${HOME}/.local/bin/${BIN_NAME}"; do
    if [[ -f "$p" ]]; then
      if [[ -w "$(dirname "$p")" ]]; then
        rm -f "$p"
      else
        sudo rm -f "$p"
      fi
      echo "Removed ${p}"
    fi
  done
  exit 0
fi

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$OS" != "linux" ]]; then
  echo "error: unsupported OS '$OS' (CZHax prebuilt binaries target Linux)" >&2
  exit 1
fi

ARCH="$(uname -m)"
case "$ARCH" in
  aarch64|arm64)
    ASSET="czhax-linux-arm64"
    ;;
  armv7l|armv7|armhf)
    ASSET="czhax-linux-armv7"
    ;;
  x86_64|amd64)
    ASSET="czhax-linux-amd64"
    ;;
  *)
    echo "error: unsupported architecture '$ARCH'" >&2
    exit 1
    ;;
esac

resolve_latest_tag() {
  local api_url="https://api.github.com/repos/${REPO}/releases/latest"
  local tag=""
  if command -v curl >/dev/null 2>&1; then
    tag="$(curl -fsSL -H 'Accept: application/vnd.github+json' -H 'Cache-Control: no-cache' "$api_url" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
    if [[ -z "$tag" ]]; then
      local url
      url="$(curl -fsSL -H 'Cache-Control: no-cache' -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" || true)"
      tag="${url##*/}"
    fi
  elif command -v wget >/dev/null 2>&1; then
    tag="$(wget -qO- --header='Accept: application/vnd.github+json' --header='Cache-Control: no-cache' "$api_url" 2>/dev/null | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
    if [[ -z "$tag" ]]; then
      local url
      url="$(wget -q --max-redirect=5 --server-response "https://github.com/${REPO}/releases/latest" -O /dev/null 2>&1 | awk '/^  Location: /{loc=$2} END{print loc}' | tr -d '\r')"
      tag="${url##*/}"
    fi
  fi
  printf '%s' "$tag"
}

if [[ "$VERSION" == "latest" ]]; then
  RESOLVED="$(resolve_latest_tag)"
  if [[ -n "$RESOLVED" && "$RESOLVED" != "latest" ]]; then
    VERSION="$RESOLVED"
  fi
fi

if [[ "$VERSION" == "latest" || -z "$VERSION" ]]; then
  echo "error: could not resolve latest release tag from GitHub" >&2
  exit 1
fi

BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${ASSET} (${VERSION})..."
if command -v curl >/dev/null 2>&1; then
  curl -fL --progress-bar "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${BIN_NAME}"
  curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress "${BASE_URL}/${ASSET}" -O "${TMP_DIR}/${BIN_NAME}"
  wget -q "${BASE_URL}/checksums.txt" -O "${TMP_DIR}/checksums.txt"
else
  echo "error: curl or wget is required" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  EXPECTED="$(grep " ${ASSET}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}' || true)"
  if [[ -z "$EXPECTED" ]]; then
    echo "error: checksum entry for ${ASSET} missing in checksums.txt" >&2
    exit 1
  fi
  ACTUAL="$(sha256sum "${TMP_DIR}/${BIN_NAME}" | awk '{print $1}')"
  if [[ "$EXPECTED" != "$ACTUAL" ]]; then
    echo "error: checksum mismatch for ${ASSET}" >&2
    echo "  expected: ${EXPECTED}" >&2
    echo "  actual:   ${ACTUAL}" >&2
    exit 1
  fi
  echo "SHA256 verified."
fi

chmod +x "${TMP_DIR}/${BIN_NAME}"

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
if [[ -w "$INSTALL_DIR" ]]; then
  install -m 0755 "${TMP_DIR}/${BIN_NAME}" "$TARGET_PATH"
elif command -v sudo >/dev/null 2>&1; then
  sudo mkdir -p "$INSTALL_DIR"
  sudo install -m 0755 "${TMP_DIR}/${BIN_NAME}" "$TARGET_PATH"
else
  echo "error: cannot write to ${INSTALL_DIR} and sudo is unavailable (try --user)" >&2
  exit 1
fi

echo ""
echo "Installed ${BIN_NAME} to ${TARGET_PATH}"
"${TARGET_PATH}" --version || true
echo "Run 'czhax' to start, or 'sudo czhax update' anytime to update."
