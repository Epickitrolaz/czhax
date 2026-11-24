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

- **WiFi-Pineapple-Style Layout**:
  - **Left Vertical Sidebar** for top-level views (`Dash`, `Recon`, `Net`, `Lab`, `Sys`, `Opts`).
  - **Top Horizontal Tabs** inside each view (e.g. `Pulse | Pinned | System`, `WiFi | BLE | IR`, `IFs | Scan | Pkt`, `Crypto | HWBus`, `Mon | Notes | Files`, `Display | Ctrls | Keys`).
  - **High-Contrast LCD Palettes**: Bright text on pure black with solid inverse selection bars tuned for the 1.9" 320×170 ST7789 display (`53×14`, `45×10`, and `40×10`).
- **Navigation & Cardputer Zero Keymap**:
  - **Tab selection view**: `↑↓` (`Fn+F/X`) moves through vertical left views, `←→` (`Fn+Z/C`) switches top horizontal tabs, `Enter` enters the tab's content
  - **Inside tab content**: `↑↓←→` navigate and adjust within that tab; `Esc` returns to the tab selection view
  - `F4`–`F8` (`Fn+4`..`Fn+8`) jumps directly to `Dash`, `Recon`, `Net`, `Sys`, `Opts`
  - `/` quick search launcher, `p` pin/unpin to Dash, `s` Stealth Mode, `?` help
- **Cybersecurity Hub (Phase 2 Pre-flight)**: Structured modules for `WiFi`, `BLE`, `IR`, `Scan`, `Pkt`, `Crypto`, and `HW Bus` (I2C/UART/Cap).
- **Basic Tools (Active)**:
  - **System Monitor** (`Sys → Mon`): CPU load, RAM, disk, temp, battery fuel gauge, CM0 detection
  - **Net Interfaces** (`Net → IFs`): live NICs, IPv4/IPv6, MAC, MTU, link state
  - **Quick Notes** (`Sys → Notes`): saved in `~/.config/czhax/notes.txt`
  - **File Browser** (`Sys → Files`): compact directory navigator & text pager
- **Persistent Options** (`~/.config/czhax/config.json`): High Contrast display mode, Viewport (`Auto`, `CZ 53x14`, `CZ 45x10`, `CZ 40x10`, `Full`), Sidebar (`Auto`, `3-Char`, `Full`), Style (`compact`/`minimal`/`ascii`), Footer & Fn4-8 toggles, Nav mode, Refresh rate, Stealth Mode, and Confirm Quit.

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
