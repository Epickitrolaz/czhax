#!/usr/bin/env bash
# ==============================================================================
# CardputerZero Setup Script for Raspberry Pi OS Lite (Debian Trixie, 64-bit)
# ==============================================================================
# Automates full hardware & console setup for the M5Stack CardputerZero:
#   1. Full system upgrade (with reboot guard if a new kernel is installed)
#   2. Kernel headers & build toolchain installation
#   3. Clean build & install of m5stack-linux-dtoverlays (LCD, keyboard, M5IOE1,
#      audio, battery gauge, RTC, IR, powerfail switch)
#   4. Raspberry Pi OS Lite display tweaks (disable fb_load splash, cma-64,
#      i2c-dev/rtc-ds1307 autoload, hardware user groups)
#   5. en_US.UTF-8 locale, US keyboard layout, and Terminus 6x12 console font
#      (53x14 characters on the 320x170 1.9" screen)
#   6. CardputerZero Sym-layer console keymap systemd service
#   7. Fn+U / Fn+I backlight brightness daemon & systemd service
#   8. Passwordless autologin on tty1 (optional --no-autologin)
#   9. LEDs left in default state (optional --disable-leds for RPi ACT LED)
#
# Intended to be easy to read, extend, and eventually merge into install.sh.
# ==============================================================================

set -euo pipefail

# ------------------------------------------------------------------------------
# Default configuration (override via CLI flags)
# ------------------------------------------------------------------------------
M5_REPO_URL="https://github.com/m5stack/m5stack-linux-dtoverlays.git"
DRIVERS_DIR="/usr/local/src/m5stack-linux-dtoverlays"
KEYMAP_PATH="/usr/share/keymaps/tca8418_keypad_m5stack_keymap.map"
BRIGHTNESS_BIN="/usr/local/bin/cardputer-brightness.py"

TARGET_USER=""
SKIP_UPGRADE=0
ENABLE_AUTOLOGIN=1
DISABLE_LEDS=0

# ------------------------------------------------------------------------------
# Logging helpers
# ------------------------------------------------------------------------------
log()  { printf '[cz-setup] %s\n' "$*"; }
warn() { printf '[cz-setup] WARNING: %s\n' "$*" >&2; }
die()  { printf '[cz-setup] ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
CardputerZero setup script for Raspberry Pi OS Lite (Debian Trixie, arm64)

Usage:
  sudo bash scripts/setup-cardputer.sh [options]

Options:
  --user <name>         Target non-root user for groups & tty1 autologin
                        (default: $SUDO_USER, or 'pi' if available)
  --skip-upgrade        Skip 'apt-get full-upgrade' (useful after rebooting
                        into a newly upgraded kernel)
  --no-autologin        Do not configure passwordless autologin on tty1
  --disable-leds        Disable the Raspberry Pi ACT LED in config.txt
                        (off by default; LEDs remain enabled unless passed)
  --drivers-dir <path>  Where to clone/keep m5stack-linux-dtoverlays
                        (default: /usr/local/src/m5stack-linux-dtoverlays)
  -h, --help            Show this help message
EOF
}

# ------------------------------------------------------------------------------
# Argument parsing
# ------------------------------------------------------------------------------
parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --user)
        TARGET_USER="${2:-}"
        [[ -n "$TARGET_USER" ]] || die "--user requires a username"
        shift 2
        ;;
      --skip-upgrade)
        SKIP_UPGRADE=1
        shift
        ;;
      --no-autologin)
        ENABLE_AUTOLOGIN=0
        shift
        ;;
      --disable-leds)
        DISABLE_LEDS=1
        shift
        ;;
      --drivers-dir)
        DRIVERS_DIR="${2:-}"
        [[ -n "$DRIVERS_DIR" ]] || die "--drivers-dir requires a path"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "Unknown option: $1 (run with --help for usage)"
        ;;
    esac
  done
}

# ------------------------------------------------------------------------------
# Environment & privilege checks
# ------------------------------------------------------------------------------
detect_boot_paths() {
  # Bookworm/Trixie use /boot/firmware; older layouts use /boot.
  if [[ -f /boot/firmware/config.txt ]]; then
    BOOT_CONFIG="/boot/firmware/config.txt"
    BOOT_CMDLINE="/boot/firmware/cmdline.txt"
  elif [[ -f /boot/config.txt ]]; then
    BOOT_CONFIG="/boot/config.txt"
    BOOT_CMDLINE="/boot/cmdline.txt"
  else
    die "Could not find config.txt under /boot/firmware or /boot"
  fi
}

check_environment() {
  if [[ "$EUID" -ne 0 ]]; then
    die "Please run this script as root (e.g. 'sudo bash $0')."
  fi

  local os arch
  os="$(uname -s)"
  arch="$(uname -m)"
  [[ "$os" == "Linux" ]] || die "Unsupported OS '$os' (expected Linux)."

  # CardputerZero out-of-tree modules target 64-bit Raspberry Pi OS (aarch64).
  if [[ "$arch" != "aarch64" && "$arch" != "arm64" ]]; then
    die "Unsupported architecture '$arch'. Flash Raspberry Pi OS Lite (64-bit / arm64)."
  fi

  # Resolve the regular user for hardware groups and tty1 autologin.
  if [[ -z "$TARGET_USER" ]]; then
    if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
      TARGET_USER="$SUDO_USER"
    elif id pi >/dev/null 2>&1; then
      TARGET_USER="pi"
    else
      die "Could not determine target user; pass '--user <username>'."
    fi
  fi

  id "$TARGET_USER" >/dev/null 2>&1 || die "User '$TARGET_USER' does not exist."

  detect_boot_paths
  log "Target user: $TARGET_USER | Boot config: $BOOT_CONFIG"
}

# ------------------------------------------------------------------------------
# Step 1: Full system upgrade + kernel-reboot safety guard
# ------------------------------------------------------------------------------
upgrade_system() {
  if [[ "$SKIP_UPGRADE" -eq 1 ]]; then
    log "Skipping apt full-upgrade (--skip-upgrade)."
    return 0
  fi

  log "Updating package lists and running full-upgrade..."
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get full-upgrade -y

  # Out-of-tree kernel modules MUST be compiled against the *currently running*
  # kernel. If apt installed a newer kernel (e.g. 6.18.x -> 6.18.y), compiling
  # against the new headers while running the old kernel (or vice versa) causes
  # 'disagrees about version of symbol module_layout' / Exec format error.
  local running_kver newest_kver
  running_kver="$(uname -r)"
  newest_kver="$(find /lib/modules -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort -V | tail -n1)"

  if [[ -n "$newest_kver" && "$newest_kver" != "$running_kver" ]] || [[ -f /var/run/reboot-required ]]; then
    echo ""
    echo "========================================================================"
    echo " ACTION REQUIRED: A newer kernel/system update was installed."
    echo "   Running kernel : ${running_kver}"
    echo "   Installed      : ${newest_kver:-unknown}"
    echo ""
    echo " Reboot now so uname -r matches the new kernel, then re-run this script"
    echo " with '--skip-upgrade':"
    echo "   sudo reboot"
    echo "========================================================================"
    exit 0
  fi
}

# ------------------------------------------------------------------------------
# Step 2: Install build tools, kernel headers, and console/keymap utilities
# ------------------------------------------------------------------------------
install_dependencies() {
  local kver
  kver="$(uname -r)"

  log "Installing kernel headers and build dependencies for ${kver}..."
  if [[ "$SKIP_UPGRADE" -eq 1 ]]; then
    apt-get update
  fi

  local base_pkgs=(
    build-essential
    device-tree-compiler
    git
    curl
    python3
    i2c-tools
    console-setup
    kbd
    locales
  )

  DEBIAN_FRONTEND=noninteractive apt-get install -y "${base_pkgs[@]}"

  # Prefer exact headers for the running kernel; fall back to metapackage.
  if ! DEBIAN_FRONTEND=noninteractive apt-get install -y "linux-headers-${kver}"; then
    warn "Package linux-headers-${kver} not found; trying linux-headers-rpi-v8..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y linux-headers-rpi-v8
  fi

  if [[ ! -d "/lib/modules/${kver}/build" ]]; then
    die "Kernel build tree /lib/modules/${kver}/build is missing. Reboot into the newest kernel and re-run."
  fi
}

# ------------------------------------------------------------------------------
# Step 3: Clone & cleanly build m5stack-linux-dtoverlays
# ------------------------------------------------------------------------------
build_drivers() {
  local kver
  kver="$(uname -r)"

  log "Fetching m5stack-linux-dtoverlays into ${DRIVERS_DIR}..."
  if [[ -d "${DRIVERS_DIR}/.git" ]]; then
    git -C "$DRIVERS_DIR" fetch --depth=1 origin
    git -C "$DRIVERS_DIR" reset --hard origin/HEAD
    # CRITICAL: 'make clean' inside modules/CardputerZero does NOT clean sibling
    # module directories (m5ioe1-1.0, tca8418-1.0, bq27220-1.0, etc.). Wiping
    # all build artifacts with 'git clean -fdx' prevents stale .ko files from
    # causing module_layout / Exec format errors after a kernel update.
    git -C "$DRIVERS_DIR" clean -fdx
  else
    mkdir -p "$(dirname "$DRIVERS_DIR")"
    git clone --depth=1 "$M5_REPO_URL" "$DRIVERS_DIR"
  fi

  local mod_dir="${DRIVERS_DIR}/modules/CardputerZero"
  [[ -d "$mod_dir" ]] || die "Expected directory not found: $mod_dir"

  log "Building and installing CardputerZero kernel modules and overlays..."
  # NOTE: uname -m reports 'aarch64' on 64-bit RPi OS, whereas Linux kbuild
  # requires ARCH=arm64 (there is no arch/aarch64 directory).
  make -C "$mod_dir" \
    ARCH=arm64 \
    KERNELDIR="/lib/modules/${kver}/build" \
    EXTRADIR="/lib/modules/${kver}/extra" \
    CONFIG_CARDPUTERO_V0_5=y \
    install

  # Guard config_setup so re-running the script doesn't duplicate dtoverlay lines.
  if grep -qE '^[[:space:]]*dtoverlay=cardputerzero-v5-overlay' "$BOOT_CONFIG"; then
    log "CardputerZero overlay already present in ${BOOT_CONFIG}; skipping config_setup."
  else
    log "Applying CardputerZero config.txt overlays via make config_setup..."
    make -C "$mod_dir" ARCH=arm64 CONFIG_CARDPUTERO_V0_5=y config_setup
  fi

  depmod -a "$kver"

  # Verify critical modules actually installed for the running kernel.
  local req_mod
  for req_mod in m5ioe1.ko tca8418_keypad_m5stack.ko panel-mipi-dbi-m.ko pwm_bl_m5stack.ko gpio-forwarder.ko; do
    [[ -f "/lib/modules/${kver}/extra/${req_mod}" ]] || die "Expected module missing after install: ${req_mod}"
  done
}

# ------------------------------------------------------------------------------
# IR overlays: Cardputer Zero uses GPIO13 for the receiver and GPIO12 for the
# transmitter. The m5stack overlay repository normally configures these, but
# keep the pins explicit here so a stale config.txt cannot silently lose IR.
# ------------------------------------------------------------------------------
ensure_ir_overlays() {
  local changed=0

  # pwm-ir-tx on GPIO12 conflicts with the gpio-ir-tx driver used by CZHax.
  if grep -qE '^[[:space:]]*dtoverlay=pwm-ir-tx,gpio_pin=12' "$BOOT_CONFIG"; then
    sed -i -E '/^[[:space:]]*dtoverlay=pwm-ir-tx,gpio_pin=12/d' "$BOOT_CONFIG"
    changed=1
  fi
  local rx_line='dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up'
  local tx_line='dtoverlay=gpio-ir-tx,gpio_pin=12'
  if grep -qE '^[[:space:]]*dtoverlay=gpio-ir,gpio_pin=13' "$BOOT_CONFIG"; then
    if ! grep -qE "^[[:space:]]*${rx_line}[[:space:]]*$" "$BOOT_CONFIG"; then
      sed -i -E "s|^[[:space:]]*dtoverlay=gpio-ir,gpio_pin=13.*|${rx_line}|" "$BOOT_CONFIG"
      changed=1
    fi
  else
    printf '\n# CZHax: Cardputer Zero IR receiver (GPIO13)\n%s\n' "$rx_line" >> "$BOOT_CONFIG"
    changed=1
  fi
  if grep -qE '^[[:space:]]*dtoverlay=gpio-ir-tx,gpio_pin=12' "$BOOT_CONFIG"; then
    if ! grep -qE "^[[:space:]]*${tx_line}[[:space:]]*$" "$BOOT_CONFIG"; then
      sed -i -E "s|^[[:space:]]*dtoverlay=gpio-ir-tx,gpio_pin=12.*|${tx_line}|" "$BOOT_CONFIG"
      changed=1
    fi
  else
    printf '# CZHax: Cardputer Zero IR transmitter (GPIO12)\n%s\n' "$tx_line" >> "$BOOT_CONFIG"
    changed=1
  fi
  if [[ "$changed" -eq 1 ]]; then
    log "Configured IR overlays: gpio-ir GPIO13 (RX), gpio-ir-tx GPIO12 (TX)."
  else
    log "IR overlays already configured (GPIO13 RX, GPIO12 TX)."
  fi
}

# ------------------------------------------------------------------------------
# Step 4: Raspberry Pi OS Lite boot, module, and user-group configuration
# ------------------------------------------------------------------------------
configure_boot_and_modules() {
  log "Configuring display console, kernel modules, and hardware groups..."

  # make install enables fb_load.service (an APPLaunch splash image service).
  # On Raspberry Pi OS Lite without APPLaunch, leaving it enabled blocks tty1
  # from appearing on the 1.9" ST7789 display.
  if systemctl list-unit-files fb_load.service >/dev/null 2>&1; then
    systemctl disable fb_load.service >/dev/null 2>&1 || true
  fi

  # Ensure fbcon=map:off is not disabling framebuffer console output.
  if [[ -f "$BOOT_CMDLINE" ]] && grep -qw 'fbcon=map:off' "$BOOT_CMDLINE"; then
    sed -i -E 's/[[:space:]]*fbcon=map:off//g' "$BOOT_CMDLINE"
  fi

  # Autoload I2C userspace access and the onboard RX8130 RTC driver.
  cat <<'EOF' > /etc/modules-load.d/cardputerzero.conf
i2c-dev
rtc-ds1307
EOF

  # Reserve 64 MiB CMA for KMS (only append if not already configured).
  if grep -qE '^[[:space:]]*dtoverlay=vc4-kms-v3d$' "$BOOT_CONFIG"; then
    sed -i 's/^[[:space:]]*dtoverlay=vc4-kms-v3d$/dtoverlay=vc4-kms-v3d,cma-64/' "$BOOT_CONFIG"
  fi

  # Grant target user access to hardware interfaces (add only groups that exist).
  local grp groups_to_add=()
  for grp in gpio i2c spi video input audio plugdev; do
    if getent group "$grp" >/dev/null 2>&1; then
      groups_to_add+=("$grp")
    fi
  done
  if [[ "${#groups_to_add[@]}" -gt 0 ]]; then
    local joined
    joined="$(IFS=,; echo "${groups_to_add[*]}")"
    usermod -aG "$joined" "$TARGET_USER"
  fi

  ensure_ir_overlays
}

# ------------------------------------------------------------------------------
# Step 5: en_US.UTF-8 locale, US keyboard, and Terminus 6x12 console font
# ------------------------------------------------------------------------------
configure_locale_and_console() {
  log "Configuring en_US.UTF-8 locale, US keyboard layout, and Terminus 6x12 font..."

  # Enable and set en_US.UTF-8 as the system default locale.
  if [[ -f /etc/locale.gen ]]; then
    sed -i -E 's/^[#[:space:]]*(en_US\.UTF-8[[:space:]]+UTF-8)/\1/' /etc/locale.gen
  fi
  locale-gen en_US.UTF-8
  update-locale LANG=en_US.UTF-8

  # Base Linux console / XKB keyboard layout: US QWERTY.
  cat <<'EOF' > /etc/default/keyboard
# KEYBOARD CONFIGURATION FILE
XKBMODEL="pc105"
XKBLAYOUT="us"
XKBVARIANT=""
XKBOPTIONS=""
BACKSPACE="guess"
EOF

  # Terminus 6x12 fits 53x14 characters on the 320x170 1.9" CardputerZero LCD,
  # matching CZHax's primary viewport (vs 40x10 with the default 8x16 font).
  cat <<'EOF' > /etc/default/console-setup
# CONFIGURATION FILE FOR SETUPCON
ACTIVE_CONSOLES="/dev/tty[1-6]"
CHARMAP="UTF-8"
CODESET="Lat15"
FONTFACE="Terminus"
FONTSIZE="6x12"
VIDEOMODE=
EOF

  # Apply immediately if possible (setupcon may return non-zero over pure SSH).
  setupcon --force >/dev/null 2>&1 || true
}

# ------------------------------------------------------------------------------
# Step 6: CardputerZero Sym-layer console keymap systemd service
# ------------------------------------------------------------------------------
configure_keymap_service() {
  log "Installing cardputer-keymap.service for Sym-layer key mappings..."

  [[ -f "$KEYMAP_PATH" ]] || die "Keymap file not found at ${KEYMAP_PATH}"

  # In cardputerzero-v5-overlay.dts, the Sym layer emits placeholder keycodes
  # (e.g. KEY_SCROLLLOCK=70 for Sym+E -> '_', KEY_DOT=52 for Sym+8 -> '*').
  # Without this keymap loaded on tty1, Sym+E triggers Scroll Lock (freezing the
  # TTY until pressed again) and Sym symbols do not match the printed keys.
  # Because Debian's console-setup resets the keymap at boot, we run loadkeys
  # in a systemd unit ordered after console-setup.service.
  cat <<EOF > /etc/systemd/system/cardputer-keymap.service
[Unit]
Description=Load CardputerZero TCA8418 console keymap
After=console-setup.service
Wants=console-setup.service

[Service]
Type=oneshot
ExecStart=/usr/bin/loadkeys -C /dev/tty1 ${KEYMAP_PATH}
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

  systemctl enable cardputer-keymap.service
}

# ------------------------------------------------------------------------------
# Step 7: Fn+U / Fn+I backlight brightness daemon & systemd service
# ------------------------------------------------------------------------------
configure_brightness_service() {
  log "Installing cardputer-brightness daemon (Fn+U / Fn+I)..."

  # The kernel TCA8418 driver sends KEY_BRIGHTNESSDOWN (224) on Fn+U and
  # KEY_BRIGHTNESSUP (225) on Fn+I, but headless/TTY Linux does not adjust
  # /sys/class/backlight/*/brightness automatically without a desktop daemon.
  cat <<'EOF' > "$BRIGHTNESS_BIN"
#!/usr/bin/env python3
"""Listen for CardputerZero Fn+U / Fn+I brightness keys and update sysfs."""

import glob
import os
import struct
import time

PREFERRED_DEV = "/dev/input/by-path/platform-3f804000.i2c-event"
EVENT_FMT = "qqHHi"
EVENT_SIZE = struct.calcsize(EVENT_FMT)
EV_KEY = 1
KEY_BRIGHTNESSDOWN = 224
KEY_BRIGHTNESSUP = 225
STEP = 10
MIN_BRIGHTNESS = 0  # Allow full off (10 clean steps: 0, 10, ..., 100).


def find_keyboard_dev():
    if os.path.exists(PREFERRED_DEV):
        return PREFERRED_DEV
    for name_path in sorted(glob.glob("/sys/class/input/event*/device/name")):
        try:
            with open(name_path, "r", encoding="utf-8") as f:
                name = f.read().strip().lower()
            if "tca8418" in name:
                ev = name_path.split("/")[4]
                dev_path = f"/dev/input/{ev}"
                if os.path.exists(dev_path):
                    return dev_path
        except OSError:
            continue
    return None


def get_bl_paths():
    dirs = sorted(glob.glob("/sys/class/backlight/*"))
    if not dirs:
        return None, None
    return os.path.join(dirs[0], "brightness"), os.path.join(dirs[0], "max_brightness")


def adjust(delta):
    b_path, m_path = get_bl_paths()
    if not b_path:
        return
    try:
        with open(m_path, "r", encoding="utf-8") as f:
            max_b = int(f.read().strip())
        with open(b_path, "r", encoding="utf-8") as f:
            cur = int(f.read().strip())
        step = max(1, max_b // 10)
        if delta > 0:
            new_b = min(max_b, ((cur // step) + 1) * step)
        elif cur <= 0:
            new_b = MIN_BRIGHTNESS
        else:
            new_b = max(MIN_BRIGHTNESS, ((cur - 1) // step) * step)
        with open(b_path, "w", encoding="utf-8") as f:
            f.write(str(new_b))
    except OSError:
        pass


def main():
    while True:
        dev = find_keyboard_dev()
        if not dev:
            time.sleep(1.0)
            continue
        try:
            with open(dev, "rb") as f:
                while True:
                    data = f.read(EVENT_SIZE)
                    if len(data) < EVENT_SIZE:
                        break
                    _, _, ev_type, code, value = struct.unpack(EVENT_FMT, data)
                    if ev_type == EV_KEY and value in (1, 2):
                        if code == KEY_BRIGHTNESSDOWN:
                            adjust(-STEP)
                        elif code == KEY_BRIGHTNESSUP:
                            adjust(STEP)
        except OSError:
            time.sleep(1.0)


if __name__ == "__main__":
    main()
EOF
  chmod 0755 "$BRIGHTNESS_BIN"

  cat <<EOF > /etc/systemd/system/cardputer-brightness.service
[Unit]
Description=CardputerZero Fn+U / Fn+I Backlight Control
After=systemd-udev-settle.service

[Service]
Type=simple
ExecStart=${BRIGHTNESS_BIN}
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

  systemctl enable cardputer-brightness.service
}

# ------------------------------------------------------------------------------
# Step 8: Passwordless tty1 autologin
# ------------------------------------------------------------------------------
configure_autologin() {
  local dropin_dir="/etc/systemd/system/getty@tty1.service.d"
  local dropin_file="${dropin_dir}/autologin.conf"

  if [[ "$ENABLE_AUTOLOGIN" -eq 0 ]]; then
    log "Skipping tty1 autologin (--no-autologin)."
    return 0
  fi

  log "Configuring tty1 autologin for user '${TARGET_USER}'..."
  mkdir -p "$dropin_dir"
  cat <<EOF > "$dropin_file"
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin ${TARGET_USER} --noclear %I \$TERM
EOF
}

# ------------------------------------------------------------------------------
# Step 9: Optional LED disable (left ON by default)
# ------------------------------------------------------------------------------
configure_leds() {
  if [[ "$DISABLE_LEDS" -eq 0 ]]; then
    log "Leaving LEDs at default behavior (pass --disable-leds to disable ACT LED)."
    return 0
  fi

  log "Disabling Raspberry Pi ACT LED in ${BOOT_CONFIG}..."
  if ! grep -qE '^[[:space:]]*dtparam=act_led_trigger=none' "$BOOT_CONFIG"; then
    cat <<'EOF' >> "$BOOT_CONFIG"

# Disable Raspberry Pi Zero ACT LED
dtparam=act_led_trigger=none
dtparam=act_led_activelow=off
EOF
  fi
}

# ------------------------------------------------------------------------------
# Finalize & summary
# ------------------------------------------------------------------------------
main() {
  parse_args "$@"
  check_environment

  upgrade_system
  install_dependencies
  build_drivers
  configure_boot_and_modules
  configure_locale_and_console
  configure_keymap_service
  configure_brightness_service
  configure_autologin
  configure_leds

  systemctl daemon-reload

  cat <<EOF

========================================================================
 CardputerZero setup complete!
========================================================================
 Configured:
   - m5stack-linux-dtoverlays built for kernel $(uname -r) (ARCH=arm64)
   - fb_load.service disabled so tty1 owns the 1.9" ST7789 display
   - Locale: en_US.UTF-8 | Keyboard: us | Console font: Terminus 6x12
   - cardputer-keymap.service enabled (Sym-layer mappings on tty1)
   - cardputer-brightness.service enabled (Fn+U dim / Fn+I brighten)
   - IR overlays: gpio-ir GPIO13 (receiver), gpio-ir-tx GPIO12 (transmitter)
   - tty1 autologin: $( [[ "$ENABLE_AUTOLOGIN" -eq 1 ]] && echo "enabled (${TARGET_USER})" || echo "disabled" )
   - LEDs: $( [[ "$DISABLE_LEDS" -eq 1 ]] && echo "ACT LED disabled" || echo "default (enabled)" )

 Notes:
   - Reboot now to activate all drivers and services:
       sudo reboot
   - If Mini-HDMI is plugged in during boot, HDMI claims fb0 and the
     1.9" LCD gets fb1. Boot with HDMI unplugged for tty1 on the LCD
     (or switch live with: sudo con2fbmap 1 1).
   - After future kernel upgrades, re-run with '--skip-upgrade' so
     m5stack-linux-dtoverlays is rebuilt for the new kernel.
========================================================================
EOF
}

main "$@"
