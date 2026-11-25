#!/usr/bin/env python3
"""Generate the nexmon "combined" firmware header from a vendor firmware blob.

nexmon's firmware Makefile extracts the RAM image and the CLM blob out of a
single C array. This tool builds that array:

    firmware bytes | zero padding to 512 | clm blob

Usage: mk-combined.py <fw.bin> <clm_blob> <out.h> <array-name>
"""
import sys


def main() -> int:
    if len(sys.argv) != 5:
        print(__doc__, file=sys.stderr)
        return 2

    fw_path, clm_path, out_path, name = sys.argv[1:]

    with open(fw_path, "rb") as f:
        fw = f.read()
    with open(clm_path, "rb") as f:
        clm = f.read()

    combined = fw + b"\x00" * ((-len(fw)) % 512) + clm

    rows = []
    for i in range(0, len(combined), 12):
        rows.append("  " + ", ".join("0x%02x" % b for b in combined[i:i + 12]) + ",")
    body = "\n".join(rows).rstrip(",")

    with open(out_path, "w") as f:
        f.write("static const unsigned char %s[] CYW43_RESOURCE_ATTRIBUTE = {\n" % name)
        f.write(body + "\n")
        f.write("};\n")
        f.write("#define CYW43_WIFI_FW_LEN (%d)\n" % len(fw))
        f.write("#define CYW43_CLM_LEN (%d)\n" % len(clm))
        f.write("const uintptr_t fw_data = (uintptr_t)&%s[0];\n" % name)

    print("wrote %s: fw %d bytes, clm %d bytes" % (out_path, len(fw), len(clm)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
