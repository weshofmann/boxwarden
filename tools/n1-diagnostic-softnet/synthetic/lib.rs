#[path="../policy.rs"]
mod boxwarden_policy;
#[path="../dispatch.rs"]
mod dispatch;
#[path="../wire.rs"]
mod wire;
#[path="../watch.rs"]
mod watch;
#[path="../admission.rs"]
mod admission;
#[path="../args.rs"]
mod args;
#[path="../../n1-softnet/policy.rs"]
mod canonical;
#[cfg(test)]
#[path="../tests.rs"]
mod tests;
