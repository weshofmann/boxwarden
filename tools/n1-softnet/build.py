#!/usr/bin/env python3
"""Build the pinned Softnet N1 candidate with no vmnet, sudo, or Cargo test runner."""
import argparse
import hashlib
import io
import json
import os
import stat
from pathlib import Path
import subprocess
import sys
import tarfile

HERE = Path(__file__).resolve().parent
IDENTITY = json.loads((HERE / "artifact.json").read_text())


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def validate_candidate_metadata(metadata, operator_uid):
    """Reject any executable metadata inconsistent with this unprivileged stage."""
    if not stat.S_ISREG(metadata.st_mode):
        raise RuntimeError("candidate executable must be a regular file")
    if metadata.st_uid != operator_uid:
        raise RuntimeError("candidate executable must be owned by the current operator")
    if metadata.st_mode & (stat.S_ISUID | stat.S_ISGID):
        raise RuntimeError("candidate executable setuid/setgid mode prohibited")
    if stat.S_IMODE(metadata.st_mode) != 0o755 or metadata.st_nlink != 1:
        raise RuntimeError("candidate executable must be single-link mode 0755")


def validate_candidate_binary(binary):
    """Verify metadata and exact artifact bytes before any candidate execution."""
    if os.geteuid() == 0:
        raise RuntimeError("candidate verification must run without root privileges")
    binary = Path(binary).absolute()
    before = binary.lstat()
    validate_candidate_metadata(before, os.getuid())
    flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
    fd = os.open(binary, flags)
    try:
        opened = os.fstat(fd)
        validate_candidate_metadata(opened, os.getuid())
        identity = lambda x: (x.st_dev, x.st_ino, x.st_size, x.st_mtime_ns,
                              x.st_ctime_ns, x.st_mode, x.st_uid, x.st_nlink)
        if identity(before) != identity(opened):
            raise RuntimeError("candidate executable changed before hashing")
        digest = hashlib.sha256()
        while block := os.read(fd, 1024 * 1024):
            digest.update(block)
        if identity(opened) != identity(os.fstat(fd)) or identity(opened) != identity(binary.lstat()):
            raise RuntimeError("candidate executable changed during verification")
        if digest.hexdigest() != IDENTITY["executable_sha256"]:
            raise RuntimeError("candidate executable digest mismatch")
    finally:
        os.close(fd)
    return binary


def run(args, *, cwd=None, env=None):
    subprocess.run(args, cwd=cwd, env=env, check=True)


def output(args, *, cwd=None, env=None, timeout=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True, timeout=timeout).strip()


def extract_git_archive(checkout, destination):
    # Avoid inheriting upstream repository hooks/config into the build tree.
    data = subprocess.check_output(["git", "archive", "--format=tar", "HEAD"], cwd=checkout)
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
        for member in archive:
            relative = Path(member.name)
            if relative.is_absolute() or ".." in relative.parts:
                raise RuntimeError("unsafe path in pinned source archive")
            target = destination / relative
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(member) as source, target.open("wb") as sink:
                    while block := source.read(1024 * 1024):
                        sink.write(block)
                target.chmod(member.mode & 0o777)
            else:
                raise RuntimeError("unexpected non-regular source entry")


def check_cli_rejections(binary, clean):
    binary = validate_candidate_binary(binary)
    base = ["--vm-fd=-1", "--vm-mac-address=02:00:00:00:00:01"]
    cases = (
        ("missing selector", base, "N1 requires exact containment selector"),
        ("unknown selector", base + ["--block=@boxwarden-bad"], "invalid value"),
        ("duplicate selector", base + ["--block=@boxwarden-host-containment,@boxwarden-host-containment"], "N1 requires exact containment selector"),
        ("allow override", base + ["--block=@boxwarden-host-containment", "--allow=0.0.0.0/0"], "N1 requires exact containment selector"),
        ("expose override", base + ["--block=@boxwarden-host-containment", "--expose=2222:22"], "N1 requires exact containment selector"),
        ("host networking", base + ["--block=@boxwarden-host-containment", "--vm-net-type=host"], "N1 requires exact containment selector"),
    )
    for name, args, expected in cases:
        argv = [str(binary), *args]
        result = subprocess.run(argv, env=clean, text=True, capture_output=True, timeout=5)
        record = {
            "name": name, "argv": argv, "argv_count": len(argv),
            "argv_hex": [os.fsencode(value).hex() for value in argv],
            "exit": result.returncode, "stdout": result.stdout, "stderr": result.stderr,
        }
        print(json.dumps(record, sort_keys=True))
        if result.returncode == 0 or expected not in result.stderr:
            raise RuntimeError(f"candidate {name} did not fail at argument policy boundary")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--source", required=True, type=Path, help="clean pinned Softnet git checkout")
    p.add_argument("--rust-bin", required=True, type=Path, help="pinned toolchain bin directory")
    p.add_argument("--cargo-home", required=True, type=Path, help="prepopulated locked Cargo cache")
    p.add_argument("--output", required=True, type=Path, help="new result directory")
    a = p.parse_args()
    required = (
        "version", "source_commit", "patch_sha256", "policy_sha256",
        "compiler", "compiler_sha256", "cargo_sha256",
        "executable_sha256", "archive_sha256",
    )
    missing = [key for key in required if not IDENTITY.get(key)]
    if missing:
        p.error("artifact identity incomplete: " + ", ".join(missing))
    # The fixed stage path makes Cargo's local package identity independent of --output.
    # It is never adopted or deleted when already present.
    stage = Path("/private/tmp/boxwarden-n1-release-stage")
    if a.output.exists() or stage.exists():
        p.error("output or canonical stage already exists; refusing to alter existing evidence")
    if output(["git", "rev-parse", "HEAD"], cwd=a.source) != IDENTITY["source_commit"]:
        p.error("upstream source commit mismatch")
    if output(["git", "status", "--porcelain"], cwd=a.source):
        p.error("upstream source is dirty")
    for name, field in (("softnet.patch", "patch_sha256"), ("policy.rs", "policy_sha256")):
        if sha(HERE / name) != IDENTITY[field]:
            p.error(f"{name} identity mismatch")
    rustc, cargo = a.rust_bin / "rustc", a.rust_bin / "cargo"
    identity_env = {"HOME": str(a.source), "PATH": f"{a.rust_bin}:/usr/bin:/bin"}
    if sha(rustc) != IDENTITY["compiler_sha256"] or sha(cargo) != IDENTITY["cargo_sha256"]:
        p.error("Rust toolchain digest mismatch")
    if output([str(rustc), "--version"], env=identity_env, timeout=5) != IDENTITY["compiler"]:
        p.error("Rust compiler version mismatch")
    stage.mkdir()
    source = stage / "src"
    source.mkdir()
    extract_git_archive(a.source, source)
    run(["git", "apply", "--check", str(HERE / "softnet.patch")], cwd=source)
    run(["git", "apply", str(HERE / "softnet.patch")], cwd=source)
    policy = source / "lib/proxy/boxwarden_policy.rs"
    if sha(policy) != IDENTITY["policy_sha256"]:
        raise RuntimeError("patched policy differs from independently packaged source")
    home = stage / "home"
    home.mkdir()
    clean = {"HOME": str(home), "PATH": f"{a.rust_bin}:/usr/bin:/bin"}
    test_bin = stage / "policy-tests"
    run([str(rustc), "--edition=2024", "--test", str(policy), "-o", str(test_bin)], env=clean)
    run([str(test_bin)], env=clean)
    env = dict(clean)
    env.update({
        "CARGO_HOME": str(a.cargo_home), "CARGO_TARGET_DIR": str(stage / "target"),
        "RUSTC": str(rustc),
        "CARGO_ENCODED_RUSTFLAGS": "\x1f".join((
            f"--remap-path-prefix={source}=/src/softnet",
            f"--remap-path-prefix={a.cargo_home}=/cargo",
        )),
    })
    # cargo build does not invoke the upstream .cargo/config.toml sudo runner.
    run([str(cargo), "build", "--locked", "--offline", "--release"], cwd=source, env=env)
    dependencies = stage / "target/release/deps"
    pinned = list(dependencies.glob("libip_network-*.rlib"))
    if len(pinned) != 1:
        raise RuntimeError("expected exactly one locked ip_network rlib for fallback test")
    pinned_test = stage / "policy-pinned-fallback-tests"
    run([str(rustc), "--edition=2024", "--test",
         "--cfg", 'feature="pinned_fallback_test"',
         "--extern", f"ip_network={pinned[0]}",
         "-L", f"dependency={dependencies}",
         str(policy), "-o", str(pinned_test)], env=clean)
    run([str(pinned_test)], env=clean)
    binary = stage / "target/release/softnet"
    validate_candidate_binary(binary)
    if output([str(binary), "--version"], env=clean, timeout=5) != "softnet " + IDENTITY["version"]:
        raise RuntimeError("candidate --version mismatch")
    check_cli_rejections(binary, clean)
    a.output.mkdir()
    stage.rename(a.output / "build")
    binary = a.output / "build/target/release/softnet"
    archive_path = a.output / ("softnet-" + IDENTITY["version"] + ".tar")
    info = tarfile.TarInfo("softnet")
    info.size = binary.stat().st_size
    info.mode = 0o755
    info.uid = info.gid = info.mtime = 0
    info.uname = info.gname = ""
    with tarfile.open(archive_path, "w", format=tarfile.USTAR_FORMAT) as archive:
        with binary.open("rb") as file:
            archive.addfile(info, file)
    result = {"source_commit": IDENTITY["source_commit"],
              "patch_sha256": IDENTITY["patch_sha256"],
              "policy_sha256": IDENTITY["policy_sha256"],
              "compiler": IDENTITY["compiler"],
              "executable_sha256": sha(binary), "archive_sha256": sha(archive_path)}
    (a.output / "build-result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))
    if result["executable_sha256"] != IDENTITY["executable_sha256"] or result["archive_sha256"] != IDENTITY["archive_sha256"]:
        print("Build inputs verified; resulting bytes differ from staged artifact. Do not admit.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
