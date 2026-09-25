#!/usr/bin/env python3
"""Request ChatGPT launch from the workstation's active user desktop manager."""

import os
import stat
import subprocess
import sys
from pathlib import Path


RUNTIME_ROOT = Path("/run/user")
RUN_ENV = {
    "PATH": "/usr/local/bin:/usr/bin:/bin",
    "HOME": "/home/boxwarden",
    "USER": "boxwarden",
    "LOGNAME": "boxwarden",
    "LANG": "C.UTF-8",
    "LC_ALL": "C.UTF-8",
}


def desktop_environment():
    uid = os.geteuid()
    if uid == 0:
        raise RuntimeError("graphical launch must run as the workstation user")
    runtime = RUNTIME_ROOT / str(uid)
    try:
        details = runtime.lstat()
    except OSError as exc:
        raise RuntimeError("workstation runtime directory is unavailable") from exc
    if not stat.S_ISDIR(details.st_mode) or details.st_uid != uid or stat.S_IMODE(details.st_mode) != 0o700:
        raise RuntimeError("workstation runtime directory is unavailable or unsafe")
    bus = runtime / "bus"
    try:
        details = bus.lstat()
    except OSError as exc:
        raise RuntimeError("workstation session bus is unavailable") from exc
    if not stat.S_ISSOCK(details.st_mode) or details.st_uid != uid:
        raise RuntimeError("workstation session bus is unavailable or unsafe")
    environment = dict(RUN_ENV)
    environment["XDG_RUNTIME_DIR"] = str(runtime)
    environment["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + str(bus)
    return environment


def run_checked(argv, environment, capture=False):
    return subprocess.run(argv, env=environment, stdin=subprocess.DEVNULL,
                          stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL, text=True, timeout=15, check=False)


def launch():
    environment = desktop_environment()
    graphical = run_checked(["/usr/bin/systemctl", "--user", "is-active", "--quiet",
                             "graphical-session.target"], environment)
    if graphical.returncode != 0:
        raise RuntimeError("workstation graphical session is not active")
    manager = run_checked(["/usr/bin/systemctl", "--user", "show-environment"],
                          environment, capture=True)
    if manager.returncode != 0 or len(manager.stdout) > 16384:
        raise RuntimeError("graphical session environment is unavailable")
    if not any(line.startswith(("DISPLAY=", "WAYLAND_DISPLAY=")) and line.partition("=")[2]
               for line in manager.stdout.splitlines()):
        raise RuntimeError("graphical display is absent from the user manager")
    current = run_checked(["/usr/bin/systemctl", "--user", "is-active", "--quiet",
                           "boxwarden-chatgpt.service"], environment)
    if current.returncode == 0:
        return
    started = run_checked([
        "/usr/bin/systemd-run", "--user", "--collect", "--service-type=exec",
        "--unit=boxwarden-chatgpt", "--property=PartOf=graphical-session.target",
        "/usr/bin/chatgpt",
    ], environment)
    if started.returncode != 0:
        raise RuntimeError("ChatGPT user service did not start")


def main():
    if len(sys.argv) != 1:
        raise RuntimeError("graphical ChatGPT launcher takes no arguments")
    launch()


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        print("ChatGPT graphical launch request failed: " + str(exc), file=sys.stderr)
        sys.exit(1)
