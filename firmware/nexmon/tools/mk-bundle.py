#!/usr/bin/env python3
"""Create a small patch bundle from a stock and a nexmon-patched firmware.

The bundle only carries the bytes the patch changes, so it can be committed
without redistributing the vendor image: the board applies it to the stock
firmware it already has.

Usage: mk-bundle.py <stock.bin> <patched.bin> <out.json>
"""
import base64
import hashlib
import json
import sys


def runs(a: bytes, b: bytes, merge_gap: int = 8):
    """Byte ranges where a and b differ, merging close neighbours."""
    if len(a) != len(b):
        raise SystemExit("size mismatch: %d != %d" % (len(a), len(b)))
    out = []
    start = None
    last = None
    for i, (x, y) in enumerate(zip(a, b)):
        if x != y:
            if start is None:
                start = i
            elif i - last > merge_gap:
                out.append((start, last + 1))
                start = i
            last = i
    if start is not None:
        out.append((start, last + 1))
    return out


def main() -> int:
    if len(sys.argv) != 4:
        print(__doc__, file=sys.stderr)
        return 2
    stock_path, patched_path, out_path = sys.argv[1:]
    stock = open(stock_path, "rb").read()
    patched = open(patched_path, "rb").read()

    chunks = []
    for off, end in runs(stock, patched):
        chunks.append([off, base64.b64encode(patched[off:end]).decode("ascii")])

    bundle = {
        "format": "czhax-nexmon-bundle/1",
        "size": len(patched),
        "stock_md5": hashlib.md5(stock).hexdigest(),
        "patched_md5": hashlib.md5(patched).hexdigest(),
        "chunks": chunks,
    }
    with open(out_path, "w") as f:
        json.dump(bundle, f, separators=(",", ":"))
        f.write("\n")

    total = sum(len(c[1]) * 3 // 4 for c in chunks)
    print("%s: %d chunks, %d bytes of payload, %d bytes JSON"
          % (out_path, len(chunks), total, len(open(out_path).read())))
    return 0


if __name__ == "__main__":
    sys.exit(main())
