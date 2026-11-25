#!/usr/bin/env bash
#
# Build the nexmon firmware patch for the Cardputer Zero's CYW43439 (BCM43439a0)
# running firmware 7.95.75 (0a0104c CY) in SDIO mode.
#
# This clones seemoo-lab/nexmon at a pinned commit, drops our port on top of it
# and builds the patched firmware image. Everything it needs from the vendor is
# the stock firmware blob and its CLM blob, taken from the device:
#
#   /lib/firmware/cypress/cyfmac43439-sdio.bin
#   /lib/firmware/cypress/cyfmac43439-sdio.clm_blob
#
# Usage:
#   ./build-firmware.sh [--fw PATH] [--clm PATH] [--work DIR] [--out FILE]
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

# nexmon commit this port was developed against
NEXMON_URL="https://github.com/seemoo-lab/nexmon.git"
NEXMON_COMMIT="68720406f54ff16cad28fd3c51f2392aefad9fb7"

FW_VER_DIR="7_95_75_0a0104c"
COMBINED="w43439A0_7_95_75_00_combined.h"

FW_PATH="${FW_PATH:-/lib/firmware/cypress/cyfmac43439-sdio.bin}"
CLM_PATH="${CLM_PATH:-/lib/firmware/cypress/cyfmac43439-sdio.clm_blob}"
WORK="${WORK:-$HOME/nexmon-build}"
OUT="${OUT:-$HERE/build/43439A0-7.95.75-nexmon.bin}"
NEXMON_OVERRIDE=""

while [ $# -gt 0 ]; do
	case "$1" in
	--fw) FW_PATH="$2"; shift 2 ;;
	--clm) CLM_PATH="$2"; shift 2 ;;
	--work) WORK="$2"; shift 2 ;;
	--out) OUT="$2"; shift 2 ;;
	--nexmon) NEXMON_OVERRIDE="$2"; shift 2 ;;
	-h|--help) sed -n '2,20p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

for f in "$FW_PATH" "$CLM_PATH"; do
	[ -r "$f" ] || { echo "missing input: $f" >&2; exit 1; }
done

mkdir -p "$WORK"

# 1. nexmon source -------------------------------------------------------
if [ -n "$NEXMON_OVERRIDE" ]; then
	NEXMON="$NEXMON_OVERRIDE"
	[ -d "$NEXMON/patches" ] || { echo "not a nexmon checkout: $NEXMON" >&2; exit 1; }
	echo ">> using nexmon at $NEXMON"
else
	NEXMON="$WORK/nexmon"
	if [ ! -d "$NEXMON/.git" ]; then
		echo ">> cloning nexmon"
		git clone "$NEXMON_URL" "$NEXMON"
	fi
	git -C "$NEXMON" fetch --all --quiet
	git -C "$NEXMON" checkout --quiet "$NEXMON_COMMIT"
fi

# 2. toolchain + build tools --------------------------------------------
# nexmon ships 32-bit arm-none-eabi toolchains; pick the one for this host.
#   x86_64 hosts need the i386 loader:
#     sudo dpkg --add-architecture i386 && sudo apt install libc6:i386 zlib1g:i386
#   arm64 hosts run the armv7l build and need the armhf loader:
#     sudo dpkg --add-architecture armhf && sudo apt install libc6:armhf zlib1g:armhf
case "$(uname -m)" in
	x86_64|i686|i386) TC=gcc-arm-none-eabi-5_4-2016q2-linux-x86 ;;
	aarch64|armv7l|armv6l) TC=gcc-arm-none-eabi-5_4-2016q2-linux-armv7l ;;
	*) echo "error: unsupported host $(uname -m)" >&2; exit 1 ;;
esac
export PATH="$NEXMON/buildtools/$TC/bin:$PATH"
if ! arm-none-eabi-gcc --version >/dev/null 2>&1; then
	echo "error: the bundled $TC toolchain cannot run on this host." >&2
	echo "       install the 32-bit loader libraries (see the comment above)." >&2
	exit 1
fi

echo ">> building nexmon buildtools"
make -s -C "$NEXMON/buildtools" >/dev/null

# 3. overlay our port ----------------------------------------------------
echo ">> overlaying the $FW_VER_DIR port"
cp -r "$HERE/overlay/." "$NEXMON/"

# the injection path needs corrected struct offsets (sk_buff.scb, wlc_band,
# wlc_hw_info.up) on top of nexmon's shared structs.common.h
if ! grep -q "struct wlc_band" "$NEXMON/firmwares/bcm43439a0/structs.common.h"; then
	( cd "$NEXMON" && patch -p1 --quiet < "$HERE/overlay/firmwares/bcm43439a0/structs.common.h.patch" )
fi

# register the firmware version id (idempotent)
if ! grep -q "FW_VER_7_95_75_0a0104c" "$NEXMON/patches/include/firmware_version.h"; then
	sed -i 's|^#define FW_VER_7_95_49_2271bb6 .*|&\n#define FW_VER_7_95_75_0a0104c             911|' \
		"$NEXMON/patches/include/firmware_version.h"
fi

# 4. regenerate the firmware header from the vendor blob -----------------
FWDIR="$NEXMON/firmwares/bcm43439a0/$FW_VER_DIR"
python3 "$HERE/tools/mk-combined.py" "$FW_PATH" "$CLM_PATH" "$FWDIR/$COMBINED" \
	w43439A0_7_95_75_00_combined

# 5. build ---------------------------------------------------------------
echo ">> extracting firmware, ucode and flash patches"
( cd "$NEXMON" && source setup_env.sh && make -s -C "firmwares/bcm43439a0/$FW_VER_DIR" )

echo ">> building the patched firmware (monitor hook: ${NEXMON_MONITOR:-0})"
( cd "$NEXMON" && source setup_env.sh && make -C "patches/bcm43439a0/$FW_VER_DIR/nexmon" NEXMON_MONITOR="${NEXMON_MONITOR:-0}" )

mkdir -p "$(dirname "$OUT")"
cp "$NEXMON/patches/bcm43439a0/$FW_VER_DIR/nexmon/43439A0.bin" "$OUT"

echo
echo "patched firmware: $OUT"
ls -l "$OUT"
echo "vendor firmware:  $(ls -l "$FW_PATH" | awk '{print $5}') bytes (must match)"
