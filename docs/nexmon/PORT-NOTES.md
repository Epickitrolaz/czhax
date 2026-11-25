# How the 7.95.75 firmware constants were derived

Target: `cyfmac43439-sdio.bin`, 229520 bytes, `43439a0-roml/sdio-g-pool-p2p-... Version: 7.95.75 (0a0104c CY)`.
Reference: nexmon's `bcm43439a0/7_95_49_2271bb6` (Pico W, 224190 bytes).

Both images are the RAM part of the firmware, loaded at `0x0`; ROM lives at
`0x800000` and is not part of the image. The two builds share almost all code,
so every constant was located by matching the *surrounding bytes* of the
corresponding 7.95.49 constant, then checking that the semantics still hold.

| Constant | 7.95.49 | 7.95.75 | how it was found / why it is right |
| --- | --- | --- | --- |
| `HNDRTE_RECLAIM_0_END_PTR` | `0x1888` = `0x36A8C` | `0x18C4` = `0x37F5C` | the pointer sits in a config block whose tail is the literal sequence `fc, 100, 0, 0`; the block around it is byte-identical |
| `HNDRTE_RECLAIM_0_END` | `0x36A8C` | `0x37F5C` | value stored at the pointer above; equals template-RAM start + `0x640` |
| `TEMPLATERAMSTART_PTR` | `0x2B77C` = `0x3644C` | `0x2C5D4` = `0x3791C` | 48-byte context match (`60334b02 63c8f44f bdf8602b <ptr> 6fc0460b ...`) |
| `TEMPLATERAMSTART` | `0x3644C` | `0x3791C` | value at the pointer; immediately follows the ucode blob |
| `TEMPLATERAMSIZE` | `0x640` | `0x640` | `reclaim_end - templateram_start` in both images |
| ucode size word | `[0x2B878]` = `0xABCD` | `[0x2C6D0]` = `0xB248` | the word directly in front of the ucode blob holds its size |
| `UCODESTART` | `0x2B87C` | `0x2C6D4` | code at `0x22824` loads `0x2C6D0`/`0x2C6D4` from its literal pool, mirroring `0x216B4`/`0x216B8` in 7.95.49 |
| `UCODESIZE` | `0xABCD` | `0xB248` | `ucode_start + size == templateram_start` |
| `WLC_UCODE_WRITE_BL_HOOK_ADDR` | `0x216A8` (`f02c da3c`) | `0x2282C` (`f02b d97a`) | the `BL wlc_ucode_write` in `wlc_ucode_download`, matched by the identical prologue and literal pool |
| `PATCHSIZE` / `PATCHSTART` | `0x2000` / `0x34A8C` | `0x2000` / `0x35F5C` | `PATCHSTART = reclaim_end - PATCHSIZE` |
| `FP_DATA_END_PTR` | `0x15CA0` = `0xA60` | `0x16FE8` = `0xA88` | same table entry: an fp-data end value followed by `0x13880` and `"sdpcmed"` |
| `FP_CONFIG_BASE_PTR_1/END_PTR_1` | `0x16F80`/`0x16F7C` | `0x182C8`/`0x182C4` | word pairs holding `(0x1000, table_end)`, at the same offset from the `sdt_...` strings |
| `FP_CONFIG_BASE_PTR_2/END_PTR_2` | `0x171F8`/`0x171F4` | `0x18540`/`0x1853C` | second copy, `+0x278` from the first in both images |
| `FP_CONFIG_ORIGBASE/ORIGEND` | `0x1000`/`0x1390` | `0x1000`/`0x13CC` | the vendor flash-patch table (`{target,size,data}` triples, 76 vs 81 entries) |
| `VERSION_PTR_1/2/3` | `0x4DEC`/`0x1E3AC`/`0x18194` | `0x37C8`/`0x1F6D8`/`0x194DC` | the four references to the version string; nexmon patches three of them, the same three were chosen (the `DEBU`-prefixed one is skipped, as upstream does) |
| `wlc_ioctl` hook site | `0x1EFC4` = `0x81A3C9` | `0x2024C` = `0x81A3C9` | identical value **and** identical 32-byte context: both firmwares call the same ROM `wlc_ioctl` |

Cross-checks after building the patched image:

* the generated flash-patch table at `0x35F5C` reproduces the vendor's 81 entries
  byte for byte;
* the compressed ucode (25264 bytes) inflates back to the extracted ucode
  (`52160` bytes) exactly;
* `templateram (0x640) + compressed ucode (0x62B0)` ends at `0x32FC4`, below
  `FP_CONFIG_BASE = 0x3535C` and `PATCHSTART = 0x35F5C`;
* on hardware the chip boots the patched image, the driver reports
  `version 7.95.75 (0a0104c CY nexmon.org: ...)` and a scan still returns
  access points.

## What is still version-dependent

`monitormode.c` hooks ROM at `0x81F6D6` with a flash patch — that address comes
from the Pico W 7.95.49 port and cannot be checked against the firmware image,
because it is ROM. It was verified against a live dump read back from the chip:

```
81f6d0: ldr r0, [r6, #8]
81f6d2: mov r1, sp
81f6d4: mov r2, r8
81f6d6: bl  0x818838          ; wl_monitor(wl, sts, p)
```

and after booting the patched image the same address holds a branch into the
patch region (`16 f4 03 f5`), i.e. the flash patch engine applied it.

Supporting evidence that the ROM is otherwise identical:

* 20+ flash-patch targets in the two vendor tables are exactly the same
  (`0x8117D8`, `0x82141C`, `0x823828`, `0x83666C`, `0x83ADAC`, `0x840808`,
  `0x824C0C`, `0x84E384`, `0x84EC18`, `0x85190C`, `0x86C084`, `0x856644`,
  `0x82A210`, `0x853B00`, `0x85B684`, `0x85B6E8`, …);
* the `wlc_ioctl` entry point resolves to the same ROM address `0x81A3C9`;
* the injection path's ROM symbols (`wlc_d11hdrs` 0x8929FC, `wlc_get_txh_info`
  0x82B8FC, `wlc_txfifo` 0x834194) work unmodified on this firmware.

Still version-specific are only the RAM-side constants in the table above and the
three struct offsets for injection, which are checked at runtime through
`nexctl txstat` (they resolve to sane heap pointers, and injected frames reach
the air).

## Injection specifics

`sendframe()` gates on `wlc->band->hwrs_scb` and `wlc->hw->up`; the counters
reported by `nexctl txstat` distinguish "queued" from "no scb" and "wlc down".
A frame only leaves the radio once `wlc_txfifo()` has been reached, so the
`queued` counter is the useful signal when debugging. The frames are transmitted
with the radiotap RATE field (500 kbps units) as the ratespec override, so a
frame without a RATE field is sent with ratespec 0.
