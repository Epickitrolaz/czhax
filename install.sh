#!/usr/bin/env bash
#
# CZHax installer for the M5Stack Cardputer Zero (and any Debian-ish Linux).
#
# With no arguments on a terminal this is an interactive menu; with --all (or
# any component flag) it does exactly what you asked for and nothing else.
#
#   sudo ./install.sh                 # interactive
#   sudo ./install.sh --all           # everything below
#   sudo ./install.sh --czhax         # just the TUI binary
#   sudo ./install.sh --nexmon --nexmon-host pi@100.64.0.1
#
# Components:
#   cardputer  hardware setup for Raspberry Pi OS Lite on the Cardputer Zero:
#              dtoverlays, console font/locale, keymap + brightness services
#   nexmon     nexmon firmware + brcmfmac for monitor mode and frame injection
#   czhax      the CZHax TUI (release binary)
#   packages   pentest/wireless tooling (aircrack-ng, hcxdumptool, mdk4, …)
#
set -euo pipefail

REPO="Epickitrolaz/czhax"
BIN_NAME="czhax"
VERSION="${CZHAX_VERSION:-latest}"
SRC_REF="${CZHAX_REF:-main}"
USER_INSTALL=0
UNINSTALL=0

WANT_CARDUPTER=0
WANT_NEXMON=0
WANT_CZHAX=0
WANT_PACKAGES=0
COMPONENTS_GIVEN=0

CARD_USER=""
CARD_AUTOLOGIN=1
CARD_LEDS=0
CARD_SKIP_UPGRADE=0
NEXMON_HOST=""

HERE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
TMP_DIR=""

log()  { printf '\033[1m>>\033[0m %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<EOF
CZHax installer for the M5Stack Cardputer Zero

Usage:
  install.sh [component ...] [options]

Components (no component = interactive menu on a terminal):
  --all             cardputer + nexmon + czhax + packages
  --cardputer       hardware setup: dtoverlays, console, keymap, brightness
  --nexmon          nexmon firmware + patched brcmfmac (monitor/injection)
  --czhax           install the CZHax TUI binary
  --packages        install wireless/pentest packages

Options:
  --version <tag>   CZHax release tag (default: latest)
  --user            install czhax to ~/.local/bin
  --uninstall       remove the czhax binary and exit
  --user-name <u>   target user for the Cardputer setup (default: autodetect)
  --no-autologin    Cardputer setup without tty1 autologin
  --disable-leds    Cardputer setup with the RPi ACT LED disabled
  --skip-upgrade    Cardputer setup without apt full-upgrade
  --nexmon-host <u@h>
                    build the firmware here, then finish on that host over ssh
  --ref <git-ref>   repo ref to take the setup scripts from (default: main)
  -h, --help        this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --all) WANT_CARDUPTER=1; WANT_NEXMON=1; WANT_CZHAX=1; WANT_PACKAGES=1; COMPONENTS_GIVEN=1; shift ;;
    --cardputer|--cardputer-zero) WANT_CARDUPTER=1; COMPONENTS_GIVEN=1; shift ;;
    --nexmon) WANT_NEXMON=1; COMPONENTS_GIVEN=1; shift ;;
    --czhax) WANT_CZHAX=1; COMPONENTS_GIVEN=1; shift ;;
    --packages) WANT_PACKAGES=1; COMPONENTS_GIVEN=1; shift ;;
    --version) VERSION="${2:-}"; [[ -n "$VERSION" ]] || die "--version needs a tag"; shift 2 ;;
    --user) USER_INSTALL=1; shift ;;
    --uninstall) UNINSTALL=1; shift ;;
    --user-name) CARD_USER="${2:-}"; [[ -n "$CARD_USER" ]] || die "--user-name needs a name"; shift 2 ;;
    --no-autologin) CARD_AUTOLOGIN=0; shift ;;
    --disable-leds) CARD_LEDS=1; shift ;;
    --skip-upgrade) CARD_SKIP_UPGRADE=1; shift ;;
    --nexmon-host) NEXMON_HOST="${2:-}"; [[ -n "$NEXMON_HOST" ]] || die "--nexmon-host needs user@host"; shift 2 ;;
    --ref) SRC_REF="${2:-}"; [[ -n "$SRC_REF" ]] || die "--ref needs a git ref"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (try --help)" ;;
  esac
done

# ---------------------------------------------------------------- helpers ---
need_root() { [[ "$(id -u)" -eq 0 ]] || die "this step needs root, re-run with sudo"; }

have() { command -v "$1" >/dev/null 2>&1; }

ask() { # ask <prompt> <default y|n>
  local prompt="$1" def="$2" ans=""
  local hint="[Y/n]"; [[ "$def" == "n" ]] && hint="[y/N]"
  printf '%s %s ' "$prompt" "$hint" >/dev/tty
  read -r ans </dev/tty || ans=""
  ans="${ans:-$def}"
  [[ "${ans,,}" == "y" ]]
}

fetch_source() {
  # reuse the local checkout when the installer runs from one
  if [[ -n "$HERE" && -f "$HERE/scripts/setup-cardputer.sh" && -f "$HERE/firmware/nexmon/provision.sh" ]]; then
    SRC_DIR="$HERE"
    return 0
  fi
  [[ -n "$TMP_DIR" ]] || { TMP_DIR="$(mktemp -d)"; trap 'rm -rf "$TMP_DIR"' EXIT; }
  SRC_DIR="$TMP_DIR/src"
  log "fetching $REPO@$SRC_REF"
  mkdir -p "$SRC_DIR"

  # a branch or a tag: try refs/heads first, then refs/tags
  local url got=""
  for kind in heads tags; do
    url="https://codeload.github.com/${REPO}/tar.gz/refs/${kind}/${SRC_REF}"
    if have curl; then
      if curl -fsSL --retry 3 --retry-delay 2 --retry-connrefused "$url" |
        tar xz -C "$SRC_DIR" --strip-components=1 2>/dev/null; then got=1; fi
    elif have wget; then
      if wget -qO- --tries=3 --waitretry=2 "$url" |
        tar xz -C "$SRC_DIR" --strip-components=1 2>/dev/null; then got=1; fi
    else
      die "curl or wget is required"
    fi
    [[ -n "$got" ]] && break
    rm -rf "$SRC_DIR"; mkdir -p "$SRC_DIR"
  done
  [[ -n "$got" ]] || die "could not fetch $REPO@$SRC_REF (try --ref <branch-or-tag>)"
}

# -------------------------------------------------------------- czhax TUI ---
install_czhax() {
  local os arch asset
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  [[ "$os" == "linux" ]] || die "unsupported OS '$os' (CZHax binaries target Linux)"
  arch="$(uname -m)"
  case "$arch" in
    aarch64|arm64) asset="czhax-linux-arm64" ;;
    armv7l|armv7|armhf) asset="czhax-linux-armv7" ;;
    x86_64|amd64) asset="czhax-linux-amd64" ;;
    *) die "unsupported architecture '$arch'" ;;
  esac

  local install_dir target version="$VERSION"
  if [[ "$USER_INSTALL" -eq 1 ]]; then install_dir="${HOME}/.local/bin"; else install_dir="/usr/local/bin"; fi
  target="${install_dir}/${BIN_NAME}"

  if [[ "$version" == "latest" ]]; then
    local url tag
    if have curl; then
      url="$(curl -fsSL --retry 3 -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest" || true)"
      tag="${url##*/}"
      if [[ -z "$tag" || "$tag" == "latest" ]]; then
        tag="$(curl -fsSL -H 'Accept: application/vnd.github+json' \
          "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
          sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1 || true)"
      fi
    else
      tag="$(wget -q --server-response -O /dev/null --max-redirect=5 \
        "https://github.com/${REPO}/releases/latest" 2>&1 |
        awk '/^  Location: /{loc=$2} END{print loc}' | tr -d '\r')"
      tag="${tag##*/}"
    fi
    [[ -n "$tag" && "$tag" != "latest" ]] || die "could not resolve the latest release tag"
    version="$tag"
  fi

  local base="https://github.com/${REPO}/releases/download/${version}"
  local tmp; tmp="$(mktemp -d)"
  log "downloading ${asset} (${version})"
  if have curl; then
    curl -fL --progress-bar --retry 3 --retry-delay 2 "${base}/${asset}" -o "${tmp}/${BIN_NAME}"
    curl -fsSL --retry 3 --retry-delay 2 "${base}/checksums.txt" -o "${tmp}/checksums.txt"
  else
    wget -q --show-progress "${base}/${asset}" -O "${tmp}/${BIN_NAME}"
    wget -q "${base}/checksums.txt" -O "${tmp}/checksums.txt"
  fi
  if have sha256sum; then
    local expected actual
    expected="$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}' || true)"
    [[ -n "$expected" ]] || die "checksum entry for ${asset} missing"
    actual="$(sha256sum "${tmp}/${BIN_NAME}" | awk '{print $1}')"
    [[ "$expected" == "$actual" ]] || die "checksum mismatch for ${asset}"
  fi
  chmod +x "${tmp}/${BIN_NAME}"
  if [[ -w "$install_dir" ]]; then
    install -m 0755 "${tmp}/${BIN_NAME}" "$target"
  else
    have sudo || die "cannot write ${install_dir} and sudo is unavailable (try --user)"
    sudo mkdir -p "$install_dir"
    sudo install -m 0755 "${tmp}/${BIN_NAME}" "$target"
  fi
  rm -rf "$tmp"
  log "installed ${target}"
}

# ------------------------------------------------------- cardputer setup ---
setup_cardputer() {
  need_root
  fetch_source
  local s="$SRC_DIR/scripts/setup-cardputer.sh"
  [[ -f "$s" ]] || die "setup-cardputer.sh not found in the source tree"
  local args=()
  [[ -n "$CARD_USER" ]] && args+=(--user "$CARD_USER")
  [[ "$CARD_AUTOLOGIN" -eq 0 ]] && args+=(--no-autologin)
  [[ "$CARD_LEDS" -eq 1 ]] && args+=(--disable-leds)
  [[ "$CARD_SKIP_UPGRADE" -eq 1 ]] && args+=(--skip-upgrade)
  log "configuring the Cardputer Zero hardware (dtoverlays, console, services)"
  bash "$s" "${args[@]}"
}

# ------------------------------------------------------ nexmon wifi stack ---
setup_nexmon() {
  need_root
  fetch_source
  local p="$SRC_DIR/firmware/nexmon/provision.sh"
  [[ -f "$p" ]] || die "firmware/nexmon/provision.sh not found in the source tree"
  chmod +x "$p" "$SRC_DIR/firmware/nexmon/driver/build-driver.sh" 2>/dev/null || true

  if [[ -n "$NEXMON_HOST" ]]; then
    [[ "$(uname -m)" == "x86_64" ]] || die "--nexmon-host builds the firmware on the x86_64 machine that runs the installer; on the board just run the installer without it"
    log "nexmon: building the firmware here, finishing on $NEXMON_HOST"
    bash "$p" --host "$NEXMON_HOST"
    return
  fi

  # provision.sh builds the firmware from source on x86_64 and otherwise applies
  # the prebuilt patch bundle to the stock image, so this works everywhere.
  log "nexmon: installing firmware and driver"
  bash "$p"
}

# ------------------------------------------------------- pentest packages ---
setup_packages() {
  need_root
  local want=(
    aircrack-ng hcxtools hcxdumptool mdk4 pixiewps reaver tshark nmap
    macchanger iw wireless-tools rfkill hostapd dnsmasq bluez bluez-tools
    bluez-deprecated-tools bettercap usbutils pciutils alsa-utils ffmpeg mpv
  )
  local avail=() p
  apt-get update -qq
  for p in "${want[@]}"; do
    if apt-cache show "$p" >/dev/null 2>&1; then avail+=("$p"); fi
  done
  [[ ${#avail[@]} -gt 0 ]] || die "none of the packages are available in your repos"
  log "installing ${#avail[@]} packages: ${avail[*]}"
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${avail[@]}"
}

# ------------------------------------------------------------ interactive ---
interactive_menu() {
  local choice
  cat <<'EOF'

CZHax installer - M5Stack Cardputer Zero

  1) Cardputer setup     display, keyboard, audio, console, services
  2) Wi-Fi monitor mode  nexmon firmware + patched brcmfmac (injection too)
  3) CZHax TUI           the terminal UI itself
  4) Pentest packages    aircrack-ng, hcxdumptool, mdk4, pixiewps, ...

  a) All of the above
  q) Quit

EOF
  printf 'Select [1-4, a, q]: ' >/dev/tty
  read -r choice </dev/tty || choice="q"

  case "${choice,,}" in
    q|quit|"") log "nothing to do"; exit 0 ;;
    a|all) WANT_CARDUPTER=1; WANT_NEXMON=1; WANT_CZHAX=1; WANT_PACKAGES=1 ;;
    *)
      local item
      for item in ${choice//,/ }; do
        case "$item" in
          1) WANT_CARDUPTER=1 ;;
          2) WANT_NEXMON=1 ;;
          3) WANT_CZHAX=1 ;;
          4) WANT_PACKAGES=1 ;;
          *) die "unknown selection: $item" ;;
        esac
      done
      ;;
  esac

  # per-component customisation, defaults on Enter
  if [[ "$WANT_CARDUPTER" -eq 1 ]]; then
    printf '\nCardputer setup options (Enter keeps the default)\n'
    if ! ask "  configure tty1 autologin?" y; then CARD_AUTOLOGIN=0; fi
    if ask "  disable the Raspberry Pi ACT LED?" n; then CARD_LEDS=1; fi
    if ! ask "  run a full system upgrade first?" y; then CARD_SKIP_UPGRADE=1; fi
  fi
  if [[ "$WANT_NEXMON" -eq 1 && "$(uname -m)" == "x86_64" ]]; then
    printf '\nWi-Fi monitor mode\n'
    printf '  Push the result to a board over ssh (user@host, Enter to stay local): ' >/dev/tty
    read -r NEXMON_HOST </dev/tty || NEXMON_HOST=""
  fi
  if [[ "$WANT_CZHAX" -eq 1 ]]; then
    if ask "  install czhax for the current user only (~/.local/bin)?" n; then USER_INSTALL=1; fi
  fi
  printf '\n'
}

# ------------------------------------------------------------------- main ---
if [[ "$UNINSTALL" -eq 1 ]]; then
  for p in "/usr/local/bin/${BIN_NAME}" "${HOME}/.local/bin/${BIN_NAME}"; do
    if [[ -f "$p" ]]; then
      if [[ -w "$(dirname "$p")" ]]; then rm -f "$p"; else sudo rm -f "$p"; fi
      echo "Removed ${p}"
    fi
  done
  exit 0
fi

if [[ "$COMPONENTS_GIVEN" -eq 0 ]]; then
  if [[ -t 0 ]]; then
    interactive_menu
  elif [[ -r /dev/tty && -w /dev/tty ]]; then
    # NOTE: do not `exec </dev/tty` here. When this script is piped into bash
    # (curl ... | bash) the shell reads the script itself from stdin; pointing
    # stdin at the terminal would make bash wait for the rest of the script on
    # the keyboard. The interactive reads below explicitly use /dev/tty.
    interactive_menu
  else
    # non-interactive (curl | bash in CI, cron, …): keep the old behaviour
    WANT_CZHAX=1
  fi
fi

if [[ "$WANT_CARDUPTER" -eq 1 ]]; then setup_cardputer; fi
if [[ "$WANT_NEXMON" -eq 1 ]]; then setup_nexmon; fi
if [[ "$WANT_CZHAX" -eq 1 ]]; then install_czhax; fi
if [[ "$WANT_PACKAGES" -eq 1 ]]; then setup_packages; fi

echo
if command -v "$BIN_NAME" >/dev/null 2>&1; then
  "$BIN_NAME" --version 2>/dev/null || true
  echo "Run '$BIN_NAME' to start, or 'sudo $BIN_NAME update' to update."
fi
