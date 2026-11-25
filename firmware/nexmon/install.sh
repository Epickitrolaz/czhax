#!/usr/bin/env bash
#
# Install the patched CYW43439 firmware (and optionally the nexmon brcmfmac)
# on the Cardputer Zero.
#
# The stock files are copied to the FAT32 boot partition first, so they can be
# copied back from any machine that can read the SD card:
#
#   /boot/firmware/nexmon-stock/cyfmac43439-sdio.bin
#   /boot/firmware/nexmon-stock/brcmfmac.ko.xz
#   /boot/firmware/nexmon-stock/restore.sh
#   /boot/firmware/nexmon-stock/RESTORE.txt
#
# Usage: ./install.sh [--fw FILE] [--module FILE] [--no-driver|--module-only]
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KVER="$(uname -r)"
BACKUP=/boot/firmware/nexmon-stock
FW_DST=/lib/firmware/cypress/cyfmac43439-sdio.bin
MOD_DIR="/lib/modules/$KVER/kernel/drivers/net/wireless/broadcom/brcm80211/brcmfmac"
MOD_DST="$MOD_DIR/brcmfmac.ko"

FW="${FW:-$HERE/build/43439A0-7.95.75-nexmon.bin}"
MODULE="${MODULE:-$HERE/../driver/build/brcmfmac.ko}"
WITH_DRIVER=1
WITH_FW=1

while [ $# -gt 0 ]; do
	case "$1" in
	--fw) FW="$2"; shift 2 ;;
	--module) MODULE="$2"; shift 2 ;;
	--no-driver) WITH_DRIVER=0; shift ;;
	--module-only) WITH_FW=0; shift ;;
	-h|--help) sed -n '2,20p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
if [ "$WITH_FW" = 1 ]; then
	[ -r "$FW" ] || { echo "missing firmware: $FW" >&2; exit 1; }
fi
if [ "$WITH_DRIVER" = 1 ]; then
	[ -r "$MODULE" ] || { echo "missing module: $MODULE" >&2; exit 1; }
fi

# --- stock backup (once, never overwritten) -----------------------------
mkdir -p "$BACKUP"
if [ "$WITH_FW" = 1 ] && [ ! -e "$BACKUP/cyfmac43439-sdio.bin" ]; then
	cp -a "$FW_DST" "$BACKUP/cyfmac43439-sdio.bin"
	echo ">> stock firmware saved to $BACKUP/cyfmac43439-sdio.bin"
fi
if [ ! -e "$BACKUP/brcmfmac.ko.xz" ] && [ -f "$MOD_DST.xz" ]; then
	cp -a "$MOD_DST.xz" "$BACKUP/brcmfmac.ko.xz"
	echo ">> stock module saved to $BACKUP/brcmfmac.ko.xz"
fi
sync

# --- install ------------------------------------------------------------
if [ "$WITH_FW" = 1 ]; then
	install -m 644 "$FW" "$FW_DST"
	echo ">> firmware: $FW"
fi

if [ "$WITH_DRIVER" = 1 ]; then
	install -m 644 "$MODULE" "$MOD_DST"
	rm -f "$MOD_DST.xz"
	depmod -a
	echo ">> module:   $MODULE"
fi

# --- reload -------------------------------------------------------------
modprobe -r brcmfmac_cyw 2>/dev/null || true
modprobe -r brcmfmac 2>/dev/null || true
sleep 1
modprobe brcmfmac_cyw 2>/dev/null || modprobe brcmfmac
sleep 3

echo
dmesg | grep -E "brcmf_c_preinit_dcmds: Firmware:" | tail -1
echo
echo "roll back with: sudo sh $BACKUP/restore.sh"
