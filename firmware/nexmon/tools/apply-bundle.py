#!/usr/bin/env python3
"""Apply a nexmon patch bundle to a stock firmware image.

Usage: apply-bundle.py <stock.bin> <bundle.json> <out.bin>

The bundle is refused unless the input matches the stock image it was built
from, so a different vendor firmware can never be silently corrupted.
"""
import base64
import hashlib
import json
import sys


def main() -> int:
    if len(sys.argv) != 4:
        print(__doc__, file=sys.stderr)
        return 2
    stock_path, bundle_path, out_path = sys.argv[1:]
    stock = bytearray(open(stock_path, "rb").read())
    bundle = json.load(open(bundle_path))

    if bundle.get("format") != "czhax-nexmon-bundle/1":
        print("error: unknown bundle format %r" % bundle.get("format"), file=sys.stderr)
        return 1
    md5 = hashlib.md5(stock).hexdigest()
    if md5 != bundle["stock_md5"]:
        print("error: stock firmware does not match the bundle", file=sys.stderr)
        print("  bundle expects %s" % bundle["stock_md5"], file=sys.stderr)
        print("  input is        %s" % md5, file=sys.stderr)
        return 1
    if len(stock) != bundle["size"]:
        print("error: unexpected firmware size %d (bundle: %d)"
              % (len(stock), bundle["size"]), file=sys.stderr)
        return 1

    for off, b64 in bundle["chunks"]:
        data = base64.b64decode(b64)
        stock[off:off + len(data)] = data

    got = hashlib.md5(stock).hexdigest()
    if got != bundle["patched_md5"]:
        print("error: patched image checksum mismatch (%s != %s)"
              % (got, bundle["patched_md5"]), file=sys.stderr)
        return 1

    with open(out_path, "wb") as f:
        f.write(stock)
    print("patched %s -> %s (%d chunks, md5 %s)"
          % (stock_path, out_path, len(bundle["chunks"]), got))
    return 0


if __name__ == "__main__":
    sys.exit(main())
