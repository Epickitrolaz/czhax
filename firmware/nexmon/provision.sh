#!/usr/bin/env bash
#
# Build + install the nexmon CYW43439 port for the Cardputer Zero.
#
# Two modes:
#
#   # from an x86_64 workstation, building the firmware here and doing the rest
#   # on the board over SSH (recommended)
#   ./provision.sh --host pi@100.91.72.108
#
#   # on the board itself (only works if nexmon's bundled toolchain can run
#   # there -- it needs libisl.so.10, which Debian 13 does not ship for armhf)
#   sudo ./provision.sh
#
# Options:
#   --host USER@HOST   build the firmware locally, build the driver remotely,
#                      install remotely (needs passwordless sudo on the target)
#   --no-install       build only
#   --fw-only          skip the kernel module
#   --driver-only      skip the firmware
#   --no-monitor       build the firmware without the ROM monitor patch
#   --fw FILE          vendor firmware to patch (default: the stock backup)
#   --clm FILE         CLM blob to embed
#   --work DIR         build directory (default: /root/nexmon-work or ~/nexmon-work)
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="Epickitrolaz/czhax"
FW_ASSET="czhax-firmware-bcm43439a0-7.95.75.bin"
WORK="${WORK:-}"
NEXMON_SRC="${NEXMON_SRC:-}"
HOST=""
DO_FW=1
DO_DRV=1
DO_INSTALL=1
MONITOR="${NEXMON_MONITOR:-1}"
FW_IN=""
CLM_IN=""

usage() { sed -n '2,26p' "$0"; }

while [ $# -gt 0 ]; do
	case "$1" in
	--host) HOST="$2"; shift 2 ;;
	--no-install) DO_INSTALL=0; shift ;;
	--fw-only) DO_DRV=0; shift ;;
	--driver-only) DO_FW=0; shift ;;
	--no-monitor) MONITOR=0; shift ;;
	--fw) FW_IN="$2"; shift 2 ;;
	--clm) CLM_IN="$2"; shift 2 ;;
	--work) WORK="$2"; shift 2 ;;
	-h|--help) usage; exit 0 ;;
	*) echo "unknown argument: $1" >&2; usage; exit 2 ;;
	esac
done

log() { printf '\n\033[1m>> %s\033[0m\n' "$*"; }
if [ -z "$WORK" ]; then
	if [ "$(id -u)" = 0 ]; then WORK=/root/nexmon-work; else WORK="$HOME/nexmon-work"; fi
fi
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
warn() { printf 'warning: %s\n' "$*" >&2; }

SSH_OPTS=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new)
# optional: SSH_IDENTITY=/path/to/key
[ -n "${SSH_IDENTITY:-}" ] && SSH_OPTS+=(-i "$SSH_IDENTITY")

# ---------------------------------------------------------------- inputs ---
# The firmware we patch must be the *vendor* one. Prefer the stock copy kept on
# the boot partition; never re-patch our own output.
STOCK_FW=/boot/firmware/nexmon-stock/cyfmac43439-sdio.bin
INSTALLED_FW=/lib/firmware/cypress/cyfmac43439-sdio.bin
CLM=/lib/firmware/cypress/cyfmac43439-sdio.clm_blob

is_patched() { grep -qa "nexmon" "$1" 2>/dev/null; }

if [ -z "$FW_IN" ]; then
	for cand in "$STOCK_FW" "$INSTALLED_FW"; do
		if [ -f "$cand" ] && ! is_patched "$cand"; then
			FW_IN="$cand"
			break
		fi
	done
fi
if [ -z "$FW_IN" ] && [ -n "$HOST" ]; then
	# workstation mode: pull the stock blob off the board
	tmp="$(mktemp)"
	ssh "${SSH_OPTS[@]}" "$HOST" "sudo cat $STOCK_FW 2>/dev/null || cat $INSTALLED_FW" > "$tmp"
	if is_patched "$tmp"; then
		rm -f "$tmp"
		die "the board only has a patched firmware; pass the vendor blob with --fw"
	fi
	FW_IN="$tmp"
fi
[ -n "$FW_IN" ] || die "no stock firmware found (use --fw /path/to/cyfmac43439-sdio.bin)"
[ -r "$FW_IN" ] || die "cannot read $FW_IN"
if [ -z "$CLM_IN" ]; then
	if [ -r "$CLM" ]; then
		CLM_IN="$CLM"
	elif [ -n "$HOST" ]; then
		CLM_IN="$(mktemp)"
		ssh "${SSH_OPTS[@]}" "$HOST" "cat $CLM" > "$CLM_IN"
	else
		die "no CLM blob found (use --clm)"
	fi
fi
[ -r "$CLM_IN" ] || die "cannot read $CLM_IN"

mkdir -p "$WORK"

# ---------------------------------------------------------- dependencies ---
# The firmware build needs: gawk (linker script generation), bison/flex (b43
# assembler), qpdf for zlib-flate (ucode compression), xxd, and the 32-bit
# loader for nexmon's bundled toolchain.
need_apt=0
for t in git gcc make python3 xxd patch gawk bison flex zlib-flate; do
	command -v "$t" >/dev/null 2>&1 || need_apt=1
done
case "$(uname -m)" in
	x86_64|i686|i386) [ -e /lib/ld-linux.so.2 ] || need_apt=1; libs="libc6:i386 zlib1g:i386"; farch=i386 ;;
	aarch64|armv7l)
		[ -e /lib/ld-linux-armhf.so.3 ] || need_apt=1
		libs="libc6:armhf zlib1g:armhf"
		if [ "$DO_FW" = 1 ]; then
			libs="$libs libgmp10:armhf libmpc3:armhf libstdc++6:armhf"
		fi
		farch=armhf
		;;
	*) die "unsupported host $(uname -m)" ;;
esac
if [ "$need_apt" = 1 ]; then
	SUDO=""
	[ "$(id -u)" = 0 ] || SUDO="sudo"
	log "installing build dependencies"
	$SUDO dpkg --print-foreign-architectures | grep -qx "$farch" || $SUDO dpkg --add-architecture "$farch"
	$SUDO apt-get update -qq
	$SUDO apt-get install -y -qq git build-essential python3 xxd patch bison flex gawk \
		qpdf zlib1g-dev $libs >/dev/null
fi

# ---------------------------------------------------------- nexmon source ---
if [ "$DO_FW" = 0 ]; then
	NEXMON=""
elif [ -n "$NEXMON_SRC" ]; then
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
	[ "$HAVE" = "$PIN" ] || die "$NEXMON is at $HAVE, expected $PIN"
fi

# -------------------------------------------------------------- firmware ---
OUT_FW="$WORK/out/43439A0-7.95.75-nexmon.bin"
# A prebuilt bundle carries only the bytes our patch changes, so the firmware
# can always be installed even where the toolchain cannot run.
BUNDLE="$HERE/prebuilt/bcm43439a0-7.95.75-nexmon.json"
# nexmon's bundled toolchain is a 32-bit ARM host binary on arm64 boards; its
# cc1 needs mpfr (ABI 4) and isl 0.10, which Debian 13 does not ship. We carry
# both here so the firmware can still be built natively on the board.
HOST_LIBS="$HERE/prebuilt/armhf"

case "$(uname -m)" in
	x86_64|i686|i386) TC=gcc-arm-none-eabi-5_4-2016q2-linux-x86 ;;
	aarch64|armv7l|armv6l) TC=gcc-arm-none-eabi-5_4-2016q2-linux-armv7l ;;
	*) TC="" ;;
esac

toolchain_runs() {
	[ -n "$TC" ] && [ -x "$NEXMON/buildtools/$TC/bin/arm-none-eabi-gcc" ] || return 1
	printf 'int x;\n' | "$NEXMON/buildtools/$TC/bin/arm-none-eabi-gcc" -x c -c -o /dev/null - >/dev/null 2>&1
}

if [ "$DO_FW" = 1 ] && [ -d "$HOST_LIBS" ]; then
	export LD_LIBRARY_PATH="$HOST_LIBS${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi

# Prebuilt firmware from the GitHub release: no toolchain, no vendor blob of
# its own. Verified against the release's checksums.txt.
fetch_release_firmware() {
	local tag ver base tmp want got
	tag="$(curl -fsSL --retry 3 -o /dev/null -w '%{url_effective}' \
		"https://github.com/${REPO}/releases/latest" 2>/dev/null || true)"
	ver="${tag##*/}"
	case "$ver" in ""|latest|releases) return 1 ;; esac
	base="https://github.com/${REPO}/releases/download/${ver}"
	tmp="$(mktemp -d)"
	if ! curl -fsSL --retry 3 "${base}/${FW_ASSET}" -o "$tmp/fw.bin" 2>/dev/null; then
		rm -rf "$tmp"; return 1
	fi
	if ! curl -fsSL --retry 3 "${base}/checksums.txt" -o "$tmp/sums" 2>/dev/null; then
		rm -rf "$tmp"; return 1
	fi
	want="$(grep " ${FW_ASSET}\$" "$tmp/sums" | awk '{print $1}')"
	got="$(sha256sum "$tmp/fw.bin" | awk '{print $1}')"
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		warn "release firmware checksum mismatch, ignoring it"
		rm -rf "$tmp"; return 1
	fi
	mkdir -p "$(dirname "$OUT_FW")"
	mv "$tmp/fw.bin" "$OUT_FW"
	rm -rf "$tmp"
	log "downloaded $FW_ASSET from release $ver (sha256 ${got%${got#????????????}})"
	return 0
}

if [ "$DO_FW" = 1 ]; then
	have_fw=0

	# Prefer the firmware published with the release (checksum verified); no
	# toolchain, no vendor blob, nothing compiled. NEXMON_BUILD=1 forces a
	# source build instead, which is what patch development needs.
	if [ "${NEXMON_BUILD:-0}" != 1 ] && fetch_release_firmware; then
		have_fw=1
	fi

	if [ "$have_fw" = 0 ] && [ -n "$NEXMON" ] && toolchain_runs; then
		log "building the patched firmware on this machine (monitor hook: $MONITOR)"
		if NEXMON_MONITOR="$MONITOR" "$HERE/build-firmware.sh" \
			--nexmon "$NEXMON" --fw "$FW_IN" --clm "$CLM_IN" \
			--work "$WORK/fw" --out "$OUT_FW"; then
			have_fw=1
		else
			warn "firmware build failed, will try the patch bundle"
		fi
	fi

	if [ "$have_fw" = 0 ]; then
		[ -f "$BUNDLE" ] || die "no release firmware, no toolchain and no $BUNDLE"
		log "applying the prebuilt patch bundle (no toolchain needed)"
		mkdir -p "$(dirname "$OUT_FW")"
		python3 "$HERE/tools/apply-bundle.py" "$FW_IN" "$BUNDLE" "$OUT_FW"
	fi
fi

# ------------------------------------------------- native driver + install ---
if [ -z "$HOST" ]; then
	OUT_MOD="$WORK/out/brcmfmac.ko"
	if [ "$DO_DRV" = 1 ]; then
		log "building brcmfmac for $(uname -r)"
		"$HERE/driver/build-driver.sh" --work "$WORK/drv" --out "$OUT_MOD"
	fi
	if [ "$DO_INSTALL" = 1 ]; then
		[ "$(id -u)" = 0 ] || die "installing needs root (or pass --no-install)"
		args=()
		if [ "$DO_FW" = 1 ]; then
			args+=(--fw "$OUT_FW")
		else
			args+=(--module-only)
		fi
		if [ "$DO_DRV" = 1 ]; then
			args+=(--module "$OUT_MOD")
		fi
		log "installing"
		"$HERE/install.sh" "${args[@]}"
	fi
	echo
	echo "artifacts in $WORK/out"
	exit 0
fi

# --------------------------------------------------- remote driver + install ---
log "deploying to $HOST"
STAGE=/tmp/nexmon-deploy
ssh "${SSH_OPTS[@]}" "$HOST" "rm -rf $STAGE && mkdir -p $STAGE/stage"
tar czf - -C "$HERE" . | ssh "${SSH_OPTS[@]}" "$HOST" "tar xzf - -C $STAGE/stage"
if [ "$DO_FW" = 1 ]; then
	ssh "${SSH_OPTS[@]}" "$HOST" "cat > $STAGE/fw.bin" < "$OUT_FW"
	echo ">> shipped firmware $(md5sum < "$OUT_FW" | cut -d' ' -f1)"
fi

if [ "$DO_DRV" = 1 ]; then
	log "building brcmfmac on $HOST"
	ssh "${SSH_OPTS[@]}" "$HOST" \
		"sudo $STAGE/stage/driver/build-driver.sh --work /root/nexmon-work/drv --out $STAGE/brcmfmac.ko"
fi

if [ "$DO_INSTALL" = 1 ]; then
	log "installing on $HOST"
	IARGS=""
	if [ "$DO_FW" = 1 ]; then
		IARGS="$IARGS --fw $STAGE/fw.bin"
	else
		IARGS="$IARGS --module-only"
	fi
	if [ "$DO_DRV" = 1 ]; then IARGS="$IARGS --module $STAGE/brcmfmac.ko"; fi
	ssh "${SSH_OPTS[@]}" "$HOST" "sudo $STAGE/stage/install.sh $IARGS"
fi

echo
echo "done"
