"""Package refusal and orchestration checks; real archives are exercised on the Mac."""
import hashlib
import json
import pathlib
import os
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).with_name("build.sh")


class PackageInputTests(unittest.TestCase):
    def test_unknown_network_selection_fails_before_staging(self):
        with tempfile.TemporaryDirectory() as temporary:
            for selection in ("", "n1", "N1candidate", "-tags=n1candidate", "stock extra"):
                with self.subTest(selection=selection):
                    output = pathlib.Path(temporary) / "new"
                    result = subprocess.run(["bash", str(SCRIPT), "0.2.0-beta.1", str(output), selection],
                                            capture_output=True, text=True)
                    self.assertEqual(result.returncode, 2)
                    self.assertIn("selection must be stock or n1candidate", result.stderr)
                    self.assertFalse(output.exists())

    def build_boundary_fixture(self, root):
        repo = root / "source"
        scripts = repo / "tools/private-beta"
        scripts.mkdir(parents=True)
        shutil.copy(SCRIPT, scripts / "build.sh")
        subprocess.run(["git", "init", "-q", str(repo)], check=True)
        subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
        subprocess.run(["git", "-C", str(repo), "-c", "user.name=Fixture", "-c",
                        "user.email=fixture@example.invalid", "commit", "-qm", "fixture"], check=True)
        tools = root / "tools"
        tools.mkdir()
        go = tools / "go"
        go.write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$CAPTURE_ARGS"\nexit 73\n')
        go.chmod(0o700)
        uname = tools / "uname"
        uname.write_text('#!/bin/bash\ncase "$1" in -s) echo Darwin;; -m) echo arm64;; *) exit 72;; esac\n')
        uname.chmod(0o700)
        return repo, tools, go

    def test_selection_controls_exact_go_build_arguments_and_package_name(self):
        # Stop at the compiler boundary: this proves orchestration, not a
        # successful archive. Actual packages still require native verification.
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            repo, tools, go = self.build_boundary_fixture(root)
            for selection in (None, "stock", "n1candidate"):
                with self.subTest(selection=selection):
                    output = root / (selection or "default")
                    capture = root / "args"
                    env = dict(os.environ, GO_BIN=str(go), CAPTURE_ARGS=str(capture),
                               PATH=str(tools) + os.pathsep + os.environ["PATH"])
                    args = ["bash", str(repo / "tools/private-beta/build.sh"), "0.2.0-beta.1", str(output)]
                    if selection is not None:
                        args.append(selection)
                    result = subprocess.run(args, env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 73, result.stderr)
                    captured = capture.read_text().splitlines()
                    name = "boxwarden-0.2.0-beta.1-darwin-arm64"
                    if selection == "n1candidate":
                        name += "-n1candidate"
                        self.assertEqual(captured[captured.index("-tags") + 1], "n1candidate")
                    else:
                        self.assertNotIn("-tags", captured)
                    self.assertEqual(captured[captured.index("-o") + 1], str(output / name / "bin/boxwarden"))
                    self.assertEqual(captured[-1], "./cmd/boxwarden")

    def test_compiled_policy_identity_and_bundle_id_reach_native_builder(self):
        # Fake only compiler/signature/native work; run the real packaging
        # orchestration and validate its published metadata before native build.
        for selection, override in ((None, None), ("stock", None),
                                    ("n1candidate", None), ("n1candidate", "org.example.review")):
            with self.subTest(selection=selection, override=override), tempfile.TemporaryDirectory() as temporary:
                root = pathlib.Path(temporary)
                repo, tools, go = self.build_boundary_fixture(root)
                native = repo / "host/clipboard-menu/build.sh"
                native.parent.mkdir(parents=True)
                native.write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$NATIVE_ARGS"\nexit 73\n')
                subprocess.run(["git", "-C", str(repo), "add", "."], check=True)
                subprocess.run(["git", "-C", str(repo), "-c", "user.name=Fixture", "-c",
                                "user.email=fixture@example.invalid", "commit", "-qm", "native fixture"], check=True)
                build = selection or "stock"
                policy = {"build": build, "description": "N1 candidate; not host-qualified" if build == "n1candidate" else "stock ADR 015; permits gateway service access",
                          "softnet_version": "0.19.0-boxwarden-n1.1" if build == "n1candidate" else "0.19.0",
                          "softnet_executable_sha256": "064206d28d82b86093244114f44f726f4f5967575a9b298a7123bf0beb740ef0" if build == "n1candidate" else "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e",
                          "softnet_archive_sha256": "e06a722dfc9ab998f99144adc88ff9b03cd3356d1d1b62cb3c692cd4731b458e" if build == "n1candidate" else "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c",
                          "block_target": "@boxwarden-host-containment" if build == "n1candidate" else ""}
                cli = root / "compiled-cli"
                cli.write_text('#!/bin/bash\n[[ "$*" == "build-info --json" ]] || exit 72\ncat <<\'JSON\'\n' + json.dumps({"network_policy": policy}) + '\nJSON\n')
                go.write_text('#!/bin/bash\nif [[ "$1" == version ]]; then echo "go version fixture"; exit; fi\nwhile [[ "$1" != -o ]]; do shift; done\ncp "$COMPILED_CLI" "$2"\nchmod 700 "$2"\n')
                codesign = tools / "codesign"
                codesign.write_text('#!/bin/bash\nexit 0\n')
                codesign.chmod(0o700)
                output = root / "output"
                env = dict(os.environ, GO_BIN=str(go), COMPILED_CLI=str(cli), NATIVE_ARGS=str(root / "native-args"),
                           PATH=str(tools) + os.pathsep + os.environ["PATH"])
                env.pop("BOXWARDEN_APP_BUNDLE_ID", None)
                if override:
                    env["BOXWARDEN_APP_BUNDLE_ID"] = override
                args = ["bash", str(repo / "tools/private-beta/build.sh"), "0.2.0-beta.1", str(output)]
                if selection is not None:
                    args.append(selection)
                result = subprocess.run(args, env=env, capture_output=True, text=True)
                self.assertEqual(result.returncode, 73, result.stderr)
                native_args = (root / "native-args").read_text().splitlines()
                app_id = override or "org.boxwarden.project-manager" + (".n1candidate" if build == "n1candidate" else "")
                self.assertEqual(native_args[native_args.index("--bundle-id") + 1], app_id)
                self.assertIn("--network-policy", native_args)
                self.assertEqual(native_args[native_args.index("--network-policy") + 1], build)
                package = next(output.iterdir())
                metadata = json.loads((package / "BUILD.json").read_text())
                self.assertEqual(metadata["network_policy"], policy)
                self.assertEqual(metadata["application_id"], app_id)
                warning = package / "N1-CANDIDATE.txt"
                self.assertEqual(warning.exists(), build == "n1candidate")
                if warning.exists():
                    self.assertIn("not host-qualified", warning.read_text())
                    self.assertIn("No N1 executable", warning.read_text())

    def test_compiled_selection_mismatch_stops_before_native_build(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            repo, tools, go = self.build_boundary_fixture(root)
            go.write_text("""#!/bin/bash
while [[ "$1" != -o ]]; do shift; done
cat > "$2" <<'CLI'
#!/bin/bash
printf '%s\\n' '{"network_policy":{"build":"stock"}}'
CLI
chmod 700 "$2"
""")
            codesign = tools / "codesign"
            codesign.write_text('#!/bin/bash\nexit 0\n')
            codesign.chmod(0o700)
            output = root / "output"
            env = dict(os.environ, GO_BIN=str(go), PATH=str(tools) + os.pathsep + os.environ["PATH"])
            result = subprocess.run(["bash", str(repo / "tools/private-beta/build.sh"), "0.2.0-beta.1",
                                     str(output), "n1candidate"], env=env, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("compiled network policy does not match requested selection", result.stderr)
            package = next(output.iterdir())
            self.assertFalse((package / "BUILD.json").exists())
            self.assertFalse((package / "Boxwarden.app").exists())

    def test_immutable_support_builder_receives_app_contained_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=pathlib.Path(temporary); repo,tools,go=self.build_boundary_fixture(root)
            native=repo/'host/clipboard-menu/build.sh';native.parent.mkdir(parents=True)
            native.write_text('#!/bin/bash\nwhile [[ "$1" != --output ]]; do shift; done\nmkdir -p "$2/Contents/MacOS"\n')
            shutil.copy(SCRIPT.with_name('prepare-projects.sh'),repo/'tools/private-beta/prepare-projects.sh')
            helper=repo/'tools/private-beta/build_support.sh'
            helper.write_text('#!/bin/bash\nprintf "%s\\n" "$@" > "$SUPPORT_ARGS"\nexit 73\n')
            subprocess.run(['git','-C',str(repo),'add','.'],check=True)
            subprocess.run(['git','-C',str(repo),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','support fixture'],check=True)
            cli=root/'compiled-cli'
            cli.write_text('#!/bin/bash\nprintf \'%s\\n\' \'{"network_policy":{"build":"stock"}}\'\n')
            go.write_text('#!/bin/bash\nif [[ "$1" == version ]]; then echo fixture;exit;fi\nwhile [[ "$1" != -o ]]; do shift;done\ncp "$COMPILED_CLI" "$2"\nchmod 700 "$2"\n')
            codesign=tools/'codesign';codesign.write_text('#!/bin/bash\nexit 0\n');codesign.chmod(0o700)
            zstd=tools/'zstd';zstd.write_text('#!/bin/bash\nexit 0\n');zstd.chmod(0o700)
            env=dict(os.environ,GO_BIN=str(go),BOXWARDEN_SUPPORT_ZSTD=str(zstd),BOXWARDEN_SUPPORT_ISO=str(root/'input.iso'),BOXWARDEN_SUPPORT_CHECKER_DEB=str(root/'checker.deb'),SUPPORT_ARGS=str(root/'support-args'),COMPILED_CLI=str(cli),PATH=str(tools)+os.pathsep+os.environ['PATH'])
            result=subprocess.run(['bash',str(repo/'tools/private-beta/build.sh'),'0.2.0-beta.1',str(root/'out')],env=env,capture_output=True,text=True)
            self.assertEqual(result.returncode,73,result.stderr)
            args=(root/'support-args').read_text().splitlines()
            app=root/'out/boxwarden-0.2.0-beta.1-darwin-arm64/Boxwarden.app/Contents/Resources/Boxwarden'
            self.assertEqual(args,[str(app/'support/source'),env['BOXWARDEN_SUPPORT_ISO'],env['BOXWARDEN_SUPPORT_CHECKER_DEB'],str(app/'support/resources')])
            self.assertTrue((app/'bin/boxwarden').is_file())
            self.assertTrue((app/'prepare-projects.sh').is_file())
            self.assertEqual((app/'prepare-projects.sh').read_bytes(),SCRIPT.with_name('prepare-projects.sh').read_bytes())
            self.assertTrue((app/'support/source/.git').is_dir())

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
