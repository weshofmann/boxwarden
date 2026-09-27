#!/usr/bin/env python3
"""Generate one private compile-time state-root binding for the signed runner."""

from __future__ import annotations

import base64
import os
from pathlib import Path
import re
import sys


def render_binding(state_root: str | Path, domain: str) -> str:
    root = str(state_root)
    if (not root.startswith("/") or os.path.normpath(root) != root
            or "//" in root or "\x00" in root or "\n" in root or "\r" in root):
        raise ValueError("managed state root must be clean and absolute")
    if re.fullmatch(r"[a-z][a-z0-9]{0,62}", domain) is None:
        raise ValueError("managed domain is invalid")
    encoded_root = base64.b64encode(root.encode()).decode("ascii")
    return ("import Foundation\n"
              "func managedStateRoot() -> String? {\n"
              f"    String(data: Data(base64Encoded: \"{encoded_root}\")!, encoding: .utf8)\n"
              "}\n"
              f"func managedDomain() -> String? {{ \"{domain}\" }}\n")


def write_binding(state_root: str | Path, domain: str, output: Path) -> None:
    source = render_binding(state_root, domain)
    descriptor = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as target:
        target.write(source)
        target.flush()
        os.fsync(target.fileno())


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit("usage: bind_root.py <clean-absolute-state-root> <domain> <new-private-swift-output>")
    write_binding(sys.argv[1], sys.argv[2], Path(sys.argv[3]))
