#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Create synthetic PAT, PMT, NIT and SDT packets for the HTTP smoke test."""
import sys


def crc32_mpeg(data):
    crc = 0xFFFFFFFF
    for byte in data:
        crc ^= byte << 24
        for _ in range(8):
            crc = ((crc << 1) ^ (0x04C11DB7 if crc & 0x80000000 else 0)) & 0xFFFFFFFF
    return crc.to_bytes(4, "big")


def section(table_id, body):
    length = len(body) + 4
    data = bytes([table_id, 0xB0 | (length >> 8), length & 255]) + body
    return data + crc32_mpeg(data)


def packet(pid, payload, continuity):
    assert len(payload) <= 184
    return bytes([0x47, 0x40 | (pid >> 8), pid & 255, 0x10 | continuity]) + payload + bytes([0xFF]) * (184 - len(payload))


def main():
    pat = section(0x00, bytes.fromhex("0001c100000065e100"))
    pmt = section(0x02, bytes.fromhex("0065c10000e101f0001be101f000"))
    nit = section(0x40, bytes.fromhex("0001c10000f000f00600010001f000"))
    name, provider = b"Synthetic", b"Test"
    descriptor = bytes([0x48, 3 + len(provider) + len(name), 1, len(provider)]) + provider + bytes([len(name)]) + name
    service = bytes.fromhex("0065ff") + bytes([0x80 | (len(descriptor) >> 8), len(descriptor) & 255]) + descriptor
    sdt = section(0x42, bytes.fromhex("0001c100000001ff") + service)
    with open(sys.argv[1], "wb") as output:
        for index in range(64):
            cc = index % 16
            output.write(packet(0, b"\0" + pat, cc))
            output.write(packet(0x10, b"\0" + nit, cc))
            output.write(packet(0x100, b"\0" + pmt, cc))
            output.write(packet(0x11, b"\0" + sdt, cc))
            output.write(packet(0x1FFF, bytes([0xFF]) * 184, cc))


if __name__ == "__main__":
    main()
