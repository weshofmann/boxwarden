import Foundation

@main struct MenuModelTests {
  static func main() {
    func check(_ condition: @autoclosure () -> Bool, _ message: String) {
      if !condition() { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    let config = Data(#"{"version":2,"domains":{"work":{"state_root":"/one"},"personal":{"state_root":"/two"}}}"#.utf8)
    check(try! ConfigDomains.names(from: config) == ["personal", "work"], "all configured domains must be selectable")
    let ready = MenuTarget(domain: "work", session: "dev", sessionID: "11111111-1111-4111-8111-111111111111", backendKind: "tart", backendObject: "bw-dev", generation: "22222222-2222-4222-8222-222222222222", available: true)
    let personal = MenuTarget(domain: "personal", session: "notes", sessionID: "33333333-3333-4333-8333-333333333333", backendKind: "tart", backendObject: "bw-notes", generation: "44444444-4444-4444-8444-444444444444", available: true)
    let stopped = MenuTarget(domain: "work", session: "stopped", sessionID: "55555555-5555-4555-8555-555555555555", backendKind: "tart", backendObject: "bw-stopped", generation: "", available: false)
    let model = MenuState()
    let refresh = model.beginRefresh()
    model.acceptRefresh(refresh, targets: [ready, personal, stopped])
    check(model.selected == nil && !model.canTransfer, "initial discovery requires explicit sandbox selection")
    model.select(personal)
    check(model.selected == personal, "explicit selection identifies domain and session")
    model.select(ready)
    let click = model.beginTransfer(.push)
    check(click?.target == ready && !model.canTransfer, "click captures exact target and disables another transfer")
    check(click?.arguments(config: "/opt/config") == ["--config", "/opt/config", "--domain", "work", "clipboard", "push", "dev", "--expected-session-id", ready.sessionID, "--expected-backend-kind", "tart", "--expected-backend-object", "bw-dev", "--expected-generation", ready.generation], "transfer uses exact argument vector")
    model.select(personal)
    check(click?.target == ready, "selection change cannot retarget in-flight transfer")
    model.finishTransfer(click!.id, result: .success)
    check(model.status == "Transfer completed." && model.selected == personal, "success publishes payload-free result")
    model.select(stopped)
    check(model.beginTransfer(.pull) == nil, "unavailable target cannot transfer")
    let old = model.beginRefresh()
    let newer = model.beginRefresh()
    model.acceptRefresh(old, targets: [ready])
    check(model.refreshing, "stale discovery cannot enable a target")
    model.acceptRefresh(newer, targets: [ready])
    check(!model.refreshing && model.selected == nil, "vanished selection is cleared")
    model.select(ready)
    let stale = model.beginRefresh()
    check(!model.canTransfer, "refresh clears prior readiness until renewed")
    model.acceptRefresh(stale, targets: [ready])
    let failed = model.beginTransfer(.pull)!
    model.finishTransfer(failed.id, result: .failed)
    check(model.status.contains("outcome unknown"), "failed transfer never promises rollback")
    let replacement = MenuTarget(domain: "work", session: "dev", sessionID: "66666666-6666-4666-8666-666666666666", backendKind: "tart", backendObject: "bw-dev-new", generation: "77777777-7777-4777-8777-777777777777", available: true)
    let changed = model.beginRefresh()
    model.acceptRefresh(changed, targets: [replacement])
    check(model.selected == nil && !model.canTransfer, "same-name replacement must require another explicit selection")
    model.select(ready)
    check(model.selected == nil, "stale menu item cannot select a replacement by name")
    model.select(replacement)
    let restarted = MenuTarget(domain: replacement.domain, session: replacement.session, sessionID: replacement.sessionID,
                               backendKind: replacement.backendKind, backendObject: replacement.backendObject,
                               generation: "88888888-8888-4888-8888-888888888888", available: true)
    let rebooted = model.beginRefresh()
    model.acceptRefresh(rebooted, targets: [restarted])
    check(model.selected == nil && !model.canTransfer, "new generation requires explicit reselection")
    let partial = model.beginRefresh()
    model.acceptRefresh(partial, targets: [ready], failedDomains: 1)
    check(model.targets == [ready] && model.status == "Some sandbox status unavailable.", "one failed domain does not hide another target")
    check(!MenuTarget(domain: "work", session: "bad", sessionID: "bad", backendKind: "tart", backendObject: "bw", generation: "bad", available: true).isValid, "malformed binding is never enabled")
    print("PASS: menu state, configured domains, exact target, stale and outcome handling")
  }
}
