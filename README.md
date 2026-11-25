<img src="assets/icon.svg" width="64" height="64" alt="">

# CZHax

**CZHax** is a minimal, high-contrast TUI built in Go for the **M5Stack Cardputer Zero** (Raspberry Pi CM0 Linux handheld with a 1.9" 320×170 ST7789 display and 46-key TCA8418 keyboard).

## Install & Update on Your Cardputer Zero

### One installer for everything

[`install.sh`](install.sh) is an interactive installer. Run it on the board and pick what you want:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | sudo bash
```

```
CZHax installer - M5Stack Cardputer Zero

  1) Cardputer setup     display, keyboard, audio, console, services
  2) Wi-Fi monitor mode  nexmon firmware + patched brcmfmac (injection too)
  3) CZHax TUI           the terminal UI itself
  4) Pentest packages    aircrack-ng, hcxdumptool, mdk4, pixiewps, ...

  a) All of the above
  q) Quit
```

It then asks only about the options that matter for your selection (tty1 autologin, ACT LED, full upgrade first, `~/.local/bin` vs `/usr/local/bin`, …) and everything else keeps its default. The same choices are available as flags for scripted installs:

```sh
sudo ./install.sh --all          # cardputer + nexmon + czhax + packages
sudo ./install.sh --cardputer --no-autologin --disable-leds
sudo ./install.sh --nexmon --nexmon-host pi@100.64.0.1   # build firmware here, finish on the board
sudo ./install.sh --czhax --version v0.2.0 --user
sudo ./install.sh --packages
```

With no component flag and no terminal (e.g. `curl | bash` in CI) it keeps the old behaviour and installs the CZHax binary only.

What each component does:

| component | contents |
| --- | --- |
| **Cardputer setup** | [`scripts/setup-cardputer.sh`](scripts/setup-cardputer.sh): `m5stack-linux-dtoverlays` for the LCD/keyboard/audio/battery/RTC, `fb_load` disabled, `en_US.UTF-8` + Terminus 6×12 console, `cardputer-keymap` and `cardputer-brightness` services, tty1 autologin |
| **Wi-Fi monitor mode** | [`firmware/nexmon`](firmware/nexmon): patched CYW43439 firmware + nexmon `brcmfmac` — monitor mode with radiotap and frame injection (`airmon-ng`, `aireplay-ng`, `mdk4` all work). Builds the firmware on x86_64, or the driver plus a firmware hand-off on the board itself |
| **CZHax TUI** | downloads the latest release binary, verifies its SHA256 and installs it to `/usr/local/bin/czhax` |
| **Pentest packages** | `aircrack-ng`, `hcxtools`, `hcxdumptool`, `mdk4`, `pixiewps`, `reaver`, `tshark`, `nmap`, `macchanger`, `hostapd`, `dnsmasq`, `bettercap`, … (only those available in your repos) |

### Fresh Raspberry Pi OS Lite Setup (Drivers, Keymap & Console)

If you prefer the hardware step on its own, run the setup script over SSH to build the CardputerZero drivers and configure the display, keyboard, and brightness controls:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/scripts/setup-cardputer.sh | sudo bash
```

What [`scripts/setup-cardputer.sh`](scripts/setup-cardputer.sh) configures:
- Runs `apt full-upgrade` and installs matching kernel headers + build tools (if a newer kernel is installed, it pauses and asks you to reboot and re-run with `--skip-upgrade`)
- Cleanly builds and installs [`m5stack-linux-dtoverlays`](https://github.com/m5stack/m5stack-linux-dtoverlays) (`ARCH=arm64`, `CONFIG_CARDPUTERO_V0_5=y`) so the 1.9" LCD, TCA8418 keyboard, M5IOE1 expander (stopping the 3 blinking left-side LEDs), audio, battery gauge, and RTC work
- Disables `fb_load.service`, sets `cma-64`, and configures `en_US.UTF-8` + `us` layout + **Terminus `6×12`** console font (`53×14` characters)
- Installs and enables `cardputer-keymap.service` (fixes `Sym`-layer symbols and prevents `Sym+E` from triggering Scroll Lock)
- Installs and enables `cardputer-brightness.service` (`Fn+U` / `Fn+I` backlight control)
- Enables `tty1` autologin for your user (pass `--no-autologin` to keep password login, `--disable-leds` to turn off the Pi ACT LED, or `--user <name>` to override the user)

### First-Time Install

Run directly on your Cardputer Zero:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | bash
```

This downloads the latest release binary from [GitHub Releases](https://github.com/Epickitrolaz/czhax/releases/latest), verifies its SHA256 checksum, and installs it to `/usr/local/bin/czhax`.

To install into `~/.local/bin` without `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/Epickitrolaz/czhax/main/install.sh | bash -s -- --user
```

### Updating Later

Always use the same single built-in command to update to the latest release:

```sh
sudo czhax update
```

*(If installed with `--user` into `~/.local/bin`, run `czhax update` without `sudo`.)*

### Launch

```sh
czhax          # Auto-fits Cardputer Zero screen (or full terminal)
czhax --cz     # Desktop preview inside a 53x14 Cardputer Zero bezel
czhax --cz45   # Ultra-compact 45x10 preview
```

---

## Current Features

- **High-Contrast Small-Screen Layout**: Built for `40×10`, `45×10`, and `53×14` terminal viewports with a left vertical view bar (`Status`/`Sts`, `System`/`Sys`) and top horizontal tab bar.
- **Device Status (`Status → Device`)**:
  - Hostname & hardware/arch (`CardZero CM0` detection)
  - Primary IP / link state
  - RAM & Disk usage bars
  - Battery percentage & **estimated time remaining until battery runs out** (via Linux `power_supply` fuel-gauge `time_to_empty_*` or charge/energy rate)
  - CPU temperature, 1m load, and uptime
- **System Tools (`System → Tools`)**:
  - **Update CZHax**: Built-in GitHub release updater with SHA256 verification
  - **Restart CZHax**: In-place relaunch (`exec`) to load newly installed updates
  - **Terminal Shell**: Drop into your interactive shell and return to CZHax with `exit` or `Ctrl+D`
  - **Start on Boot**: Toggle automatic startup on `tty1` (`systemd` service with console kernel-log suppression, or `~/.bash_profile` fallback)
  - **Sleep Mode**: Turns off LCD backlight, blanks framebuffer, sets USB autosuspend, blocks Wi-Fi/BT, sets CPU `powersave` governor, and dims LEDs; wakes on side boot/GPIO button (or `Esc`/`Enter` fallback)
  - **Reboot & Shutdown**: Clean system restart / poweroff with inline confirmation
- **Keymap**:
  - `↑↓` (`Fn+F/X`) moves vertical views; `←→` (`Fn+Z/C`) moves horizontal tabs
  - `Enter` enters tab content (`↑↓` scrolls inside content); `Esc` returns to tab selection
  - `r` refreshes status, `?` toggles help, `q` / `Ctrl+C` quits

## Build from Source

```sh
make test        # Run unit & layout bounds tests
make run-cz      # Desktop simulator (53x14 Cardputer Zero bezel)
make build-all   # Cross-compile arm64, armv7, amd64 + SHA256 checksums
```

See [AGENTS.md](AGENTS.md) for guidelines on adding new UI tabs and features.

Created in [T3 Code](https://t3.codes).
