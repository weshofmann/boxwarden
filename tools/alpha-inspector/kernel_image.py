#!/usr/bin/env python3
"""Unpack one pinned gzip kernel into a bounded ARM64 Linux Image artifact."""

import hashlib
import os
from pathlib import Path
import sys
import zlib


MAX_COMPRESSED = 32 * 1024 * 1024
MAX_IMAGE = 128 * 1024 * 1024
ARM64_IMAGE_MAGIC = b"ARM\x64"  # little-endian 0x644d5241 at offset 0x38


def validate_image(image: bytes) -> None:
    if not 64 <= len(image) <= MAX_IMAGE or image[56:60] != ARM64_IMAGE_MAGIC:
        raise ValueError("not a bounded uncompressed ARM64 Linux Image")


def decode_kernel(compressed: bytes) -> bytes:
    if not 0 < len(compressed) <= MAX_COMPRESSED:
        raise ValueError("compressed kernel exceeds fixed limit")
    decoder = zlib.decompressobj(wbits=31)
    try:
        image = decoder.decompress(compressed, MAX_IMAGE + 1)
        image += decoder.flush()
    except zlib.error as error:
        raise ValueError("invalid gzip kernel") from error
    if not decoder.eof or decoder.unused_data or decoder.unconsumed_tail:
        raise ValueError("gzip kernel is truncated or has trailing data")
    validate_image(image)
    return image


def unpack_kernel(source: Path, output: Path, *, expected_source: str, expected_image: str) -> None:
    compressed = source.read_bytes()
    if hashlib.sha256(compressed).hexdigest() != expected_source:
        raise ValueError("compressed kernel digest mismatch")
    image = decode_kernel(compressed)
    if hashlib.sha256(image).hexdigest() != expected_image:
        raise ValueError("uncompressed kernel digest mismatch")
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "wb") as destination:
            destination.write(image)
            destination.flush()
            os.fsync(destination.fileno())
    except BaseException:
        output.unlink(missing_ok=True)
        raise


if __name__ == "__main__":
    if len(sys.argv) != 5:
        raise SystemExit("usage: kernel_image.py <gzip-vmlinuz> <kernel-image> <source-sha256> <image-sha256>")
    unpack_kernel(Path(sys.argv[1]), Path(sys.argv[2]),
                  expected_source=sys.argv[3], expected_image=sys.argv[4])
