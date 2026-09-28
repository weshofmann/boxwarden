Softnet N1 candidate modification

Upstream Softnet 0.19.0 source: https://github.com/openai/softnet
Pinned source commit: df84a30016e3d6acc0d30acc660cf3a726f42a9b
Upstream license: GNU Affero General Public License version 3 (LICENSE)

Boxwarden N1 modifications add the exact containment selector, a pure packet
policy, host-interface address refresh, and enforcement at the Softnet proxy
write path. Corresponding modification source is in softnet.patch and policy.rs.
The upstream source, this patch, and the pinned Cargo.lock together form the
complete corresponding Rust source for the staged executable. This package
does not include Tart and does not change Tart's license.
