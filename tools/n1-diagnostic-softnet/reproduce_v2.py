#!/usr/bin/env python3
"""Reproduce published diagnostic Rust source offline; never install or run a proxy.

An artifact identity is a result, not an input to this first reproduction.
Each leg needs a new output, an empty target, and the same fresh staging pathname.
The caller archives a completed stage by rename; failed stages are never adopted.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import plistlib
import shutil
import stat
import subprocess
import tarfile
import time

HERE = Path(__file__).resolve().parent
SOURCE_COMMIT = "85bcb9aebffcc4a8c98b1bec111df2db8c18e81f"
UPSTREAM_COMMIT = "df84a30016e3d6acc0d30acc660cf3a726f42a9b"
SOURCE_MANIFEST_SHA = "c820aad5292d0a8cda78d57a3a87072b0e7cac67a1499d13436eb6c7854346c5"
VERSION = "0.19.0-boxwarden-n1-diagnostic.2"
VOLUME_UUID = "A178510A-D5EC-4495-828B-BD5445E2B66D"
IMAGE_PATH = "/Volumes/DevelData/boxwarden/alpha-qualification-state.sparsebundle"
MOUNT = "/Volumes/BoxwardenAlphaQualification"
COMPILER = "rustc 1.98.1 (48a229cea 2026-09-01)"
RUST_SHA = "766eda9d8f53afd6fc7f27b3cd2e444dd22afacb5afa710a5625fc8e45b8c941"
CARGO_SHA = "6e17e865f3a20dd55a1d212f849f58b77124179f0de7c52973096d84ba34118d"


def sha(path):
    with Path(path).open("rb") as stream:
        h = hashlib.sha256()
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def retain(path, data):
    path = Path(path)
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    if path.read_bytes() != data:
        raise RuntimeError("retained evidence readback mismatch")


def retain_json(path, value):
    retain(path, (json.dumps(value, indent=2, sort_keys=True) + "\n").encode())


def argv_record(argv, env, cwd):
    return {"argv": argv, "argv_count": len(argv),
            "argv_hex": [os.fsencode(v).hex() for v in argv],
            "environment": env, "cwd": str(cwd)}


def transform_tool(name, tool, args, config, environment):
    """Bind deterministic flags at the actual selected child exec boundary."""
    argv = [tool, *args]
    child_env = dict(environment)
    if name == "ld":
        if any(v == "-oso_prefix" or v.startswith("-oso_prefix=") for v in args):
            raise RuntimeError("preexisting linker OSO prefix override refused")
        argv = [tool, "-oso_prefix", config["target"], *args]
    elif name == "ar":
        child_env["ZERO_AR_DATE"] = "1"
    return argv, child_env


def validate_binary_metadata(metadata, operator_uid):
    if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != operator_uid or \
            stat.S_IMODE(metadata.st_mode) != 0o755 or metadata.st_nlink != 1:
        raise RuntimeError("binary must be operator-owned single-link regular0755")


def verify_binary(path, digest):
    if os.geteuid() == 0:
        raise RuntimeError("unprivileged verification only")
    path = Path(path)
    before = path.lstat()
    validate_binary_metadata(before, os.getuid())
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        opened = os.fstat(fd)
        validate_binary_metadata(opened, os.getuid())
        identity = lambda s: (s.st_dev, s.st_ino, s.st_mode, s.st_uid,
                              s.st_gid, s.st_nlink, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        if identity(before) != identity(opened):
            raise RuntimeError("binary changed before verification")
        h = hashlib.sha256()
        for block in iter(lambda: os.read(fd, 1024 * 1024), b""):
            h.update(block)
        if h.hexdigest() != digest or identity(opened) != identity(os.fstat(fd)) or \
                identity(opened) != identity(path.lstat()):
            raise RuntimeError("binary digest or identity changed")
    finally:
        os.close(fd)


def validate_mount(disk, images):
    if disk.get("VolumeUUID") != VOLUME_UUID:
        raise RuntimeError("exact mounted volume UUID required")
    stores = {v["APFSPhysicalStore"] for v in disk["APFSPhysicalStores"]}
    required = stores | {disk["DeviceIdentifier"]}
    if not any(image.get("image-path") == IMAGE_PATH and
               image.get("image-encrypted") is True and
               required <= {v.get("dev-entry", "").removeprefix("/dev/")
                            for v in image["system-entities"]}
               for image in images["images"]):
        raise RuntimeError("exact current encrypted backing-image association required")


def mount_proof():
    clean = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL": "C", "LANG": "C"}
    disk = plistlib.loads(subprocess.check_output(
        ["/usr/sbin/diskutil", "info", "-plist", MOUNT], env=clean, timeout=15))
    images = plistlib.loads(subprocess.check_output(
        ["/usr/bin/hdiutil", "info", "-plist"], env=clean, timeout=15))
    validate_mount(disk, images)
    return {"volume_uuid": disk["VolumeUUID"], "device": disk["DeviceIdentifier"],
            "physical_stores": disk["APFSPhysicalStores"], "image": IMAGE_PATH,
            "image_encrypted": True}


def check_boundary(config):
    proof = mount_proof()
    for path, digest in config["pinned_files"].items():
        if sha(path) != digest:
            raise RuntimeError("compiler input changed: " + path)
    if str(Path(config["sdk_path"]).resolve()) != config["sdk_resolved"]:
        raise RuntimeError("selected SDK path changed")
    return proof


def write_archive(binary, destination):
    info = tarfile.TarInfo("softnet")
    info.size = Path(binary).stat().st_size
    info.mode = 0o755
    info.uid = info.gid = info.mtime = 0
    info.uname = info.gname = ""
    with Path(destination).open("xb") as sink:
        with tarfile.open(fileobj=sink, mode="w", format=tarfile.USTAR_FORMAT) as archive:
            with Path(binary).open("rb") as source:
                archive.addfile(info, source)
        sink.flush()
        os.fsync(sink.fileno())
    with tarfile.open(destination, "r:") as archive:
        members = archive.getmembers()
        if len(members) != 1 or members[0].name != "softnet" or \
                archive.extractfile(members[0]).read() != Path(binary).read_bytes():
            raise RuntimeError("archive readback mismatch")


def inventory(root):
    files = {}
    for p in sorted(Path(root).rglob("*")):
        if p.is_symlink():
            raise RuntimeError("unapproved build-input symlink: " + str(p))
        if p.is_file():
            files[str(p.relative_to(root))] = sha(p)
        elif not p.is_dir():
            raise RuntimeError("special build input")
    return files


def validate_retention_devices(stage_device, output_device):
    if stage_device != output_device:
        raise RuntimeError("stage retention requires the same filesystem before build")


def validate_inventory(observed, expected):
    if observed != expected:
        raise RuntimeError("immutable build input inventory changed")


def extract_archive(data, destination):
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
        for member in archive:
            name = Path(member.name)
            if name.is_absolute() or ".." in name.parts:
                raise RuntimeError("unsafe source archive path")
            target = destination / name
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            elif member.isfile():
                target.parent.mkdir(parents=True, exist_ok=True)
                retain(target, archive.extractfile(member).read())
                target.chmod(member.mode & 0o777)
            else:
                raise RuntimeError("source archive nonregular entry")


def run_recorded(argv, env, cwd, output, label, boundary=None, timeout=1800):
    proof = check_boundary(boundary) if boundary else mount_proof()
    retain_json(output / (label + "-invocation.json"),
                dict(argv_record(argv, env, cwd), boundary=proof, child_umask=0o022))
    log = output / (label + ".txt")
    with log.open("xb") as sink:
        os.chmod(log, 0o600)
        result = subprocess.run(argv, env=env, cwd=cwd, stdout=sink,
                                stderr=subprocess.STDOUT, timeout=timeout, umask=0o022)
        sink.flush()
        os.fsync(sink.fileno())
    retain_json(output / (label + "-result.json"),
                {"exit_code": result.returncode, "log_sha256": sha(log)})
    if result.returncode:
        raise RuntimeError(label + " failed; preserve stage/target/output, do not retry")
    return log.read_text()


WRAPPER = '''#!/usr/bin/python3
import importlib.util,json,os,sys,time
from pathlib import Path
config=json.loads(Path(os.environ["N1_BUILD_BOUNDARY"]).read_text())
spec=importlib.util.spec_from_file_location("recipe",config["recipe"])
b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)
name=Path(sys.argv[0]).name
if name=="rustc-wrapper":
    if sys.argv[1]!=config["tools"]["rustc"]: raise RuntimeError("foreign rustc")
    argv=sys.argv[1:]
else:
    argv=[config["tools"][name],*sys.argv[1:]]
    if name=="clang":
        argv += ["-isysroot",config["sdk_path"]]
        if not any(v in argv for v in ("-c","-E","-S","-fsyntax-only")):
            argv += ["-fuse-ld="+str(Path(sys.argv[0]).with_name("ld"))]
argv,child_env=b.transform_tool(name,argv[0],argv[1:],config,dict(os.environ))
proof=b.check_boundary(config)
record=dict(b.argv_record(argv,child_env,os.getcwd()),tool=name,boundary=proof)
record["tool_sha256"]=b.sha(argv[0])
fd=os.open(config["argv_log"],os.O_WRONLY|os.O_APPEND)
try:
    data=(json.dumps(record,sort_keys=True)+"\\n").encode()
    import fcntl
    fcntl.flock(fd,fcntl.LOCK_EX);os.write(fd,data);os.fsync(fd)
finally: os.close(fd)
os.execve(argv[0],argv,child_env)
'''


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ("inputs", "target", "stage", "output", "native-input"):
        p.add_argument("--" + name, required=True, type=Path)
    a = p.parse_args()
    if os.geteuid() == 0:
        p.error("unprivileged builds only")
    proof = mount_proof()
    external = Path(MOUNT) / "n1build-20260930"
    if a.target not in [external / "rust-target-9", external / "rust-target-10"] or \
            not a.target.is_dir() or list(a.target.iterdir()):
        p.error("target must be one approved independent empty reproduction directory")
    if a.stage != external / "task5-prearm-stage-r2" or a.stage.exists() or a.output.exists():
        p.error("fresh fixed external stage and new output required")
    leg = a.target.name.removeprefix("rust-target-")
    if a.output != external / "outputs" / ("task5-prearm-reproduction-" + leg):
        p.error("exact same-volume per-leg retention output required")
    for parent in (external, external / "outputs"):
        info = parent.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
            p.error("external build/output parent must be direct operator-owned private0700")
    validate_retention_devices(a.stage.parent.stat().st_dev, a.output.parent.stat().st_dev)
    if sha(HERE / "source-manifest.json") != SOURCE_MANIFEST_SHA:
        p.error("frozen source manifest changed")
    source_manifest = json.loads((HERE / "source-manifest.json").read_text())
    for name, digest in source_manifest["files"].items():
        if sha(HERE / name) != digest:
            p.error("frozen Rust source changed: " + name)
    repo = HERE.parent.parent
    clean = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL": "C", "LANG": "C"}
    # Compare frozen source to its published identity, not mutable HEAD or primary checkout.
    source_archive = subprocess.check_output(["/usr/bin/git", "archive", SOURCE_COMMIT,
                                             "tools/n1-diagnostic-softnet"], cwd=repo, env=clean)
    with tarfile.open(fileobj=io.BytesIO(source_archive)) as archive:
        for member in archive:
            if member.isfile() and (repo / member.name).read_bytes() != archive.extractfile(member).read():
                p.error("source differs from published commit")
    upstream = a.inputs / "softnet-source"
    if subprocess.check_output(["/usr/bin/git", "rev-parse", "HEAD"], cwd=upstream, env=clean).decode().strip() != UPSTREAM_COMMIT or \
            subprocess.check_output(["/usr/bin/git", "status", "--porcelain"], cwd=upstream, env=clean):
        p.error("clean pinned upstream checkout required")
    if sha(a.native_input) != "73cc3b2f38d978b9ab63046c89eea610ff26ab467f2353e064bdcfa18465f36e":
        p.error("reviewed selected native compiler/SDK input record required")
    for name, key in (("softnet.patch", "canonical_patch_sha256"), ("policy.rs", "canonical_policy_sha256")):
        if sha(repo / "tools/n1-softnet" / name) != source_manifest[key]:
            p.error("canonical source input changed: " + name)
    native = json.loads(a.native_input.read_text())
    tools = {name: value["path"] for name, value in native["resolved_tools"].items()}
    tools.update(rustc=str(a.inputs / "toolchain/bin/rustc"), cargo=str(a.inputs / "toolchain/bin/cargo"))
    pins = {value["path"]: value["sha256"] for value in native["resolved_tools"].values()}
    pins.update({tools["rustc"]: RUST_SHA, tools["cargo"]: CARGO_SHA})
    pins.update({str(Path(native["sdk_path"]) / name): digest for name, digest in native["sdk_metadata"].items()})
    pins[str(HERE / "reproduce_v2.py")] = sha(HERE / "reproduce_v2.py")
    pins[str(a.native_input)] = sha(a.native_input)
    boundary = {"tools": tools, "pinned_files": pins, "sdk_path": native["sdk_path"],
                "sdk_resolved": str(Path(native["sdk_path"]).resolve()), "recipe": str(HERE / "reproduce_v2.py"),
                "target": str(a.target)}
    check_boundary(boundary)
    # Fully hash immutable compiler/cache inputs once per independent leg.
    cache_catalogue = inventory(a.inputs / "cargo-home")
    cache_record = a.inputs / "cargo-cache-file-digests.jsonl"
    if sha(cache_record) != "1dce44745ee12f7288a931b273f4964a5f4281d1cff41bba0076495c325265f4":
        p.error("reviewed cache inventory changed")
    recorded_cache = {}
    for line in cache_record.read_text().splitlines():
        item = json.loads(line)
        if item["type"] == "file":
            recorded_cache[str(Path(item["path"]).relative_to("cargo-home"))] = item["sha256"]
    validate_inventory(cache_catalogue, recorded_cache)
    toolchain_catalogue = inventory(a.inputs / "toolchain")
    a.output.mkdir(mode=0o700)
    retain_json(a.output / "input-provenance.json", {"mount": proof, "native": native,
                "published_source_commit": SOURCE_COMMIT, "upstream_commit": UPSTREAM_COMMIT,
                "frozen_source_manifest_sha256": SOURCE_MANIFEST_SHA,
                "source_files": source_manifest["files"], "cargo_cache": cache_catalogue,
                "toolchain": toolchain_catalogue})
    a.stage.mkdir(mode=0o700)
    source = a.stage / "src"; source.mkdir(mode=0o700)
    extract_archive(subprocess.check_output(["/usr/bin/git", "archive", UPSTREAM_COMMIT],
                                           cwd=upstream, env=clean), source)
    for patch in (repo / "tools/n1-softnet/softnet.patch", HERE / "diagnostic.patch"):
        for args in (["--check"], []):
            subprocess.run(["/usr/bin/git", "apply", *args, str(patch)], cwd=source, env=clean, check=True)
    applied = inventory(source)
    for name, digest in applied.items():
        if name.endswith((".rs", ".toml", ".lock")):
            pins[str(source / name)] = digest
    retain_json(a.output / "applied-source.json", applied)
    # No original repository hooks/config enter the detached source tree.
    shutil.copytree(a.inputs / "cargo-home", a.stage / "cargo-home")
    for name in ("home", "temp", "tools"):
        (a.stage / name).mkdir(mode=0o700)
    argv_log = a.output / "nested-argv.jsonl"; retain(argv_log, b"")
    boundary.update(argv_log=str(argv_log))
    for name in ("rustc-wrapper", "clang", "ar", "ld"):
        wrapper = a.stage / "tools" / name; retain(wrapper, WRAPPER.encode()); wrapper.chmod(0o700)
        pins[str(wrapper)] = sha(wrapper)
    config_path = a.stage / "boundary.json"; retain_json(config_path, boundary)
    env = dict(clean, HOME=str(a.stage / "home"),
               PATH=str(a.stage / "tools") + ":" + str(a.inputs / "toolchain/bin") + ":/usr/bin:/bin",
               CARGO_HOME=str(a.stage / "cargo-home"), CARGO_TARGET_DIR=str(a.target),
               TMPDIR=str(a.stage / "temp"), RUSTC=tools["rustc"],
               RUSTDOC=str(a.inputs / "toolchain/bin/rustdoc"),
               RUSTC_WRAPPER=str(a.stage / "tools/rustc-wrapper"), N1_BUILD_BOUNDARY=str(config_path),
               CC=str(a.stage / "tools/clang"), AR=str(a.stage / "tools/ar"),
               SDKROOT=native["sdk_path"], DEVELOPER_DIR=native["selected_developer"],
               CARGO_NET_OFFLINE="true", CARGO_BUILD_JOBS="4",
               CARGO_ENCODED_RUSTFLAGS="\x1f".join([
                   "--remap-path-prefix="+str(a.stage)+"=/build",
                   "--remap-path-prefix="+str(a.target)+"=/target",
                   "--remap-path-prefix="+str(a.inputs)+"=/inputs",
                   "-C", "linker="+str(a.stage / "tools/clang")]))
    version = run_recorded([tools["rustc"], "--version"], env, a.stage, a.output, "rust-version", boundary, 10)
    if version.strip() != COMPILER: raise RuntimeError("compiler version mismatch")
    # Cheap selected-linker routing check before expensive release reproduction.
    run_recorded([str(a.stage / "tools/clang"), "-###", "-x", "c", "/dev/null", "-o", str(a.stage / "linker-proof")],
                 env, a.stage, a.output, "linker-routing", boundary, 15)
    argv = [tools["cargo"], "--config", 'target.aarch64-apple-darwin.runner="/usr/bin/false"',
            "build", "--locked", "--offline", "--release", "--manifest-path", str(source / "Cargo.toml")]
    run_recorded(argv, env, a.stage, a.output, "release-build", boundary)
    binary = a.target / "release/softnet"
    executable = sha(binary); verify_binary(binary, executable)
    archive = a.output / "softnet-diagnostic.tar"; write_archive(binary, archive)
    # Grammar controls stop before self-admission, user lookup, bootpd or Proxy.
    clean_run = dict(clean, HOME=str(a.stage / "home"))
    version = run_recorded([str(binary), "--version"], clean_run, a.stage, a.output, "binary-version", boundary, 5)
    if version.strip() != "softnet " + VERSION: raise RuntimeError("binary version mismatch")
    retain_json(a.output / "build-result.json", {"version": VERSION, "artifact_source_commit": SOURCE_COMMIT,
                "upstream_commit": UPSTREAM_COMMIT, "executable_sha256": executable,
                "archive_sha256": sha(archive), "compiler": COMPILER, "compiler_sha256": RUST_SHA,
                "cargo_sha256": CARGO_SHA, "recipe_sha256": sha(HERE / "reproduce_v2.py"),
                "native_input_sha256": sha(a.native_input), "applied_source_manifest_sha256": sha(a.output / "applied-source.json")})
    # Retain source/caches/wrappers unchanged; next leg creates the same canonical stage afresh.
    a.stage.rename(a.output / "stage")
    print((a.output / "build-result.json").read_text())


if __name__ == "__main__":
    main()
