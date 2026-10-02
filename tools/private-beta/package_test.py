"""Portable package refusal checks; successful archive is exercised on the Mac."""
import pathlib
import os
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("build.sh")


class PackageInputTests(unittest.TestCase):
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
            helper.write_text('#!/bin/bash\nset -e\nout=$(mktemp -d /private/tmp/boxwarden-alpha-formatter.XXXXXX)\n/bin/cp "$FIXTURE_ARTIFACTS"/* "$out/"\nprintf "%s\\n" "$out" >> "$FIXTURE_OUTPUTS"\nprintf "prepared formatter boot artifacts: %s\\n" "$out"\n')
            (package / "bin").mkdir()
            cli = package / "bin/boxwarden"
            cli.write_text('#!/bin/bash\nset -e\nfor name in ' + ' '.join(names) + '; do test -f "$(dirname "$0")/../formatter/$name"; done\necho saved\n')
            cli.chmod(0o700)
            bin_dir = root / "tools"
            bin_dir.mkdir()
            go = bin_dir / "go"
            go.write_text('#!/bin/bash\nexit 0\n')
            go.chmod(0o700)
            cp = bin_dir / "cp"
            cp.write_text('#!/bin/bash\nif [[ "$1" == */alpha-formatter ]]; then exit 73; fi\nexec /bin/cp "$@"\n')
            cp.chmod(0o700)
            inputs = [root / name for name in ("config", "iso", "checker")]
            for path in inputs:
                path.write_text("synthetic")
            outputs = root / "outputs"
            env = dict(os.environ, FIXTURE_ARTIFACTS=str(prepared), FIXTURE_OUTPUTS=str(outputs))
            args = ["bash", str(package / "prepare-projects.sh"), *map(str, inputs), str(go)]
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
