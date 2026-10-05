"""Portable package refusal checks; successful archive is exercised on the Mac."""
import hashlib
import pathlib
import os
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("build.sh")


class PackageInputTests(unittest.TestCase):
    def test_invalid_application_identifier_fails_before_staging(self):
        with tempfile.TemporaryDirectory() as temporary:
            for identifier in ("../escape", "bad id", "org..test", "org." + "a" * 256):
                output = pathlib.Path(temporary) / "new"
                result = subprocess.run(["bash", str(SCRIPT), "0.2.0-beta.1", str(output)],
                                        env=dict(os.environ, BOXWARDEN_APP_BUNDLE_ID=identifier),
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("BOXWARDEN_APP_BUNDLE_ID", result.stderr)
                self.assertFalse(output.exists())

    def test_existing_output_is_preserved(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = pathlib.Path(temporary) / "existing"
            output.mkdir()
            sentinel = output / "sentinel"
            sentinel.write_text("preserve")
            result = subprocess.run(["bash", str(SCRIPT), "0.2.0-beta.1", str(output)], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("output must not exist", result.stderr)
            self.assertEqual(sentinel.read_text(), "preserve")

    def test_version_is_a_filename_safe_beta_version(self):
        with tempfile.TemporaryDirectory() as temporary:
            for version in ("../../bad", "", "0.2", "$(touch injected)"):
                output = pathlib.Path(temporary) / "new"
                result = subprocess.run(["bash", str(SCRIPT), version, str(output)], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("version must be", result.stderr)
                self.assertFalse(output.exists())

    def test_setup_update_is_explicit_and_preserves_prepared_assets_on_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            package = root / "new-package"
            (package / "bin").mkdir(parents=True)
            (package / "formatter").mkdir()
            marker = package / "formatter/manifest.json"
            marker.write_text("existing-new-package-assets")
            shutil.copy(SCRIPT.with_name("prepare-projects.sh"), package / "prepare-projects.sh")
            cli = package / "bin/boxwarden"
            cli.write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\nexit "${FIXTURE_EXIT:-0}"\n')
            cli.chmod(0o700)
            tools = root / "tools"
            tools.mkdir()
            for name in ("go", "zstd"):
                path = tools / name
                path.write_text('#!/bin/bash\nexit 0\n')
                path.chmod(0o700)
            inputs = [root / name for name in ("config", "iso", "checker")]
            for path in inputs:
                path.write_text("synthetic")
            capture = root / "args"
            env = dict(os.environ, CAPTURE_ARGS=str(capture))
            args = ["bash", str(package / "prepare-projects.sh")]
            values = [*map(str, inputs), str(tools / "go"), str(tools / "zstd")]
            first = subprocess.run(args + values, env=env, capture_output=True, text=True)
            self.assertEqual(first.returncode, 0, first.stderr)
            self.assertEqual(capture.read_text().splitlines()[5], "setup")
            env["FIXTURE_EXIT"] = "74"
            updated = subprocess.run(args + ["--update"] + values, env=env, capture_output=True, text=True)
            self.assertEqual(updated.returncode, 74, updated.stderr)
            self.assertEqual(capture.read_text().splitlines()[5], "setup-update")
            self.assertEqual(marker.read_text(), "existing-new-package-assets")

    def recipe_setup_fixture(self, root):
        package = root / "package"
        helper = package / "support/source/tools/alpha-formatter/prepare_boot.sh"
        helper.parent.mkdir(parents=True)
        helper.write_text('#!/bin/bash\nprintf invoked > "$FORMATTER_CALLED"\nexit 73\n')
        (package / "bin").mkdir()
        cli = package / "bin/boxwarden"
        cli.write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\n')
        cli.chmod(0o700)
        shutil.copy(SCRIPT.with_name("prepare-projects.sh"), package / "prepare-projects.sh")
        tools = root / "tools"
        tools.mkdir()
        for name in ("go", "zstd"):
            tool = tools / name
            tool.write_text('#!/bin/bash\nexit 0\n')
            tool.chmod(0o700)
        openssl = tools / "openssl"
        openssl.write_text('#!/bin/bash\n[[ "$*" == "passwd -6 -salt boxwarden-prerequisite synthetic-prerequisite" ]] || exit 72\nprintf \'%s\\n\' \'$6$boxwarden-prereq$bsvT6K3VcjFnqFANCjJcS./f/0oensl45IiNphHg.TT7aDUUsaKlss3qt2ek23fOdujPaG.Lttdp6kxOGRqq50\'\n')
        openssl.chmod(0o700)
        xorriso = tools / "xorriso"
        xorriso.write_text('#!/bin/bash\n[[ "$*" == "-version" ]] || exit 72\nprintf \'xorriso version : 1.5.8\\n\'\n')
        xorriso.chmod(0o700)
        inputs = [root / name for name in ("config", "iso", "checker")]
        for path in inputs:
            path.write_text("synthetic")
        args = ["bash", str(package / "prepare-projects.sh"), *map(str, inputs),
                str(tools / "go"), str(tools / "zstd"), str(openssl), str(xorriso)]
        env = dict(os.environ, FORMATTER_CALLED=str(root / "formatter-called"), CAPTURE_ARGS=str(root / "args"))
        return package, openssl, xorriso, args, env

    def test_recipe_setup_tools_fail_before_formatter(self):
        for failure in ("missing", "partial", "symlink", "wrong-name", "nonexecutable", "openssl-capability", "openssl-success-without-hash", "xorriso-capability", "xorriso-success-without-version"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                root = pathlib.Path(temporary)
                package, openssl, xorriso, args, env = self.recipe_setup_fixture(root)
                if failure == "missing":
                    openssl.unlink()
                elif failure == "partial":
                    args.pop()
                elif failure == "symlink":
                    actual = root / "actual-openssl"
                    openssl.rename(actual)
                    openssl.symlink_to(actual)
                elif failure == "wrong-name":
                    actual = openssl.with_name("alternative-openssl")
                    openssl.rename(actual)
                    args[-2] = str(actual)
                elif failure == "nonexecutable":
                    xorriso.chmod(0o600)
                elif failure.startswith("openssl"):
                    openssl.write_text('#!/bin/bash\nexit ' + ('0' if failure == 'openssl-success-without-hash' else '64') + '\n')
                else:
                    xorriso.write_text('#!/bin/bash\nexit ' + ('0' if failure == 'xorriso-success-without-version' else '64') + '\n')
                result = subprocess.run(args, env=env, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertFalse((root / "formatter-called").exists(), result.stderr)
                self.assertFalse((root / "args").exists(), "failed preflight invoked project setup")
                self.assertFalse((package / "formatter").exists())

    def test_recipe_setup_valid_tools_reach_formatter_after_preflight(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            package, _, _, args, env = self.recipe_setup_fixture(root)
            result = subprocess.run(args, env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 73, result.stderr)
            self.assertEqual((root / "formatter-called").read_text(), "invoked")
            self.assertIn("Checking exact recipe preparation tools", result.stdout)
            self.assertIn("Preparing formatter assets", result.stdout)
            self.assertFalse((root / "args").exists())
            self.assertFalse((package / "formatter").exists())

    def test_recipe_setup_forwards_exact_tool_hashes_for_setup_and_update(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            package, openssl, xorriso, args, env = self.recipe_setup_fixture(root)
            (package / "formatter").mkdir()
            for update in (False, True):
                command = args[:]
                if update:
                    command.insert(2, "--update")
                result = subprocess.run(command, env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                captured = (root / "args").read_text().splitlines()
                self.assertEqual(captured[5], "setup-update" if update else "setup")
                for flag, path in (("--openssl", openssl), ("--xorriso", xorriso)):
                    self.assertIn(flag, captured)
                    self.assertEqual(captured[captured.index(flag) + 1], str(path))
                    digest_flag = flag + "-sha256"
                    self.assertIn(digest_flag, captured)
                    self.assertEqual(captured[captured.index(digest_flag) + 1], hashlib.sha256(path.read_bytes()).hexdigest())
                self.assertFalse((root / "formatter-called").exists())

    @unittest.skipUnless(os.uname().sysname == "Darwin", "Mac formatter publication")
    def test_failed_copy_can_retry_without_partial_final_bundle(self):
        with tempfile.TemporaryDirectory(dir="/private/tmp", prefix="boxwarden-beta-prep-test.") as temporary:
            root = pathlib.Path(temporary)
            package = root / "package"
            helper = package / "support/source/tools/alpha-formatter/prepare_boot.sh"
            helper.parent.mkdir(parents=True)
            shutil.copy(SCRIPT.with_name("prepare-projects.sh"), package / "prepare-projects.sh")
            prepared = root / "artifacts"
            prepared.mkdir()
            names = "kernel-image formatter-initrd alpha-formatter e2fsck.static alpha-formatter-host binding.swift manifest.json".split()
            for name in names:
                (prepared / name).write_text(name)
            helper.write_text('#!/bin/bash\nset -e\n[[ "$PWD" == "$FIXTURE_SOURCE" ]] || exit 72\ncommand -v zstd >/dev/null || exit 71\nout=$(mktemp -d /private/tmp/boxwarden-alpha-formatter.XXXXXX)\n/bin/cp "$FIXTURE_ARTIFACTS"/* "$out/"\nprintf "%s\\n" "$out" >> "$FIXTURE_OUTPUTS"\nprintf "prepared formatter boot artifacts: %s\\n" "$out"\n')
            (package / "bin").mkdir()
            cli = package / "bin/boxwarden"
            cli.write_text('#!/bin/bash\nset -e\nfor name in ' + ' '.join(names) + '; do test -f "$(dirname "$0")/../formatter/$name"; done\necho saved\n')
            cli.chmod(0o700)
            bin_dir = root / "tools"
            bin_dir.mkdir()
            go = bin_dir / "go"
            go.write_text('#!/bin/bash\nexit 0\n')
            go.chmod(0o700)
            zstd = bin_dir / "zstd"
            zstd.write_text('#!/bin/bash\nexit 0\n')
            zstd.chmod(0o700)
            cp = bin_dir / "cp"
            cp.write_text('#!/bin/bash\nif [[ "$1" == */alpha-formatter ]]; then exit 73; fi\nexec /bin/cp "$@"\n')
            cp.chmod(0o700)
            inputs = [root / name for name in ("config", "iso", "checker")]
            for path in inputs:
                path.write_text("synthetic")
            outputs = root / "outputs"
            env = dict(os.environ, FIXTURE_ARTIFACTS=str(prepared), FIXTURE_OUTPUTS=str(outputs), FIXTURE_SOURCE=str(package / "support/source"))
            args = ["bash", str(package / "prepare-projects.sh"), *map(str, inputs), str(go), str(zstd)]
            try:
                failed = subprocess.run(args, env=env, capture_output=True, text=True)
                self.assertEqual(failed.returncode, 73, failed.stderr)
                self.assertFalse((package / "formatter").exists(), "partial bundle became final")
                cp.unlink()
                retried = subprocess.run(args, env=env, capture_output=True, text=True)
                self.assertEqual(retried.returncode, 0, retried.stderr)
                self.assertEqual(len(outputs.read_text().splitlines()), 2)
                self.assertEqual(sorted(p.name for p in (package / "formatter").iterdir()), sorted(names))
            finally:
                if outputs.exists():
                    for path in outputs.read_text().splitlines():
                        if pathlib.Path(path).exists():
                            shutil.rmtree(path)


if __name__ == "__main__":
    unittest.main()
