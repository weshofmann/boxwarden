#!/bin/bash
set -euo pipefail
umask 077
# Imports must never write into the retained signed source checkout.
export PYTHONDONTWRITEBYTECODE=1
# Reproducible immutable helpers only: no VM starts or disk attachments.
if [[ $# != 4 ]]; then echo 'usage: build_support.sh SOURCE PINNED_ISO PINNED_CHECKER_DEB NEW_RESOURCES' >&2;exit 2;fi
source=$1
iso=$2
checker=$3
output=$4
go=${GO_BIN:?set actual Go compiler for package construction}
zstd_bin=${BOXWARDEN_SUPPORT_ZSTD:?set actual absolute build-time zstd executable}
[[ "$zstd_bin" == /* && -f "$zstd_bin" && -x "$zstd_bin" && ! -L "$zstd_bin" && $(basename "$zstd_bin") == zstd ]] || { echo 'supply the actual zstd executable, not a shim' >&2;exit 2; }
export PATH="$(dirname "$go"):$(dirname "$zstd_bin"):/usr/bin:/bin"
[[ ! -e "$output" && ! -L "$output" ]] || { echo 'support output must not exist' >&2;exit 2; }
[[ -z $(git -C "$source" status --porcelain --untracked-files=all) ]] || { echo 'support build requires the exact clean package source' >&2;exit 1; }
# Verification precedes archive parsing and every compiler invocation.
[[ $(shasum -a 256 "$iso" | cut -d ' ' -f1) == c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe ]] || { echo 'support ISO digest differs' >&2;exit 1; }
[[ $(shasum -a 256 "$checker" | cut -d ' ' -f1) == 0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae ]] || { echo 'support checker package digest differs' >&2;exit 1; }
mkdir -m 700 "$output"
work=$(mktemp -d "$(dirname "$output")/.support-build.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir -m 700 "$output/formatter" "$output/inspector" "$output/inspector/casper"
bsdtar -xf "$iso" -C "$work" casper/vmlinuz casper/initrd
cp "$work/casper/vmlinuz" "$output/inspector/casper/vmlinuz"
cp "$work/casper/initrd" "$output/inspector/casper/initrd"
chmod 400 "$output/inspector/casper/"*
python3 "$source/tools/alpha-inspector/kernel_image.py" "$work/casper/vmlinuz" "$output/formatter/kernel-image" 000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617 a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d
cp "$output/formatter/kernel-image" "$output/inspector/kernel-image"
ar -p "$checker" data.tar.zst | bsdtar -xOf - ./usr/sbin/e2fsck.static > "$output/formatter/e2fsck.static"
[[ $(shasum -a 256 "$output/formatter/e2fsck.static" | cut -d ' ' -f1) == e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279 ]] || { echo 'support extracted checker digest differs' >&2;exit 1; }
export GOCACHE="$work/gocache" GOMODCACHE="$work/modcache" GOTOOLCHAIN=local GOENV=off GOPROXY=off GOSUMDB=off
(cd "$source"; CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$go" build -trimpath -o "$output/formatter/alpha-formatter" ./tools/alpha-formatter/guest)
(cd "$source"; CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$go" build -trimpath -o "$output/inspector/alpha-probe" ./tools/alpha-inspector/guest)
python3 "$source/tools/alpha-formatter/pack_initramfs.py" "$work/casper/initrd" "$output/formatter/alpha-formatter" "$output/formatter/e2fsck.static" "$output/formatter/formatter-initrd"
printf '// Boxwarden managed runtime binding v1\n' > "$output/formatter/binding.swift"
swiftc -D MANAGED_RUNTIME -module-cache-path "$work/swift-cache" -o "$output/formatter/alpha-formatter-host" "$source/tools/alpha-formatter/main.swift" "$source/tools/alpha-formatter/boot.swift"
swiftc -module-cache-path "$work/swift-cache" -o "$output/inspector/alpha-inspector" "$source/tools/alpha-inspector/main.swift" "$source/tools/alpha-inspector/boot.swift"
for runner in "$output/formatter/alpha-formatter-host" "$output/inspector/alpha-inspector"; do
 codesign --force --sign - --entitlements "$source/tools/alpha-inspector/virtualization.entitlements" "$runner"
 codesign --verify --strict "$runner"
done
find "$output" -type f -exec chmod 600 {} +
chmod 400 "$output/inspector/casper/"*
chmod 700 "$output/formatter/alpha-formatter" "$output/formatter/alpha-formatter-host" "$output/inspector/alpha-probe" "$output/inspector/alpha-inspector"
python3 - "$source" "$output" <<'PYBUILD'
import json
from pathlib import Path
import plistlib
import subprocess
import sys
source,root=map(Path,sys.argv[1:])
sys.path.insert(0,str(source/'tools/private-beta'))
from support_resources import ISO_SHA,DEB_SHA,FORMATTER_NAMES,INSPECTOR_NAMES,digest,inventory,write_private
for name in ('formatter/alpha-formatter-host','inspector/alpha-inspector'):
 result=subprocess.run(['/usr/bin/codesign','-d','--entitlements',':-',str(root/name)],capture_output=True,check=True,timeout=10)
 if plistlib.loads(result.stdout)!={'com.apple.security.virtualization':True}:raise ValueError('support runner entitlements differ')
files={'formatter/'+name:digest(root/'formatter'/name) for name in FORMATTER_NAMES}
files.update({'inspector/'+name:digest(root/'inspector'/name) for name in INSPECTOR_NAMES})
m={'version':1,'source_commit':subprocess.check_output(['/usr/bin/git','-C',str(source),'rev-parse','HEAD'],text=True).strip(),'iso_sha256':ISO_SHA,'checker_deb_sha256':DEB_SHA,'files':files,'source_inputs':inventory(source),'runner_entitlements':{'com.apple.security.virtualization':True}}
write_private(root/'manifest.json',(json.dumps(m,sort_keys=True)+'\n').encode())
PYBUILD
