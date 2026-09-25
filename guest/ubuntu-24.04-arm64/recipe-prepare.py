#!/usr/bin/env python3
"""Run one bounded reusable recipe phase inside an installer candidate guest."""

import json
import os
import re
import subprocess
import sys


PAYLOAD_PATH = "/var/lib/boxwarden/recipe-prepare.json"
READY_MARKER = "boxwarden recipe prepare complete"
MAX_PAYLOAD = 1 << 20
PACKAGE = re.compile(r"^[a-z][a-z0-9+.-]{0,127}$")
IDENTIFIER = re.compile(r"^[a-z][a-z0-9-]{0,63}$")
DIGEST = re.compile(r"^[0-9a-f]{64}$")
RUN_ENV = {
    "PATH": "/usr/local/bin:/usr/bin:/bin",
    "HOME": "/root",
    "LANG": "C.UTF-8",
    "LC_ALL": "C.UTF-8",
    "DEBIAN_FRONTEND": "noninteractive",
}


def _unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate recipe preparation field")
        result[key] = value
    return result


def parse_payload(raw):
    if not isinstance(raw, bytes) or not raw or len(raw) > MAX_PAYLOAD:
        raise ValueError("recipe preparation payload exceeds bound")
    try:
        value = json.loads(raw.decode("utf-8"), object_pairs_hook=_unique_pairs)
    except (UnicodeError, json.JSONDecodeError) as exc:
        raise ValueError("recipe preparation payload is invalid JSON") from exc
    if not isinstance(value, dict) or set(value) != {"version", "preparation_key", "apt_packages", "steps"}:
        raise ValueError("recipe preparation payload has unexpected fields")
    if type(value["version"]) is not int or value["version"] != 1:
        raise ValueError("unsupported recipe preparation version")
    if not isinstance(value["preparation_key"], str) or DIGEST.fullmatch(value["preparation_key"]) is None:
        raise ValueError("invalid recipe preparation key")
    packages = value["apt_packages"]
    steps = value["steps"]
    if not isinstance(packages, list) or len(packages) > 128 or not isinstance(steps, list) or len(steps) > 128:
        raise ValueError("recipe preparation item count exceeds bound")
    seen_packages = set()
    for package in packages:
        if not isinstance(package, str) or PACKAGE.fullmatch(package) is None or package in seen_packages:
            raise ValueError("invalid or duplicate recipe package")
        seen_packages.add(package)
    seen_steps = set()
    for step in steps:
        if not isinstance(step, dict) or set(step) != {"id", "argv"}:
            raise ValueError("recipe preparation step has unexpected fields")
        identifier = step["id"]
        argv = step["argv"]
        if not isinstance(identifier, str) or IDENTIFIER.fullmatch(identifier) is None or identifier in seen_steps:
            raise ValueError("invalid or duplicate recipe preparation step")
        seen_steps.add(identifier)
        if not isinstance(argv, list) or not 1 <= len(argv) <= 32:
            raise ValueError("recipe preparation argv count exceeds bound")
        for index, arg in enumerate(argv):
            if not isinstance(arg, str) or len(arg.encode("utf-8")) > 65536 or (index == 0 and not arg):
                raise ValueError("recipe preparation argv is invalid")
            if any(ord(char) < 32 and char not in "\n\t" or ord(char) == 127 for char in arg):
                raise ValueError("recipe preparation argv contains a control byte")
    return value


def command_plan(payload):
    commands = []
    if payload["apt_packages"]:
        commands.append(("apt-update", ["/usr/bin/apt-get", "update"]))
        commands.append(("apt-install", ["/usr/bin/apt-get", "install", "-y", "--no-remove", "--no-install-recommends", *payload["apt_packages"]]))
    commands.extend((step["id"], step["argv"]) for step in payload["steps"])
    return commands


def execute_payload(payload):
    for identifier, argv in command_plan(payload):
        try:
            completed = subprocess.run(argv, cwd="/", env=RUN_ENV, stdin=subprocess.DEVNULL,
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                       timeout=1800, check=False, start_new_session=True)
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise RuntimeError(f"recipe preparation {identifier} could not finish") from exc
        if completed.returncode != 0:
            raise RuntimeError(f"recipe preparation {identifier} exited nonzero")


def main(args):
    if len(args) == 2 and args[0] == "--check":
        with open(args[1], "rb") as file:
            parse_payload(file.read(MAX_PAYLOAD + 1))
        return 0
    if args != ["--run"] or os.geteuid() != 0:
        raise ValueError("expected root --run with fixed guest payload")
    with open(PAYLOAD_PATH, "rb") as file:
        payload = parse_payload(file.read(MAX_PAYLOAD + 1))
    execute_payload(payload)
    print(READY_MARKER, flush=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (OSError, ValueError, RuntimeError) as exc:
        print(f"boxwarden recipe prepare failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
