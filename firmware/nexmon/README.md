# Porting nexmon to the Cardputer Zero's CYW43439 (fw 7.95.75)

The Cardputer Zero (Compute Module 0) has an on-board Murata Type 1YN module:
Infineon **CYW43439** (BCM43439a0), SDIO bus, driven by `brcmfmac`.

Two things are needed for monitor mode / frame injection:

1. **A patched firmware image** — nexmon hooks `wlc_ioctl`, hides the saved
   MAC template RAM inside the patch region, and re-enables the firmware's
   monitor dispatch that the vendor build compiles out.
2. **A patched `brcmfmac`** — the stock driver only ever creates a *host-only*
   monitor netdev. nexmon's monitor netdev has to exist as a bsscfg in the
   firmware, and userspace needs a way to push raw ioctls to the firmware
   (the netlink channel).

Upstream nexmon supports this chip only for the Pico W build **7.95.49**
(`gSPI`/`sdio-g` RAM image, 224190 bytes). The CM0 runs **7.95.75**
(`43439a0-roml/sdio-g-pool-... Version: 7.95.75 (0a0104c CY)`, 229520 bytes).
The RAM layout differs, so the patch had to be re-derived.

## Status

| Piece | State |
| --- | --- |
| Firmware constants for 7.95.75 (RAM layout, patch region, ucode, template RAM, flash-patch table, version pointers, `wlc_ioctl` hook) | done, see [PORT-NOTES.md](../../docs/nexmon/PORT-NOTES.md) |
| Patched firmware builds | done |
| Patched firmware boots on the CM0, Wi-Fi still works, version string reports `nexmon.org` | done |
| `brcmfmac` 6.18 port (netlink ioctl channel + firmware-backed monitor vif + `set_monitor_channel`) | done, builds, loads, `iw list` advertises `monitor`, `iw ... interface add mon0 type monitor` succeeds |
| ROM verification (`wl_monitor` call site at `0x81F6D6`) | **verified against a live ROM dump** — `ldr r0,[r6,#8]; mov r1,sp; mov r2,r8; bl 0x818838` |
| Firmware monitor dispatch (`wl_monitor` ROM flash patch) | **done** — patch lands, frames flow |
| Monitor capture with radiotap | **working** — captured beacons/data on ch6 |
| Frame injection | **working** — `aireplay-ng --test mon0` reports 30/30 (100%) against a real AP |

### Verified on hardware

```
# ./nexctl caps
capabilities: 0x00000003 monitor radiotap

# iw dev mon0 info
	type monitor
	channel 6 (2437 MHz), width: 20 MHz

# capture on mon0
frame 6 len=244 rtap_ver=0 rtap_len=24 hdr=80000000ffffffffffff7e6ad2f4d41a...

# injection
#   ./nexctl caps
capabilities: 0x00000007 monitor radiotap injection
#   aireplay-ng --test mon0
15:51:53  46:F7:9F:4E:78:78 - channel: 6 - 'DIRECT-78-HP M207 LaserJet'
15:51:53  Ping (min/avg/max): 2.077ms/123.826ms/182.393ms Power: -88.63
15:51:53  30/30: 100%
```

### How injection is wired

The firmware side is the injection path from upstream PR #688 (written for this
chip): `NEX_INJECT_FRAME` (408) takes one radiotap-prefixed 802.11 frame,
`injection.c` parses the radiotap header and `sendframe.c` hands the bare frame
to `wlc_d11hdrs()`/`wlc_txfifo()`. The struct offsets that needs (`sk_buff.scb`,
`wlc_band.hwrs_scb`, `wlc_hw_info.up`) are applied by
`overlay/firmwares/bcm43439a0/structs.common.h.patch`.

On the driver side the stock kernel drops everything written to a monitor
interface. Here `ndo_start_xmit` copies the frame into a workqueue and issues it
as the `NEX_INJECT_FRAME` dcmd — a dcmd sleeps, so it cannot run in the transmit
softirq. That is what makes ordinary tools work:

```sh
sudo aireplay-ng --deauth 5 -a <BSSID> mon0
sudo mdk4 mon0 b -c 6
```

Because every frame is one synchronous SDIO round trip, injection is fine for
management frames (auth/deauth/probe/beacon floods at a few per second) but not
a high-rate data injector.

`nexctl txstat` reports the firmware's counters, which is how the path was
debugged:

```
queued 30  no_scb 0  wlc_down 0
wlc 0x00035350  band 0x00034a7c  hwrs_scb 0x000664ac  hw->up 3
```

Constraints found while testing:

* the interface must be brought up **before** the channel can be set, and
  cfg80211 only allows `set_monitor_channel` when no other interface is up
  (`-EBUSY` otherwise) — so `ip link set wlan0 down` first;
* bringing `mon0` up is what sets the firmware monitor iovar (`WLC_SET_MONITOR`,
  `nexctl get 107 4` then reads `2`);
* `nexctl mem` reads work for RAM and ROM, **except address `0x0`** — that read
  faults the firmware and takes the board down.

Two driver bugs found on hardware and fixed in the patch:

* `cfg->pub->mon_if` kept pointing at the monitor `brcmf_if` after the interface
  was deleted, so `brcmf_detach()` dereferenced freed memory (`NULL` +
  offset 1376) and unloading the module panicked the kernel. `brcmf_remove_interface()`
  now clears the pointer (see the `mon_if` hunk).
* the firmware-backed monitor `ifp` never got `brcmf_cfg80211_free_netdev` as its
  netdev destructor (upstream sets it in `brcmf_net_attach()`, our path uses
  `brcmf_net_mon_attach()`), so the vif stayed in `cfg->vif_list` forever. After
  the first `mon0` delete every later monitor add failed with `-EOPNOTSUPP`, which
  is what made `airmon-ng start wlan0` fail. Fixed in `brcmf_mon_add_vif`.
* the netlink handler dereferenced a stale `ndev_global` after the interface was
  removed, and never answered malformed requests, which left `nexctl` blocked
  forever. It now bails out early instead.

One firmware bug found the same way and fixed in `monitormode.c`:

* `wl_monitor_radiotap()` allocates a new skb per frame and (as in upstream
  nexmon) never releases the original. Because our hook *replaces* the ROM
  `wl_monitor()` call, nothing else frees it either, so RX drained the packet
  pool after ~30 frames and then went silent — looking exactly like "monitor
  mode works for a second, then nothing". The original skb is now freed. After
  the fix RX is continuous (~120 beacons/s, 98% of them reaching an
  `AF_PACKET` socket with an 8 MB buffer).

`airmon-ng start wlan0` now works end to end (`wlan0mon` created, radiotap
frames flowing). It prints

```
command failed: Unknown error 524 (-524)
		(mac80211 station mode vif disabled for [phy0]wlan0)
```

This is cosmetic and expected: after creating the monitor interface airmon-ng
runs `iw wlan0 del` to drop the station interface. brcmfmac's
`brcmf_cfg80211_del_iface()` refuses to delete its primary interface with
`-ENOTSUPP` (524) — `iw` has no name for that errno, hence "Unknown error".
Monitor mode is already active at that point (the line above it says the vif was
enabled) and `airmon-ng stop wlan0mon` restores the station interface cleanly.

`airmon-ng check kill` should **not** be used when another interface is carrying
your network — it kills NetworkManager and would take that link down with it.

### If the chip stops answering

After many `modprobe -r`/`modprobe` cycles on top of monitor-mode use the CYW4349
can wedge: `brcmf_attach: dongle is not responding: err=-84` with
`brcmf_sdio_txfail` lines, and no `wlan0`. This is not a bad firmware image —
reloading does not clear it, a **power cycle does**. Reboot before concluding
that a newly built image is broken.

### Building the monitor hook

`monitormode.c` installs a ROM flash patch, so the build keeps it out unless you
ask for it:

```sh
NEXMON_MONITOR=1 ./build-firmware.sh ...
```

## Safety

Nothing is written to the WiFi chip permanently. Two files on the SD card are
replaced, and the actual patch lives in the firmware image that `brcmfmac`
downloads into the chip's RAM on every load:

| file | contents |
| --- | --- |
| `/lib/firmware/cypress/cyfmac43439-sdio.bin` | firmware image (patched copy) |
| `/lib/modules/<kver>/.../brcmfmac/brcmfmac.ko` | nexmon driver (stock `.ko.xz` removed) |

The stock versions sit on the FAT32 boot partition, so they can be copied back
from any machine:

```
/boot/firmware/nexmon-stock/cyfmac43439-sdio.bin   stock firmware (md5 09e289c0...)
/boot/firmware/nexmon-stock/brcmfmac.ko.xz         stock module
/boot/firmware/nexmon-stock/restore.sh             sh /boot/firmware/nexmon-stock/restore.sh
/boot/firmware/nexmon-stock/RESTORE.txt            by-hand copy commands
```

The round trip was exercised on hardware: install → monitor works → `restore.sh`
→ stock firmware and stock module loaded again (no `nexmon` suffix in the
firmware version line) → install again.

Worst realistic failure is a firmware that panics `brcmfmac` on load, which
would boot-loop the board because WiFi is auto-loaded; that is what RESTORE.txt
is for. It cannot brick the chip: no ROM, eFUSE/OTP, calibration or bootloader
is touched, and the radio/PHY registers are never written by this patch set.

## Build and install

One command, on the board:

```sh
sudo ./provision.sh
```

It builds `brcmfmac.ko` for the running kernel and installs the patched
firmware. The firmware is either built from source (x86_64, where nexmon's
bundled toolchain runs) or applied from the prebuilt patch bundle in
`prebuilt/` — a 44 KB JSON that carries only the bytes the patch changes, so no
toolchain is needed to install it. It refuses to apply unless the input matches
the stock image it was built against.

From a workstation, build from source and finish on the board in one go:

```sh
./provision.sh --host pi@100.91.72.108
```

The workstation needs passwordless sudo on the board (`sudo -n true` must work)
and the vendor blobs are pulled from the board's stock backup automatically.

`prebuilt/` exists so an install never blocks on the toolchain. It holds:

* `bcm43439a0-7.95.75-nexmon.json` — the compiled patch as changed-byte ranges,
  applied to the stock image the board already has (md5-checked both ways);
* `armhf/libmpfr.so.4` and `armhf/libisl.so.10` — the two 32-bit ARM host
  libraries nexmon's bundled `cc1` needs and Debian 13 no longer ships, taken
  from Debian's own `libmpfr4`/`libisl10` armhf packages (see
  `prebuilt/armhf/NOTICE`).

With those two libraries in place the bundled compiler runs on the board and the
firmware is built natively: `provision.sh` probes the toolchain by compiling a
throwaway file and only falls back to the bundle if that fails. A board build
differs from an x86_64 build only in the embedded version string (27 bytes).

### Keeping the bundle in sync

`prebuilt/bcm43439a0-7.95.75-nexmon.json` is generated from a source build:

```sh
NEXMON_MONITOR=1 ./build-firmware.sh --fw <stock>.bin --clm <clm_blob> \
    --out build/43439A0.bin --bundle-out prebuilt/bcm43439a0-7.95.75-nexmon.json
```

Regenerate it whenever the patch sources change (the CI-free workflow is: build,
run the command above, commit the JSON).

Options:

```sh
--host USER@HOST   build firmware locally, driver + install on the target
--no-install       build only
--fw-only          skip the kernel module
--driver-only      skip the firmware
--no-monitor       firmware without the ROM monitor patch
--fw FILE          vendor firmware to patch
--clm FILE         CLM blob to embed
```

The stages are also usable on their own:

```sh
# firmware only (needs the vendor blobs)
./build-firmware.sh --fw  cypress/cyfmac43439-sdio.bin \
                    --clm cypress/cyfmac43439-sdio.clm_blob

# driver only (on the device, or a host with the target's kernel headers)
sudo apt install linux-source-6.18 git build-essential
./driver/build-driver.sh
```

`provision.sh` always patches a *stock* firmware: it prefers
`/boot/firmware/nexmon-stock/cyfmac43439-sdio.bin`, refuses a file that already
contains the `nexmon` version string, and accepts `--fw` to point somewhere else.
Build dependencies (`gawk`, `bison`, `flex`, `qpdf`, `xxd` and the 32-bit loader
for the toolchain) are installed automatically.

## Install / rollback

```sh
sudo ./install.sh --fw build/43439A0-7.95.75-nexmon.bin \
                  --module driver/build/brcmfmac.ko
sudo ./rollback.sh      # restores the stock firmware and module
```

`install.sh` copies the stock files to the FAT32 boot partition the first time
it runs and never touches them again:

```
/boot/firmware/nexmon-stock/cyfmac43439-sdio.bin   stock firmware
/boot/firmware/nexmon-stock/brcmfmac.ko.xz         stock module
/boot/firmware/nexmon-stock/restore.sh             run on the board
/boot/firmware/nexmon-stock/RESTORE.txt            run from a mounted SD card
```

Only those two files are modified. Nothing is written to the WiFi chip
permanently — the patch lives in the firmware image that `brcmfmac` downloads
into the chip's RAM on every load, so pulling the file (or power-cycling with
the stock one) is always enough. `RESTORE.txt` contains the by-hand copy
commands for the case where the board boot-loops and cannot be reached over the
network; the boot partition is FAT32, so any machine can read it.

Installing the patched firmware alone (without `--module`) is safe: the stock
driver still brings the interface up, it just cannot use monitor mode.

## Testing

```sh
sudo ./tools/nexctl caps                         # nexmon capability flags
sudo iw phy $(basename $(readlink /sys/class/net/wlan0/phy80211)) \
        interface add mon0 type monitor
sudo ip link set wlan0 down                      # required before set channel
sudo ip link set mon0 up                         # sets the firmware monitor iovar
sudo iw dev mon0 set channel 6
sudo tcpdump -i mon0 -e -n
```

`airmon-ng start wlan0` also works and produces `wlan0mon`; the same rules apply
(set the channel with `wlan0` down). Do not run `airmon-ng check kill` if another
interface is carrying your network.

## Layout

```
overlay/                 files dropped into a nexmon checkout
  firmwares/bcm43439a0/7_95_75_0a0104c/
  patches/bcm43439a0/7_95_75_0a0104c/
driver/                  brcmfmac 6.18 port (patch + out-of-tree Makefile)
tools/nexctl.c           netlink ioctl tool (caps/mon/get/set/mem/inject)
tools/mk-combined.py     builds nexmon's firmware+CLM C array from vendor blobs
```
