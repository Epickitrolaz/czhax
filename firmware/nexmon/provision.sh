#!/usr/bin/env bash
#
# One-shot build + install of the nexmon CYW43439 port for the Cardputer Zero.
#
# Builds the patched firmware and the patched brcmfmac for the running kernel,
# then installs both (keeping the stock files on the FAT32 boot partition).
#
# Run on the board:
#
#   sudo ./provision.sh                # build firmware + driver, install
#   sudo ./provision.sh --no-install   # just build
#   sudo ./provision.sh --driver-only  # rebuild the kernel module only
#   sudo ./provision.sh --fw-only      # rebuild the firmware only
#
# The first run clones nexmon (~2 GB, it ships three cross toolchains) into
# $WORK/nexmon; later runs reuse it. Point NEXMON_SRC at an existing checkout to
# skip the clone.
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORK="${WORK:-/root/nexmon-work}"
NEXMON_SRC="${NEXMON_SRC:-}"

DO_FW=1
DO_DRV=1
DO_INSTALL=1
MONITOR="${NEXMON_MONITOR:-1}"
FW_IN=""
CLM_IN=""

usage() { sed -n '2,20p' "$0"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--no-install) DO_INSTALL=0; shift ;;
	--driver-only) DO_FW=0; shift ;;
	--fw-only) DO_DRV=0; shift ;;
	--no-monitor) MONITOR=0; shift ;;
	--fw) FW_IN="$2"; shift 2 ;;
	--clm) CLM_IN="$2"; shift 2 ;;
	--work) WORK="$2"; shift 2 ;;
	-h|--help) usage; exit 0 ;;
	*) echo "unknown argument: $1" >&2; usage; exit 2 ;;
	esac
done

log() { printf '\n\033[1m>> %s\033[0m\n' "$*"; }

# ---------------------------------------------------------------- inputs ---
# The firmware we patch must be the *vendor* one. Prefer the stock copy kept on
# the boot partition; never re-patch our own output.
STOCK_FW=/boot/firmware/nexmon-stock/cyfmac43439-sdio.bin
INSTALLED_FW=/lib/firmware/cypress/cyfmac43439-sdio.bin
CLM=/lib/firmware/cypress/cyfmac43439-sdio.clm_blob

is_patched() { grep -qa "nexmon" "$1" 2>/dev/null; }

if [ -z "$FW_IN" ]; then
	if [ -f "$STOCK_FW" ] && ! is_patched "$STOCK_FW"; then
		FW_IN="$STOCK_FW"
	elif [ -f "$INSTALLED_FW" ] && ! is_patched "$INSTALLED_FW"; then
		FW_IN="$INSTALLED_FW"
	else
		echo "error: no stock firmware found to patch." >&2
		echo "       looked at $STOCK_FW and $INSTALLED_FW" >&2
		echo "       pass the vendor blob with --fw /path/to/cyfmac43439-sdio.bin" >&2
		exit 1
	fi
fi
[ -n "$CLM_IN" ] || CLM_IN="$CLM"
[ -r "$CLM_IN" ] || { echo "error: missing CLM blob $CLM_IN" >&2; exit 1; }

mkdir -p "$WORK"

# ------------------------------------------------------------ build deps ---
missing=""
for t in git gcc make python3 xxd patch; do
	command -v "$t" >/dev/null 2>&1 || missing="$missing $t"
done
if [ -n "$missing" ]; then
	log "installing build dependencies:$missing"
	apt-get update -qq
	apt-get install -y -qq git build-essential python3 xxd patch bison flex gawk \
		qpdf zlib1g-dev libc6:armhf zlib1g:armhf >/dev/null
fi

# ----------------------------------------------------------- nexmon tree ---
if [ -n "$NEXMON_SRC" ]; then
	NEXMON="$NEXMON_SRC"
	log "using nexmon from $NEXMON"
else
	NEXMON="$WORK/nexmon"
	PIN="$(sed -n 's/^NEXMON_COMMIT="\(.*\)"/\1/p' "$HERE/build-firmware.sh")"
	if [ ! -d "$NEXMON/.git" ]; then
		log "cloning nexmon at $PIN (~2 GB, one time)"
		rm -rf "$NEXMON"
		mkdir -p "$NEXMON"
		git -C "$NEXMON" init -q
		git -C "$NEXMON" remote add origin https://github.com/seemoo-lab/nexmon.git
		if git -C "$NEXMON" fetch --depth 1 -q origin "$PIN"; then
			git -C "$NEXMON" checkout -q FETCH_HEAD
		else
			log "shallow fetch failed, doing a full clone"
			rm -rf "$NEXMON"
			git clone -q https://github.com/seemoo-lab/nexmon.git "$NEXMON"
			git -C "$NEXMON" checkout -q "$PIN"
		fi
	fi
	HAVE="$(git -C "$NEXMON" rev-parse HEAD 2>/dev/null || echo unknown)"
	if [ "$HAVE" != "$PIN" ]; then
		echo "error: $NEXMON is at $HAVE, expected $PIN" >&2
		echo "       remove it, or pass NEXMON_SRC=<checkout at $PIN>" >&2
		exit 1
	fi
fi

# ------------------------------------------------------------- firmware ---
OUT_FW="$WORK/out/43439A0-7.95.75-nexmon.bin"
if [ "$DO_FW" = 1 ]; then
	log "building the patched firmware (monitor hook: $MONITOR)"
	NEXMON_MONITOR="$MONITOR" \
		"$HERE/build-firmware.sh" --nexmon "$NEXMON" --fw "$FW_IN" --clm "$CLM_IN" \
			--work "$WORK/fw" --out "$OUT_FW"
fi

# --------------------------------------------------------------- driver ---
OUT_MOD="$WORK/out/brcmfmac.ko"
if [ "$DO_DRV" = 1 ]; then
	log "building brcmfmac for $(uname -r)"
	WORK="$WORK/drv" OUT="$OUT_MOD" "$HERE/driver/build-driver.sh" --out "$OUT_MOD"
fi

# -------------------------------------------------------------- install ---
if [ "$DO_INSTALL" = 1 ]; then
	[ "$(id -u)" = 0 ] || { echo "error: installing needs root (or pass --no-install)" >&2; exit 1; }
	log "installing"
	args=(--fw "$OUT_FW")
	if [ "$DO_DRV" = 1 ]; then
		args+=(--module "$OUT_MOD")
	fi
	"$HERE/install.sh" "${args[@]}"
	echo
	echo "firmwares built in $WORK/out"
else
	echo
	echo "build only; artifacts in $WORK/out"
fi
