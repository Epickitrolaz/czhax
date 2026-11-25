#!/usr/bin/env bash
#
# Restore the stock CYW43439 firmware and the stock brcmfmac module from the
# backup that install.sh keeps on the FAT32 boot partition.
#
# Works on the running system, and also from a mounted SD card:
#   sudo sh rollback.sh ROOT=/mnt/root
#
set -euo pipefail

BACKUP="${BACKUP:-/boot/firmware/nexmon-stock}"
ROOT="${ROOT:-}"

[ -f "$BACKUP/cyfmac43439-sdio.bin" ] || {
	echo "no backup in $BACKUP" >&2
	echo "if the card is mounted elsewhere, pass ROOT=/mnt/root and unpack the boot partition to \$ROOT/boot/firmware" >&2
	exit 1
}

KVER="${KVER:-$(uname -r)}"
FW_DST="$ROOT/lib/firmware/cypress/cyfmac43439-sdio.bin"
MOD_DIR="$ROOT/lib/modules/$KVER/kernel/drivers/net/wireless/broadcom/brcm80211/brcmfmac"

install -m 644 "$BACKUP/cyfmac43439-sdio.bin" "$FW_DST"
echo ">> restored $FW_DST"

if [ -f "$BACKUP/brcmfmac.ko.xz" ] && [ -d "$MOD_DIR" ]; then
	rm -f "$MOD_DIR/brcmfmac.ko"
	install -m 644 "$BACKUP/brcmfmac.ko.xz" "$MOD_DIR/brcmfmac.ko.xz"
	echo ">> restored $MOD_DIR/brcmfmac.ko.xz"
fi

if [ -z "$ROOT" ]; then
	depmod -a
	modprobe -r brcmfmac_cyw 2>/dev/null || true
	modprobe -r brcmfmac 2>/dev/null || true
	sleep 1
	modprobe brcmfmac_cyw 2>/dev/null || modprobe brcmfmac || true
	sleep 3
	dmesg | grep -E "brcmf_c_preinit_dcmds: Firmware:" | tail -1
fi

echo "rollback done"
