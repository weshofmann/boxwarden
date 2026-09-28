#!/usr/bin/env python3
"""Run only closed-environment N1 version and rejected-argument checks."""
import argparse
import json
import os
from pathlib import Path
import subprocess
from build import IDENTITY, check_cli_rejections, validate_candidate_binary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    args = parser.parse_args()
    binary = validate_candidate_binary(args.binary)
    clean = {"HOME": "/private/tmp/boxwarden-n1-build-home", "PATH": "/usr/bin:/bin"}
    argv = [str(binary), "--version"]
    result = subprocess.run(argv, env=clean, text=True, capture_output=True, timeout=5)
    print(json.dumps({"name": "version", "argv": argv, "argv_count": len(argv),
                      "argv_hex": [os.fsencode(value).hex() for value in argv],
                      "exit": result.returncode, "stdout": result.stdout,
                      "stderr": result.stderr}, sort_keys=True))
    if result.returncode != 0 or result.stdout.strip() != "softnet " + IDENTITY["version"]:
        raise RuntimeError("candidate version mismatch")
    check_cli_rejections(binary, clean)


if __name__ == "__main__":
    main()
