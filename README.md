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
| **Wi-Fi monitor mode** | [`firmware/nexmon`](firmware/nexmon): patched CYW43439 firmware + nexmon `brcmfmac` — monitor mode with radiotap and frame injection (`airmon-ng`, `aireplay-ng`, `mdk4` all work). Builds the firmware from source on x86_64, and on the board applies the prebuilt patch bundle to the stock image (no toolchain needed) |
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

- **High-Contrast Small-Screen Layout**: Built for `40×10`, `45×10`, and `53×14` terminal viewports with a left vertical view bar (`Status`/`Sts`, `WiFi`/`WiF`, `IR`, `System`/`Sys`) and top horizontal tab bar.
- **Device Status (`Status → Device`)**:
  - Hostname & hardware/arch (`CardZero CM0` detection)
  - Primary IP / link state
  - RAM & Disk usage bars
  - Battery percentage & **estimated time remaining until battery runs out** (via Linux `power_supply` fuel-gauge `time_to_empty_*` or charge/energy rate)
  - CPU temperature, 1m load, and uptime
- **WiFi Scan (`WiFi → Scan`)**:
  - Scan nearby networks on any detected wireless interface (`nmcli`, falling back to `iw`)
  - Signal, security, channel, and in-use state per AP, with a detail view
  - Connect to open, WPA/WPA2/WPA3, or hidden networks; pick the target interface and reveal the passphrase while typing
  - **Pentest actions** on the selected AP: `w` captures a WPA handshake, `d` sends deauth frames, `b` does both at once. EAPOL captures are written as radiotap pcaps to `~/czhax-captures` (requires root or passwordless sudo, a monitor-capable adapter, and nexmon firmware for injection).
  - **WPS attack (OneShot-Extended)**: press `Enter` on an AP and pick **WPS Attack (OneShot)**. OneShot-Extended is **bundled in the CZHax binary** (pinned upstream snapshot extracted to `~/.cache/czhax/oneshot-<commit>` on first use), so no separate download is needed; set `oneshot_dir` or `CZHAX_ONESHOT_DIR` to override with a local clone. Default attack is `-P -k` (Pixie Dust + kill interfering processes); presets also cover Pixie force, Null PIN, push-button, bruteforce, and a custom PIN. After each run CZHax restores NetworkManager, the interface and `rfkill` state that `--kill` terminated. If OneShot recovers a PSK, press **`c`** to connect to the AP with it after choosing an interface; if it recovers only the WPS PIN, press **`p`** to rerun the PSK capture with the saved PIN (also available as *Retry with PIN* in the WPS menu). Requires Python 3.11+, `pixiewps`, `wpa_supplicant`, `iw` and `ip`; its `vulnwsc.txt` DB drives the vulnerability colors.
  - **Network colors**: press `o` to cycle coloring modes — **SEC** (red WPA3, orange WPA2, yellow WPA/WEP, green Open) or **OSE** (green = in OneShot's vulnerable-device DB or WPS 1.0, red = WPS locked, muted = no WPS). The mode is persisted in `~/.config/czhax/config.json` as `wifi_color_mode`.
- **IR Remote (`IR → Remote`, `IR → Learn`, `IR → Browse`)**:
  - **Learn**: copy any physical remote button with the GPIO13 IR receiver and bind it to a Cardputer key. Learned profiles persist in `~/.config/czhax/ir.json`.
  - **Remote**: the active profile maps buttons to keys (`1` Power, `2` Vol+, `3` Vol−, `4` Mute, `5`/`6` channel, `7`–`0` arrows, `Enter` OK, `Backspace` Back); presses transmit over the GPIO12 IR LED.
  - **Browse**: load Flipper `.ir` databases (`CZHAX_IR_FLIPPER`; Flipper firmware assets or Flipper-IRDB) or Xiaomi Mi Remote exports (`CZHAX_IR_MI`; `mi_remote_database` Flipper `.ir` output (`db_export -f flipper`) or TVKILL JSON (`db_export -f tvkill`)). The Mi app DB is encrypted, so point this at the exported files rather than the raw `ir_data` SQLite file.
  - **Probe mode**: repeat `p` (power), `v` (volume) or `m` (mute) to send the next candidate; press `a` to accept and generate a profile for that device.
  - **Grid mode**: press `g`, then each mapped key sends and cycles that button's candidates, so you can walk every combination before accepting.
  - Hardware bindings: `dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up` (receiver) and `dtoverlay=gpio-ir-tx,gpio_pin=12` (transmitter); devices appear as `/dev/lirc*` and require membership in the `video` group. Run `czhax ir` to print discovered LIRC devices, capabilities and binding errors.
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
  - `r` refreshes status / rescans, `s` / `c` scan or connect in the WiFi view, `w` / `d` / `b` handshake / deauth / both, `o` cycles network color modes, `?` toggles help, `q` / `Ctrl+C` quits
  - IR: `l` learns, `p`/`v`/`m` try power/volume/mute candidates, `a` accepts, `g` toggles grid mode, and `[`/`]` cycle profiles

## Build from Source
```sh
make test        # Run unit & layout bounds tests
make run-cz      # Desktop simulator (53x14 Cardputer Zero bezel)
make build-all   # Cross-compile arm64, armv7, amd64 + SHA256 checksums
```

## Third-party code

- [OneShot-Extended](https://github.com/chkndrp/OneShot-Extended) is bundled as a pinned upstream snapshot under `internal/oneshot/upstream` (GPL-2.0; see its `LICENSE`). It is extracted to the user cache directory on first use and executed with `python3`; CZHax itself does not modify it.

See [AGENTS.md](AGENTS.md) for guidelines on adding new UI tabs and features.

Created in [T3 Code](https://t3.codes).
