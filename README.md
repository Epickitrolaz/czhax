<img src="assets/icon.svg" width="64" height="64" alt="">

# CZHax

**CZHax** is a cybersecurity-focused TUI (Terminal User Interface) built in Go for the **M5Stack Cardputer Zero** (Raspberry Pi CM0 Linux handheld with a 1.9" 320×170 ST7789 display and 46-key TCA8418 keyboard).

## Install on Your Cardputer Zero

### Option 1: One-line Binary Install (Recommended)

Run directly on your Cardputer Zero (over SSH or in a terminal on the unit):

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | bash
```

This auto-detects `aarch64` / `arm64` (or `armv7l` / `x86_64`), downloads the latest release binary from [GitHub Releases](https://github.com/Epickitrolaz/czhax/releases/latest), verifies its SHA256 checksum, and installs it to `/usr/local/bin/czhax`.

To install into `~/.local/bin` without `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | bash -s -- --user
```

To uninstall:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | bash -s -- --uninstall
```

### Option 2: Direct Binary Download

Download `czhax-linux-arm64` (64-bit Raspberry Pi OS / Kali arm64) from [GitHub Releases](https://github.com/Epickitrolaz/czhax/releases/latest):

```sh
curl -fLO https://github.com/Epickitrolaz/czhax/releases/latest/download/czhax-linux-arm64
chmod +x czhax-linux-arm64
sudo mv czhax-linux-arm64 /usr/local/bin/czhax
czhax
```

### Launch

```sh
czhax          # Auto-fits Cardputer Zero screen (or full terminal)
czhax --cz     # Desktop preview inside a 53x14 Cardputer Zero bezel
czhax --cz45   # Ultra-compact 45x10 preview
```

---

## Phase 1 Features

- **Small-Screen-First Layout**: Engineered for `53×14` and `45×10` terminal viewports (1-line header + 1-line footer, inline scroll counters, automatic truncation).
- **Navigation & Cardputer Zero Keymap**:
  - `↑↓←→` (`Fn+F/X/Z/C` on Cardputer Zero) and optional `h/j/k/l`
  - `1`–`4` or `Tab`/`Shift+Tab` to switch **Home / SecTools / Basic / Options**
  - `F4`–`F8` mapped to **Cardputer Zero `Fn+4`..`Fn+8`** hardware softkeys
  - `/` quick search launcher across all modules
  - `p` to pin/unpin tools to Home, `t` to cycle themes, `s` to toggle Stealth Mode, `?` for keymap help
- **Cybersecurity Hub (Phase 2 Pre-flight)**: Structured modules for `Wi-Fi Recon`, `LAN & Port Scan`, `BLE & IR Lab`, `Packet & DNS`, `Crypto & Hash`, and `I2C / UART / Cap`.
- **Basic Tools (Active)**:
  - **System Monitor** (CPU load, RAM, disk, thermal temp, battery fuel gauge, CM0 detection)
  - **Net Interfaces** (live NICs, IPv4/IPv6, MAC, MTU, link state)
  - **Quick Notes** (`~/.config/czhax/notes.txt`)
  - **File Browser** (compact directory navigator & text pager)
- **Persistent Options** (`~/.config/czhax/config.json`): 5 LCD-tuned themes (`Cyber Green`, `Purple Hax`, `Amber Ops`, `Ice Cyan`, `High Contrast`), Viewport profile, Style (`compact`/`minimal`/`ascii`), Footer hints toggle, Fn4-8 strip toggle, Nav key mode, Telemetry refresh interval, Stealth Mode, and Quit confirmation.

## Build from Source

```sh
# Run tests
make test

# Desktop simulator (53x14 Cardputer Zero viewport)
make run-cz

# Build all release binaries (arm64, armv7, amd64) + checksums
make build-all

# Install local build to /usr/local/bin/czhax
sudo make install
```

Created in [T3 Code](https://t3.codes).
