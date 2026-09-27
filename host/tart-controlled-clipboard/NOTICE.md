# Source and license provenance

The patch applies only to Cirrus Labs Tart 2.32.1, Git commit
`8aa377b71ebfd90b2df9803d3e20033f58d6800c`. Existing source changes are confined
to `Sources/tart/Commands/Run.swift` and `Sources/tart/CI/CI.swift`.
`ClipboardInvocation.swift` and `ClipboardUI.swift` are original Boxwarden
additions; their editable source is retained here as well as in the patch.
The parser regression test in `Tests/TartTests/ControlledClipboardRunTests.swift`
is also an original addition. No source or EOF fix from a newer Tart revision is incorporated.

The exact upstream `LICENSE` is included as `Tart-LICENSE`, SHA-256
`2b20b2bd5ed91350b37b724bbf99e6ccbeefc5b4779578f375de47c687f60055`.
It is Fair Source License 0.9, Copyright (C) 2023 Cirrus Labs, Inc. The pinned
license permits derivatives and redistribution subject to its use limitation
and conditions; redistribution must include the license and remains subject to
it. The use limitation is 100 CPU cores per entity, with the pinned license's
exception for CPUs in devices used by one individual. No trademark rights are
granted. Preserve this notice and the upstream license with staged binaries.

`Package.resolved` remains the exact pinned upstream file, SHA-256
`633b12e9adb9f5863783a28fce2a33fe51aa5f599cfb7c1d1aae775db88314ba`.
Dependency licenses remain their respective upstream licenses. Automatic
resolution is disabled; no dependencies are upgraded by this patch.
