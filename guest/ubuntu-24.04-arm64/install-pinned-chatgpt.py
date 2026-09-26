#!/usr/bin/env python3
"""Install one verified official ARM64 ChatGPT package in a disposable guest."""

import hashlib
import json
import os
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.request import Request, urlopen


PACKAGE_URL = "https://persistent.oaistatic.com/codex-app-prod/linux/deb/pool/main/c/chatgpt/chatgpt_26.917.71314_arm64.deb"
PACKAGE_SHA256 = "2114883623dae34a4bc7a67faad3e6652dd9bfdc7a28f57c36ed03e350be1cf1"
PACKAGE_SIZE = 399743370
PACKAGE_VERSION = "26.917.71314"
DEFAULTS_FILE = Path("/etc/default/chatgpt")
SOURCES_FILE = Path("/etc/apt/sources.list.d/chatgpt.sources")
TEMP_ROOT = Path("/var/tmp")
RESULT_PATH = Path("/var/lib/boxwarden/chatgpt-install-result.json")
RUN_ENV = {
    "PATH": "/usr/local/bin:/usr/bin:/bin",
    "HOME": "/root",
    "LANG": "C.UTF-8",
    "LC_ALL": "C.UTF-8",
    "DEBIAN_FRONTEND": "noninteractive",
}


def fetch_verified_package(target):
    digest = hashlib.sha256()
    count = 0
    request = Request(PACKAGE_URL)
    with urlopen(request, timeout=30) as response:
        if response.url != PACKAGE_URL:
            raise RuntimeError("package source redirected away from the pinned URL")
        with target.open("xb") as output:
            while True:
                chunk = response.read(1 << 20)
                if not chunk:
                    break
                count += len(chunk)
                if count > PACKAGE_SIZE:
                    raise RuntimeError("package exceeds pinned size")
                digest.update(chunk)
                output.write(chunk)
            output.flush()
            os.fsync(output.fileno())
    if count != PACKAGE_SIZE:
        raise RuntimeError("package length differs from pin")
    if digest.hexdigest() != PACKAGE_SHA256:
        raise RuntimeError("package digest differs from pin")


def run_checked(argv, timeout, capture=True):
    return subprocess.run(argv, env=RUN_ENV, stdin=subprocess.DEVNULL,
                          stdout=subprocess.PIPE if capture else subprocess.DEVNULL,
                          stderr=subprocess.PIPE if capture else subprocess.DEVNULL,
                          text=True, check=True, timeout=timeout)


def refuse_existing_repository():
    if os.path.lexists(SOURCES_FILE):
        raise RuntimeError("ChatGPT apt source already exists in generic base")


def disable_repository_registration():
    if os.path.lexists(DEFAULTS_FILE):
        metadata = DEFAULTS_FILE.lstat()
        if not stat.S_ISREG(metadata.st_mode) or DEFAULTS_FILE.read_text() != 'repo_add_once="false"\n':
            raise RuntimeError("existing ChatGPT defaults differ from pinned build policy")
        return
    with DEFAULTS_FILE.open("x") as output:
        output.write('repo_add_once="false"\n')
        output.flush()
        os.fsync(output.fileno())
    DEFAULTS_FILE.chmod(0o644)


def record_result(stage, outcome, failure_kind=None, exit_code=None):
    # This standalone ISO helper retains only fixed stages and typed outcomes.
    # Child output, argv and exception text never enter the diagnostic record.
    result = {"version": 1, "package_sha256": PACKAGE_SHA256, "stage": stage,
              "outcome": outcome, "failure_kind": failure_kind, "exit_code": exit_code}
    raw = (json.dumps(result, sort_keys=True, separators=(",", ":")) + "\n").encode()
    fd, temporary = tempfile.mkstemp(prefix=".chatgpt-install-result-", dir=RESULT_PATH.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(raw)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, RESULT_PATH)
        directory = os.open(RESULT_PATH.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def install():
    stage = "source-policy"
    try:
        record_result(stage, "started")
        refuse_existing_repository()
        stage = "temporary-directory"
        record_result(stage, "started")
        with tempfile.TemporaryDirectory(prefix="boxwarden-chatgpt-", dir=TEMP_ROOT) as temporary:
            package = Path(temporary) / "chatgpt.deb"
            stage = "download"
            record_result(stage, "started")
            fetch_verified_package(package)
            stage = "package-identity"
            record_result(stage, "started")
            # Multi-field dpkg-deb --field output prefixes each value with its name.
            fields = run_checked(["/usr/bin/dpkg-deb", "--field", str(package),
                                  "Package", "Version", "Architecture"], 30).stdout.strip().splitlines()
            if fields != ["Package: chatgpt", "Version: " + PACKAGE_VERSION, "Architecture: arm64"]:
                raise RuntimeError("package identity differs from pin")
            stage = "repository-policy"
            record_result(stage, "started")
            disable_repository_registration()
            stage = "package-install"
            record_result(stage, "started")
            run_checked(["/usr/bin/apt-get", "install", "-y", "--no-remove", "--no-install-recommends", str(package)], 1200, capture=False)
            stage = "installed-identity"
            record_result(stage, "started")
            installed = run_checked(["/usr/bin/dpkg-query", "--show", "--showformat=${Version} ${Architecture}", "chatgpt"], 30).stdout.strip()
            if installed != PACKAGE_VERSION + " arm64":
                raise RuntimeError("installed ChatGPT identity differs from pin")
            stage = "final-source-policy"
            record_result(stage, "started")
            refuse_existing_repository()
            stage = "cleanup"
            record_result(stage, "started")
        record_result(None, "complete")
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        if isinstance(exc, subprocess.TimeoutExpired):
            kind, code = "timeout", None
        elif isinstance(exc, subprocess.CalledProcessError):
            kind, code = "nonzero", exc.returncode
        elif isinstance(exc, OSError):
            kind, code = "io-error", None
        elif isinstance(exc, RuntimeError):
            kind, code = "validation-error", None
        else:
            kind, code = "execution-error", None
        record_result(stage, "failed", kind, code)
        raise


def main():
    if len(sys.argv) != 1 or os.geteuid() != 0:
        raise RuntimeError("pinned ChatGPT installer requires guest root and no arguments")
    install()


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        print("pinned ChatGPT guest installation failed: " + str(exc), file=sys.stderr)
        sys.exit(1)
