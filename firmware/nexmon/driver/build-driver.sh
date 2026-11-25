#!/usr/bin/env bash
#
# Build nexmon's brcmfmac for the *running* Raspberry Pi kernel (6.18.y).
#
# The stock driver refuses to create a monitor interface backed by the
# firmware, so the nexmon firmware patch is useless without it. This script
# takes the pristine brcmfmac sources that ship with the running kernel
# (linux-source package), applies firmware/nexmon/driver/nexmon-brcmfmac-6.18.patch
# and builds brcmfmac.ko out of tree.
#
# The rest of the driver package (brcmfmac-cyw.ko, brcmutil) is left untouched:
# the patched module exports exactly the same symbols and CRCs.
#
# Usage: ./build-driver.sh [--work DIR] [--src TARBALL_OR_DIR]
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="${WORK:-$HOME/nexmon-driver}"
SRC="${SRC:-}"
KVER="$(uname -r)"
OUT="${OUT:-}"

while [ $# -gt 0 ]; do
	case "$1" in
	--work) WORK="$2"; shift 2 ;;
	--src) SRC="$2"; shift 2 ;;
	--out) OUT="$2"; shift 2 ;;
	-h|--help) sed -n '2,20p' "$0"; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

KDIR="/lib/modules/$KVER/build"
[ -d "$KDIR" ] || { echo "missing kernel headers: $KDIR" >&2; exit 1; }

# find the kernel sources
if [ -z "$SRC" ]; then
	for c in /usr/src/linux-source-*.tar.xz /usr/src/linux-source-*.tar.gz; do
		[ -e "$c" ] && SRC="$c" && break
	done
fi
[ -n "$SRC" ] || {
	echo "no kernel sources found; install them:" >&2
	echo "  sudo apt install linux-source-$(uname -r | cut -d. -f1-2)" >&2
	exit 1
}

mkdir -p "$WORK/src"
if [ -d "$SRC" ]; then
	DRVSRC="$SRC/drivers/net/wireless/broadcom/brcm80211"
else
	if [ ! -d "$WORK/src/brcm80211" ]; then
		echo ">> extracting brcm80211 from $SRC"
		TMP="$(mktemp -d)"
		case "$SRC" in
		*.xz) tar -xJf "$SRC" -C "$TMP" --wildcards '*/drivers/net/wireless/broadcom/brcm80211/*' ;;
		*.gz) tar -xzf "$SRC" -C "$TMP" --wildcards '*/drivers/net/wireless/broadcom/brcm80211/*' ;;
		esac
		mv "$TMP"/*/drivers/net/wireless/broadcom/brcm80211 "$WORK/src/brcm80211"
		rm -rf "$TMP"
	fi
	DRVSRC="$WORK/src/brcm80211"
fi

[ -d "$DRVSRC/brcmfmac" ] || { echo "no brcmfmac sources in $DRVSRC" >&2; exit 1; }

# work on a copy so reruns are deterministic
rm -rf "$WORK/build"
mkdir -p "$WORK/build"
cp -r "$DRVSRC/brcmfmac" "$DRVSRC/include" "$DRVSRC/brcmutil" "$WORK/build/"

echo ">> applying the nexmon patch"
( cd "$WORK/build" && patch -p1 --dry-run < "$HERE/nexmon-brcmfmac-6.18.patch" >/dev/null \
	&& patch -p1 < "$HERE/nexmon-brcmfmac-6.18.patch" )

cp "$HERE/Makefile.brcmfmac" "$WORK/build/brcmfmac/Makefile"
cp "$HERE/nexmon_ioctls.h" "$WORK/build/brcmfmac/"

echo ">> building brcmfmac.ko for $KVER"
make -C "$KDIR" M="$WORK/build/brcmfmac" modules -j1

echo
echo "module: $WORK/build/brcmfmac/brcmfmac.ko"
ls -l "$WORK/build/brcmfmac/brcmfmac.ko"

if [ -n "$OUT" ]; then
	mkdir -p "$(dirname "$OUT")"
	cp "$WORK/build/brcmfmac/brcmfmac.ko" "$OUT"
	echo "copied to $OUT"
fi
