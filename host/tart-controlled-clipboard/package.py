#!/usr/bin/env python3
"""Package an exact staged binary and public build metadata deterministically."""
import gzip
import hashlib
from pathlib import Path
import sys
import tarfile


def package(stage: Path, archive: Path) -> str:
    # Explicit whitelist: never recursively package runtime state or payloads.
    names = [
        "Tart-LICENSE", "boxwarden.entitlements", "identity.sha256",
        "signed-entitlements.plist", "signing.txt", "swift-version.txt",
        "tart-boxwarden-clipboard", "version.txt",
    ]
    for name in names:
        item = stage / name
        if not item.is_file() or item.is_symlink():
            raise ValueError(f"missing/nonregular staged artifact: {name}")
    # Exclusive creation preserves every earlier stage/package.
    with archive.open("xb") as out:
        with gzip.GzipFile(filename="", fileobj=out, mode="wb", mtime=0) as zipped:
            with tarfile.open(fileobj=zipped, mode="w", format=tarfile.USTAR_FORMAT) as tar:
                for name in sorted(names):
                    item = stage / name
                    info = tarfile.TarInfo(name)
                    info.size = item.stat().st_size
                    info.mode = 0o755 if name == "tart-boxwarden-clipboard" else 0o644
                    info.uid = info.gid = info.mtime = 0
                    info.uname = info.gname = ""
                    with item.open("rb") as source:
                        tar.addfile(info, source)
    digest = hashlib.sha256(archive.read_bytes()).hexdigest()
    archive.with_name(archive.name + ".sha256").write_text(f"{digest}  {archive.name}\n")
    return digest


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: package.py STAGE_DIRECTORY NEW_ARCHIVE.tar.gz")
    print(package(Path(sys.argv[1]), Path(sys.argv[2])))
