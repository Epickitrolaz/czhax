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
| **Pentest packages** | `aircrack-ng`, `hcxtools`, `hcxdumptool`, `mdk4`, `pixiewps`, `reaver`, `tshark`, `nmap`, `macchanger`, `hostapd`, `dnsmasq`, `bettercap`, `bluez`, `bluez-deprecated-tools`, `alsa-utils`, `ffmpeg`, `mpv`, `rtl-sdr`, `rtl-433`, … (only those available in your repos) |

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

- **High-Contrast Small-Screen Layout**: Built for `40×10`, `45×10`, and `53×14` terminal viewports with a left vertical view bar (`Status`/`Sts`, `WiFi`/`WiF`, `BT`, `IR`, `SDR`, `Local`/`Loc`, `Files`/`Fil`, `System`/`Sys`) and top horizontal tab bar, plus auto-dismissing **Brightness** (10 levels, `Fn+U`/`Fn+I`) and **Volume** (20 levels + Mute, `Fn+S`/`Fn+D`/`Fn+A`) OSD popups.
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
  - **Pentest actions** on the selected AP: `w` captures a WPA handshake, `d` sends deauth frames, `b` does both at once. EAPOL captures are written as radiotap pcaps to `~/czhax/captures` (requires root or passwordless sudo, a monitor-capable adapter, and nexmon firmware for injection).
  - **WPS attack (OneShot-Extended)**: press `Enter` on an AP and pick **WPS Attack (OneShot)**. OneShot-Extended is **bundled in the CZHax binary** (pinned upstream snapshot extracted to `~/.cache/czhax/oneshot-<commit>` on first use), so no separate download is needed; set `oneshot_dir` or `CZHAX_ONESHOT_DIR` to override with a local clone. Default attack is `-P -k` (Pixie Dust + kill interfering processes); presets also cover Pixie force, Null PIN, push-button, bruteforce, and a custom PIN. After each run CZHax restores NetworkManager, the interface and `rfkill` state that `--kill` terminated. If OneShot recovers a PSK, press **`c`** to connect to the AP with it after choosing an interface; if it recovers only the WPS PIN, press **`p`** to rerun the PSK capture with the saved PIN (also available as *Retry with PIN* in the WPS menu). Requires Python 3.11+, `pixiewps`, `wpa_supplicant`, `iw` and `ip`; its `vulnwsc.txt` DB drives the vulnerability colors.
  - **Network colors**: press `o` to cycle coloring modes — **SEC** (red WPA3, orange WPA2, yellow WPA/WEP, green Open) or **OSE** (green = in OneShot's vulnerable-device DB or WPS 1.0, red = WPS locked, muted = no WPS). The mode is persisted in `~/.config/czhax/config.json` as `wifi_color_mode`.
- **WiFi Spam & Evil Twin (`WiFi → Spam`, `WiFi → Evil`)**:
  - **Spam**: beacon flood driven by a single SSID, a numbered prefix (`FreeWiFi1..N`, up to 1000) or a file, on any 2.4 GHz channel, with a fresh random BSSID per beacon and a 0–200 ms delay.
  - **Evil twin**: cloned SSID on an AP-capable adapter (others are marked `(no AP)`) with optional NAT to a spare wifi, `eth0` or USB tethering uplink, plus a deauth burst and PMKID harvest *before* the AP comes up (a single radio cannot monitor and host at once).
  - **Portal** (`←/→` cycles **off → login → custom**): **login** serves the built-in sign-in page and records every submission; **custom** serves `~/czhax/user/portal.html` (falling back to the built-in page if unreadable). Submissions are appended as JSON lines to `~/czhax/captures/portal-<timestamp>.ndjson` with the client IP, method, path, user agent, the URL the client tried to reach and every submitted field — and echoed in the twin log (`portal: email,password from 192.168.66.23`).
  - **DNS** (`⏎` on the DNS row): rule editor that redirects chosen hostnames to a local server, e.g. `login.example.com=192.168.66.1`, written to dnsmasq as `address=/host/ip`. A catch-all (`address=/#/<ip>`, defaulting to the twin gateway) is always kept so every other URL still lands on the portal. `a` adds (`host=ip`, `*` = all names), `x` deletes, `Esc` returns.
  - Headless equivalents: `sudo czhax wifi spam --ssid FreeWiFi --channel 6 --random-mac` and `sudo czhax wifi evil --iface wlan0 --ssid Target --portal-mode custom --portal-file ~/czhax/user/portal.html --dns "login.test=192.168.66.1" --nat --uplink eth0`.
- **Local Scan (`Local → Scan`)**:
  - Lists WiFi (`wlan*`), Ethernet (`eth*`, `en*`), and USB tethering (`usb0`, `rndis0`, `ipheth`, `cdc_ether`, etc.) interfaces along with the connected network/subnet; disconnected interfaces are greyed out and unselectable.
  - Offers `nmap` scan presets on the selected interface's local subnet: **Host Discovery** (`-sn`), **Quick Ports** (`-F`), **Service Scan** (`-sV`), **OS & Scripts** (`-A`), **Top 1000 Ports**, and **Full Port Scan** (`-p-`), streaming live progress and parsing discovered hosts, MACs/vendors, OS, and open ports.
  - Select any discovered host to view its details or press `s` to run a targeted `nmap` scan on that host alone.
- **DHCP Starvation (`Local → DHCP`)**:
  - Exhausts a DHCP pool with raw `DHCPDISCOVER` frames, each using a fresh **random client MAC**, so the server hands out (and holds) a lease for every fake client until legitimate devices can no longer join. No external tool: the frame builder/parser is built into CZHax (`CGO_ENABLED=0`) and only needs root for `AF_PACKET`.
  - **Probe** mode sends a fixed number of requests (10–20 000) and reports the outcome; **flood** mode keeps going until stopped. Rate is adjustable 1–250 req/s and the tab shows live `sent / offers / unique / drops` counters plus every address the server offered.
  - **A full driver queue no longer aborts the run**: `EAGAIN`/`ENOBUFS` on a busy radio retries briefly, then throttles (drops are counted and reported). Wired/tethered links are preferred — a WiFi client is flagged up front because most APs/drivers rewrite or drop spoofed source MACs, which shows up as `offers=0` (the run also prints a hint when a flood gets no answers at all).
  - Requires root (`sudo` prompt) and an interface on the target segment; use it only on networks you are authorised to test. Headless: `sudo czhax local dhcp --iface eth0 --rate 25 --count 500 --random-mac`.
- **Ethernet Tap (`Local → Tap`)**:
  - Pick **two wired ports** (`Port A` = device side, `Port B` = uplink side) and CZHax builds a Linux bridge with **STP off and no addresses of its own**, so every frame is forwarded byte-for-byte. TTLs are not decremented, MACs and IPs are untouched: the tapped device cannot see the Pi in the path.
  - **Traffic recording** is built in (no `tcpdump` needed): `Record` writes a standard pcap to `~/czhax/captures/tap-<timestamp>.pcap` that Wireshark opens directly, plus a `tap-<timestamp>.log` of the decoded events. **Live counters** show packets/bytes/hosts, DNS/HTTP/TLS share and the current top talker.
  - **Passive MITM recon** reads cleartext in flight: DNS queries (`dns … asks …`), HTTP requests (`http … GET host/path`), TLS SNI (`tls … host`), Basic-auth credentials (`auth … user:pass`) and urlencoded login POSTs (`form …`), rate-limited so a busy link stays readable.
  - **DNS spoof → captive portal** (`DNS spoof` on): the tap picks a free address in the tapped subnet, answers every **IPv4/UDP** DNS `A` query with it before the real server (forged Ethernet frame, so the reply also looks like it came from the real resolver) and serves the same captive portal as the evil twin — `login` (built-in page) or `custom` (`~/czhax/user/portal.html`). Every submission is appended to `~/czhax/captures/tap-<timestamp>.ndjson` with the client IP, method, path, user agent, the URL the client tried to reach and all fields.
  - Host firewalls that drop `FORWARD` traffic cannot blackhole the bridge: the tap disables the `br_netfilter` hooks for the run (the previous values are restored on stop). If a selected port carries the Pi's own default route, the run warns that the Pi loses its uplink while tapped.
  - Connect the two ports across the cable you cut (device ↔ router/switch port). **Do not plug both ports into the same switch**: with STP off that would create a loop. A long recording is written live, so mind the SD card size on a busy link.
  - Needs root (`sudo` prompt) and two real Ethernet interfaces (USB LAN adapters work; WiFi and raw-IP `usb0` tethering are rejected up front). Stopping restores both ports, removes the bridge and deletes any stale `cztap0` left by a killed run.
  - Headless: `sudo czhax local tap --port-a eth0 --port-b eth1 --record --spoof --subnet 192.168.1.0/24 --portal login`.
- **Bluetooth Scan (`BT → Scan`)**:
  - Scans nearby Bluetooth peers on any `hci*` adapter, with `m` cycling **BLE+Classic**, **BLE**, and **Classic** discovery (`bluetoothctl`, falling back to `hcitool`).
  - Discovery keeps an interactive `bluetoothctl` session open (with a PTY fallback) because BlueZ 5.75+ exits immediately from non-interactive `scan on`; it also unblocks/powers the adapter automatically. Run `czhax bt scan --raw` to print the raw session for troubleshooting.
  - Shows device name, address, RSSI, Classic/BLE kind, Fast Pair (`FP`) advertisements with Model ID, and paired/connected state. Names come from the EIR/alias; BlueZ's auto-generated `AA-BB-CC-DD-EE-FF` aliases are replaced by the colon-form address, and RSSI is enriched via `btmgmt find` when `bluetoothctl` omits it. Press `←→` to switch adapters and `⏎` for per-device actions.
  - **Carwhisperer**: opens the hands-free RFCOMM channel (local adapter address/channel 1, exactly like the original C tool), sends `RING` **before** opening SCO, then records 8 kHz WAV audio natively (`~/czhax/captures/carwhisperer-*.wav`). The Carwhisperer menu is a checklist with a **Start** button: toggle **Tap** (record the device microphone in the same session), **Play audio** (pick a WAV/MP3 from `~/czhax/user/audio`), and/or **Record message** (record from the Cardputer mic first, then play it to the target), then Start. Temporary audio conversions live in `~/czhax/temp`. It works with Classic devices and also tries dual-mode devices that scan as BLE. Requires root or sudo — CZHax prompts for a sudo password when passwordless sudo is not configured.
  - **WhisperPair**: built-in invalid-curve Fast Pair test using `gatttool` (no external WhisperPair download); it discovers the `0xFE2C` Key-based Pairing characteristic, sends the small-order point payloads, and reports whether the device accepts the request. Falls back to an installed `whisperpair` CLI, otherwise asks for `bluez-deprecated-tools`.
  - **l2ping Flood**: floods the selected Classic device with `l2ping -f` (`bluez`); `Esc` stops any running action and keeps the captured output on screen.
- **Files (`Files → Files`)**:
  - Browses the CZHax working directory (`~/czhax` by default, configurable with `work_dir`), with folders first, sizes, and nested navigation.
  - Create text files/folders (`n`/`m`), rename (`r`), delete (`x`), and a small in-TUI text editor (`Ctrl+S` saves).
  - In-TUI image viewer for PNG/JPEG/GIF and external playback for music/video via `mpv`/`ffplay`.
  - Microphone recording (`a`) writes WAV files to `~/czhax/user/recordings`.
  - CZHax keeps user files, captures (`captures/`), Bluetooth audio (`bt/`), and recordings (`recordings/`) under the same working directory.
- **IR Remote (`IR → Remote`, `IR → Learn`, `IR → Browse`)**:
  - **Learn**: copy any physical remote button with the GPIO13 IR receiver and bind it to a Cardputer key. Learned profiles persist in `~/.config/czhax/ir.json`.
  - **Remote**: the active profile maps buttons to keys (`1` Power, `2` Vol+, `3` Vol−, `4` Mute, `5`/`6` channel, `7`–`0` arrows, `Enter` OK, `Backspace` Back); presses transmit over the GPIO12 IR LED.
  - **Browse**: load Flipper `.ir` databases (`CZHAX_IR_FLIPPER`; Flipper firmware assets or Flipper-IRDB) or Xiaomi Mi Remote exports (`CZHAX_IR_MI`; `mi_remote_database` Flipper `.ir` output (`db_export -f flipper`) or TVKILL JSON (`db_export -f tvkill`)). The Mi app DB is encrypted, so point this at the exported files rather than the raw `ir_data` SQLite file.
  - **Probe mode**: repeat `p` (power), `v` (volume) or `m` (mute) to send the next candidate; press `a` to accept and generate a profile for that device.
  - **Grid mode**: press `g`, then each mapped key sends and cycles that button's candidates, so you can walk every combination before accepting.
  - Hardware bindings: `dtoverlay=gpio-ir,gpio_pin=13,gpio_pull=up` (receiver) and `dtoverlay=gpio-ir-tx,gpio_pin=12` (transmitter); devices appear as `/dev/lirc*` and require membership in the `video` group. Run `czhax ir` to print discovered LIRC devices, capabilities and binding errors.
- **RTL-SDR (`SDR → Radio`, `SDR → Spec`, `SDR → 433`, `SDR → Srv`)**:
  - **Radio**: USB RTL2832U dongle detection (`czhax sdr` to inspect), live `WBFM`/`NFM`/`AM`/`USB`/`LSB` demodulation (`rtl_fm` → `aplay`/`pw-play`/`ffplay`/`mpv`), live audio/S-meter level bar, **Gain and Squelch adjustable while listening** (squelch 0–100, gain Auto/0–49 dB — both restart the stream in place; no extra mute, the OS volume/mute already covers that), live WAV recording to `~/czhax/captures`, built-in frequency presets (FM, NOAA WX, Airband, Marine, PMR446, FRS, 2m/70cm, ISM), and persistent bookmarks in `~/.config/czhax/sdr.json`.
  - **Channel bandwidth** is adjustable in **every** demodulation mode (`BW` row): WBFM 120–240 kHz, NFM 8–48 kHz, AM 6–24 kHz, USB/LSB 3–12 kHz. Narrow settings are upsampled in Go so they play cleanly on any audio backend, and AM/SSB get matching gain compensation.
  - **Spec**: pick an exact **Start** and **End** frequency (typed in MHz or stepped with `←/→`, **1 kHz** granularity), optionally from band presets (FM, Airband, 2m, NOAA, ISM 433, 70cm), then sweep continuously, refreshing **every 0.5 s**. The **cursor is a full-height marker** whose position is held as a frequency in the UI, so it moves on **every key press**; `Enter` tunes the Radio tab to it. Chart cells are drawn as **style runs instead of one escape sequence per cell**, so a spectrum frame is ~6× smaller on the wire and the console keeps up on the CM0 (that per-cell cost is what made the selector look like it only moved on the frame interval). **`z`/`x` zoom in and out** around the cursor (up to 256×, which also narrows the SDR's sample rate so bins get finer), **`c` cycles Contrast** — the autoscale stretch that lifts the strongest carriers to the top and pushes weak signals down — and **`m` toggles** the live **bargraph** with the **Top 50** ranked signal list. Spans up to ~3 MHz use a local FFT for noise averaging; wider spans stream `rtl_power` hop data.
  - **433**: Sub-GHz ISM packet decoder (`rtl_433`) at 433.92 / 315 / 868.3 / 915 MHz with packet detail view and optional JSONL capture logging to `~/czhax/captures`.
  - **Srv**: built-in **rtl_tcp** IQ server. Choose bind address (`0.0.0.0` for LAN or `127.0.0.1`), port, start frequency, IQ rate (1.024M / 2.048M / 2.4M) and gain; the tab shows the client connect string and live rtl_tcp log. Headless: `czhax sdr serve --bind 0.0.0.0 --port 1234 --freq 100M --rate 2.048M`. Requires `rtl-sdr` (and `rtl-433` for the 433 tab).
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
  - `r` refreshes status / rescans, `s` / `c` scan or connect in the WiFi view, `w` / `d` / `b` handshake / deauth / both, `o` cycles network color modes, `m` cycles Bluetooth scan modes, `?` toggles help, `q` / `Ctrl+C` quits
  - Files: `⏎` opens, `n` new file, `m` new folder, `a` records the mic, `x` deletes, `r` renames; the editor uses `Ctrl+S` to save
  - IR: `l` learns, `p`/`v`/`m` try power/volume/mute candidates, `a` accepts, `g` toggles grid mode, and `[`/`]` cycle profiles

## Build from Source
```sh
make test        # Run unit & layout bounds tests
make run-cz      # Desktop simulator (53x14 Cardputer Zero bezel)
make build-all   # Cross-compile arm64, armv7, amd64 + SHA256 checksums
```

## Third-party code

- [OneShot-Extended](https://github.com/chkndrp/OneShot-Extended) is bundled as a pinned upstream snapshot under `internal/oneshot/upstream` (GPL-2.0; see its `LICENSE`). It is extracted to the user cache directory on first use and executed with `python3`; CZHax itself does not modify it.
- The Bluetooth tab integrates Carwhisperer natively (RFCOMM + SCO sockets) and uses BlueZ's `l2ping`/`btmgmt`/`gatttool` from the OS `bluez` packages. The WhisperPair invalid-curve test is implemented in CZHax; no WhisperPair toolkit is downloaded or bundled.

See [AGENTS.md](AGENTS.md) for guidelines on adding new UI tabs and features.

Created in [T3 Code](https://t3.codes).
