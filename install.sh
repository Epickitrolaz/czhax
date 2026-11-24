#!/usr/bin/env bash
set -euo pipefail

REPO="Epickitrolaz/czhax"
BIN_NAME="czhax"
VERSION="${CZHAX_VERSION:-latest}"
USER_INSTALL=0
UNINSTALL=0

usage() {
  cat <<EOF
CZHax binary installer for M5Stack Cardputer Zero & Linux

Usage:
  install.sh [options]

Options:
  --version <tag>   Install a specific release tag (e.g. v0.1.0). Default: latest
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

if [[ "$VERSION" == "latest" ]]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo "Downloading ${ASSET} (${VERSION})..."
if command -v curl >/dev/null 2>&1; then
  curl -fL --progress-bar "${BASE_URL}/${ASSET}" -o "${TMP_DIR}/${BIN_NAME}"
  curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt" 2>/dev/null || true
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress "${BASE_URL}/${ASSET}" -O "${TMP_DIR}/${BIN_NAME}"
  wget -q "${BASE_URL}/checksums.txt" -O "${TMP_DIR}/checksums.txt" 2>/dev/null || true
else
  echo "error: curl or wget is required" >&2
  exit 1
fi

if [[ -s "${TMP_DIR}/checksums.txt" ]] && command -v sha256sum >/dev/null 2>&1; then
  EXPECTED="$(grep " ${ASSET}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}' || true)"
  if [[ -n "$EXPECTED" ]]; then
    ACTUAL="$(sha256sum "${TMP_DIR}/${BIN_NAME}" | awk '{print $1}')"
    if [[ "$EXPECTED" != "$ACTUAL" ]]; then
      echo "error: checksum mismatch for ${ASSET}" >&2
      echo "  expected: ${EXPECTED}" >&2
      echo "  actual:   ${ACTUAL}" >&2
      exit 1
    fi
    echo "SHA256 verified."
  fi
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
echo "Run 'czhax' on Cardputer Zero, or 'czhax --cz' on a desktop terminal."
