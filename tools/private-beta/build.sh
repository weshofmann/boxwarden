#!/bin/bash
set -euo pipefail
umask 077

if [[ ( $# != 2 && $# != 3 ) || ! $1 =~ ^0\.2\.[0-9]+-beta\.[1-9][0-9]*$ ]]; then
  echo 'version must be 0.2.N-beta.N; usage: GO_BIN=/absolute/go bash tools/private-beta/build.sh VERSION NEW-OUTPUT-DIRECTORY [stock|n1candidate]' >&2
  exit 2
fi
version=$1
selection=${3-stock}
case "$selection" in
  stock) build_args=(build -trimpath); name_suffix=""; default_bundle_id=org.boxwarden.project-manager ;;
  n1candidate) build_args=(build -trimpath -tags n1candidate); name_suffix=-n1candidate; default_bundle_id=org.boxwarden.project-manager.n1candidate ;;
  *) echo 'selection must be stock or n1candidate' >&2; exit 2 ;;
esac
app_bundle_id=${BOXWARDEN_APP_BUNDLE_ID:-$default_bundle_id}
if [[ ${#app_bundle_id} -gt 255 || ! "$app_bundle_id" =~ ^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9][A-Za-z0-9-]*)+$ ]]; then
  echo 'BOXWARDEN_APP_BUNDLE_ID must be a reverse-DNS application identifier' >&2
  exit 2
fi
output=$2
if [[ -e "$output" || -L "$output" ]]; then
  echo 'output must not exist' >&2
  exit 2
fi
if [[ "$output" != /* || ! -d "$(dirname "$output")" ]]; then
  echo 'output requires an absolute path with an existing parent' >&2
  exit 2
fi
if [[ $(uname -s) != Darwin || $(uname -m) != arm64 ]]; then
  echo 'build requires an Apple Silicon Mac with Xcode Command Line Tools' >&2
  exit 1
fi
go_bin=${GO_BIN:?set GO_BIN to the actual absolute Go executable}
if [[ "$go_bin" != /* || ! -x "$go_bin" || -L "$go_bin" || $(basename "$go_bin") != go ]]; then
  echo 'GO_BIN must be the actual absolute Go executable, not a shim' >&2
  exit 2
fi
repo_root="$(cd "$(dirname "$0")/../.." && pwd -P)"
if [[ -n $(git -C "$repo_root" status --porcelain --untracked-files=all) ]]; then
  echo 'commit the clean source before packaging' >&2
  exit 1
fi
revision=$(git -C "$repo_root" rev-parse HEAD)
export PATH="$(dirname "$go_bin"):/usr/bin:/bin"
export GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
export CGO_ENABLED=1 GOOS=darwin GOARCH=arm64
mkdir -m 700 "$output"
temporary=$(mktemp -d /private/tmp/boxwarden-beta-build.XXXXXX)
trap 'rm -rf -- "$temporary"' EXIT
export GOCACHE="$temporary/gocache" GOMODCACHE="$temporary/modcache"
name="boxwarden-$version-darwin-arm64$name_suffix"
package="$output/$name"
mkdir -p "$package/bin" "$package/support" "$package/notices"
git clone --quiet --depth 1 --no-local --no-checkout "$repo_root" "$package/support/source"
git -C "$package/support/source" checkout --quiet --detach "$revision"
git -C "$package/support/source" remote remove origin
if [[ $(git -C "$package/support/source" rev-parse HEAD) != "$revision" || -n $(git -C "$package/support/source" status --porcelain --untracked-files=all) ]]; then
  echo 'packaged source is not the exact clean revision' >&2
  exit 1
fi
(
  cd "$package/support/source"
  "$go_bin" "${build_args[@]}" -ldflags "-X main.buildVersion=$version -X main.buildRevision=$revision" -o "$package/bin/boxwarden" ./cmd/boxwarden
)
codesign --force --sign - "$package/bin/boxwarden"
# Read the selected pins from the exact signed CLI being distributed, rather
# than duplicating the host-admission constants in the packaging script.
"$package/bin/boxwarden" build-info --json > "$temporary/build-info.json"
python3 - "$package" "$version" "$revision" "$go_bin" "$app_bundle_id" "$selection" "$temporary/build-info.json" <<'PY'
import json, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
policy = json.loads(pathlib.Path(sys.argv[7]).read_text())["network_policy"]
if policy["build"] != sys.argv[6]:
    raise SystemExit("compiled network policy does not match requested selection")
data = {"version": sys.argv[2], "revision": sys.argv[3], "platform": "darwin-arm64",
        "application_id": sys.argv[5], "cgo": True, "signing": "ad-hoc; not notarized",
        "go": subprocess.check_output([sys.argv[4], "version"], text=True).strip(),
        "network_policy": policy}
(root / "BUILD.json").write_text(json.dumps(data, indent=2) + "\n")
PY
if [[ "$selection" == n1candidate ]]; then
  cat > "$package/N1-CANDIDATE.txt" <<'WARNING'
N1 candidate; not host-qualified. This archive selects the pinned N1 Softnet
identity recorded in BUILD.json. No N1 executable is shipped or installed.
Do not treat this package as host containment or real-VM qualification evidence.
Privileged installation and host qualification require separate explicit approval.
WARNING
fi
bash "$repo_root/host/clipboard-menu/build.sh" --app project-manager --cli "$package/bin/boxwarden" \
  --bundle-id "$app_bundle_id" --network-policy "$selection" --output "$package/Boxwarden.app" --version "${version%-beta.*}" --build "${version##*.}"
# Retain all required source and reusable helpers when the app is moved alone.
# The copied CLI has the same signature as Contents/MacOS/boxwarden.
app_support="$package/Boxwarden.app/Contents/Resources/Boxwarden"
mkdir -p "$app_support/bin" "$app_support/support"
cp "$package/bin/boxwarden" "$app_support/bin/boxwarden"
cp "$package/support/source/tools/private-beta/prepare-projects.sh" "$app_support/prepare-projects.sh"
cp -R "$package/support/source" "$app_support/support/source"
zstd_bin=${BOXWARDEN_SUPPORT_ZSTD:?set the actual absolute build-time zstd executable}
if [[ "$zstd_bin" != /* || ! -f "$zstd_bin" || ! -x "$zstd_bin" || -L "$zstd_bin" || $(basename "$zstd_bin") != zstd ]]; then
  echo 'BOXWARDEN_SUPPORT_ZSTD must be the actual absolute zstd executable, not a shim' >&2
  exit 2
fi
export PATH="$(dirname "$go_bin"):$(dirname "$zstd_bin"):/usr/bin:/bin"
bash "$app_support/support/source/tools/private-beta/build_support.sh" \
  "$app_support/support/source" "${BOXWARDEN_SUPPORT_ISO:?set the pinned build-time Ubuntu ARM64 ISO}" \
  "${BOXWARDEN_SUPPORT_CHECKER_DEB:?set the pinned build-time static checker deb}" "$app_support/support/resources"
cp "$repo_root/LICENSE" "$repo_root/NOTICE" "$package/"
cp "$repo_root/tools/private-beta/TRY-ME.md" "$package/TRY-ME.md"
cp "$repo_root/tools/private-beta/UPGRADING.md" "$package/UPGRADING.md"
cp "$package/TRY-ME.md" "$package/UPGRADING.md" "$package/BUILD.json" "$app_support/"
cp "$repo_root/tools/private-beta/prepare-projects.sh" "$package/prepare-projects.sh"
cp "$repo_root/tools/private-beta/prepare-guest-clipboard.sh" "$package/prepare-guest-clipboard.sh"
goroot=$("$go_bin" env GOROOT)
cp "$goroot/LICENSE" "$package/notices/Go-LICENSE"
cp "$goroot/PATENTS" "$package/notices/Go-PATENTS"
for module in crypto net sys text; do
  cp "$goroot/src/vendor/golang.org/x/$module/LICENSE" "$package/notices/Go-x-$module-LICENSE"
  cp "$goroot/src/vendor/golang.org/x/$module/PATENTS" "$package/notices/Go-x-$module-PATENTS"
done
# Preserve embedded runtime attributions as well as the standard-library license.
for source in math/log.go math/atan.go crypto/internal/fips140/aes/aes_generic.go; do
  cp "$goroot/src/$source" "$package/notices/Go-${source##*/}"
done
cat > "$package/notices/README.txt" <<'NOTICES'
Boxwarden is Apache-2.0; see ../LICENSE and ../NOTICE.
The CLI, app-bundled CLI and tracked Linux guest bootstrap incorporate Go code.
Go and its vendored source licenses and patent grants are retained here.
The app dynamically uses macOS AppKit/Foundation and system Swift libraries;
these operating-system frameworks are not redistributed in this archive.
support/source is a complete shallow source checkout, not an installed toolchain.
Its historical tools/n1-softnet patch is separately AGPL-3.0; its LICENSE and
NOTICE.md remain there. That patch was modified 2026-09-28; no N1 executable is
shipped or installed by this package. Tart, Softnet and Go compiler
executables are not redistributed. Pinned Linux kernel/initrd and static e2fsck
resources are bundled; see PREBUILT-SUPPORT.md for provenance and licenses.
NOTICES
cp "$repo_root/tools/private-beta/PREBUILT-SUPPORT.md" "$package/notices/PREBUILT-SUPPORT.md"
# Retain the original upstream checker copyright from the admitted package.
ar -p "$BOXWARDEN_SUPPORT_CHECKER_DEB" data.tar.zst | bsdtar -xOf - ./usr/share/doc/e2fsck-static/copyright > "$package/notices/e2fsprogs-copyright"
cp -R "$package/notices" "$app_support/notices"
cp "$package/LICENSE" "$package/NOTICE" "$app_support/"
# Re-sign after adding resources. The two VZ helpers retain their individual
# signatures and sole virtualization entitlement; app signing adds no grants.
chmod -R go-rwx "$package"
codesign --force --sign - "$package/Boxwarden.app"
codesign --verify --strict "$package/Boxwarden.app"

(
  cd "$package"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 shasum -a 256 > SHA256SUMS
  shasum -a 256 -c SHA256SUMS > /dev/null
)
COPYFILE_DISABLE=1 tar -czf "$output/$name.tar.gz" -C "$output" "$name"
(
  cd "$output"
  shasum -a 256 "$name.tar.gz" > "$name.tar.gz.sha256"
)
printf 'archive: %s\nrevision: %s\n' "$output/$name.tar.gz" "$revision"
