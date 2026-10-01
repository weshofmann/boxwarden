# Diagnostic.2 reproduction recipe

`reproduce_v2.py` is the separately named recipe for Rust source commit
`85bcb9aebffcc4a8c98b1bec111df2db8c18e81f` and source-manifest SHA256
`c820aad5292d0a8cda78d57a3a87072b0e7cac67a1499d13436eb6c7854346c5`.
The historical `build.py` and `artifact.json` remain diagnostic.1 evidence.

The new recipe changes only eight explicit bindings from the historical recipe:
source commit, source manifest, version, two target directories, staging path,
output prefix and four references to its own filename. It retains comparison
of every file in the published source directory. The new recipe was absent from
that earlier source commit; its independently reviewed bytes and later recipe
publication identity are recorded separately. This avoids a self-referential
source-commit cycle without narrowing the published-source check.

Fresh independent targets are `rust-target-9` and `rust-target-10` below
`/Volumes/BoxwardenAlphaQualification/n1build-20260930`. Both legs use the same
fresh `task5-prearm-stage-r2` pathname, moved into each new per-leg output on
completion. The recipe refuses nonempty targets, existing stage/output,
changed inputs and foreign retention paths before compiler dispatch. Selected
compiler/SDK records, encrypted backing association, immutable cache inventory,
closed environments, child argv elements and archive date rules remain exact.

`test_reproduction_v2.py` checks the actual path gate with disposable fixtures,
exact source/version bindings, recipe drift refusal before child execution and
linker/archive controls. It does not build or invoke the native diagnostic.
For the historical synthetic metadata fixtures on this Mac, retain the approved
operator-owned `TMPDIR`; root-group `/private/tmp` clears a synthetic setgid bit
when the operator lacks that group, so it cannot exercise that control.

Recipe/source checks are preparation evidence. New executable/archive digests
require two fresh successful builds and byte equality. The completed diagnostic.2 twins are recorded in `artifact_v2.json`; no
installation or live qualification is claimed.


The first diagnostic.2 leg7 compiled but failed mandatory metadata admission:
inherited caller umask077 made its binary0700. Failed target7/stage-r1/output7
remain immutable; unused target8 is retained. The corrected recipe records
and passes child umask022 directly to each `subprocess.run`, so compiler
children inherit a known mask independently of the caller. It never changes
the parent's mask or normalizes a produced binary. Private directories700 and
evidence600 remain explicit. A harmless actual child fixture demonstrates the
700-before/755-after behavior and unchanged parent/evidence modes. Existing
metadata refusal stays exact. Fresh9/10 twins now have full binary/archive/applied-source byte equality.
Both compiler runs and version queries exited zero; the raw binaries admitted
0755 without normalization. Executable SHA256 is
`1bb12bec8821835ada8c036426c2c03061be6cebdf4a1b658249d268fc8bde05`;
USTAR SHA256 is
`76704f05eba242cb649ab49db28028556c338a4daeefe9327fdfe37f874808ac`.
The producing Rust commit remains85bcb9a; the separate recipe publication is
02c4b6. The deterministic child-umask fixture uses an explicit SDK symlink and
its canonical target, so runner temporary-path aliases do not falsely fail
production boundary validation.
