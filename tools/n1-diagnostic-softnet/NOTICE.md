Softnet N1 diagnostic modification source

Upstream Softnet 0.19.0: https://github.com/openai/softnet
Pinned commit: df84a30016e3d6acc0d30acc660cf3a726f42a9b
License: GNU Affero General Public License version 3 (LICENSE).

Complete corresponding modification source consists of unchanged canonical
../n1-softnet/softnet.patch followed by diagnostic.patch, against that upstream
commit. Cargo.toml and Cargo.lock are exact diagnostic build definitions;
policy.rs, args.rs, admission.rs, dispatch.rs, wire.rs and watch.rs duplicate
the applied modules byte for byte. synthetic/ tests those same modules.
This source preparation includes no staged executable/archive identity.
Stock Tart 2.32.1 remains separate and unchanged; its source/license is not
included. Upstream third-party dependencies retain their respective licenses.
