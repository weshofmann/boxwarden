import Foundation

@main struct ProjectPresentationTests {
  static func main() {
    func check(_ condition: @autoclosure () -> Bool, _ message: String) {
      if !condition() { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    func ticket(_ value: UUID?, _ message: String) -> UUID {
      guard let value else { fputs("FAIL: \(message)\n", stderr); exit(1) }
      return value
    }
    let state = ProjectPresentation()
    state.chooseConfiguration("/private/one.json")
    let old = ticket(state.beginRefresh(), "a new configuration or completed refresh permits a new observation")
    check(state.beginRefresh() == nil, "refresh cannot duplicate while pending")
    state.chooseConfiguration("/private/two.json")
    let current = ticket(state.beginRefresh(), "a new configuration or completed refresh permits a new observation")
    check(!state.finishRefresh(old, names: ["wrong"]), "stale configuration result cannot replace current selection")
    check(state.finishRefresh(current, names: ["first", "second"]), "current snapshot accepted")
    state.select("second")
    let update = ticket(state.beginRefresh(), "a new configuration or completed refresh permits a new observation")
    check(state.finishRefresh(update, names: ["first", "second"]), "refresh accepted")
    check(state.selectedName == "second", "refresh preserves operator's project")
    let background = ticket(state.beginRefresh(), "background refresh starts from validated selection")
    let operation = ticket(state.beginOperation(), "validated selection starts an operation during refresh")
    check(!state.refreshing && !state.finishRefresh(background, names: ["stale"]), "operation invalidates pending inventory without changing target")
    check(state.beginRefresh() == nil, "new refresh cannot race active operation")
    check(state.beginOperation() == nil, "repeated effect click is refused")
    state.select("first")
    check(state.selectedName == "second", "active operation retains visible target")
    state.finishOperation(UUID())
    check(state.busy, "unrelated completion cannot unlock actions")
    state.finishOperation(operation)
    check(!state.busy, "exact completion unlocks actions")
    check(!state.finishRefresh(background, names: ["stale"]), "late inventory stays invalid after operation completion")
    let following = ticket(state.beginRefresh(), "operation completion permits fresh inventory")
    check(state.finishRefresh(following, names: ["first", "second"]), "new inventory accepted after operation")

    let draft = ImportConfirmation()
    draft.source = "/private/source"
    draft.exclusionsText = "node_modules, dist"
    let digest = String(repeating: "a", count: 64)
    check(!draft.acceptPreview(source: "/private/other", exclusions: ["dist", "node_modules"], digest: digest), "wrong source cannot become confirmed selection")
    check(!draft.acceptPreview(source: draft.source, exclusions: ["node_modules"], digest: digest), "wider selection cannot be accepted")
    check(draft.acceptPreview(source: draft.source, exclusions: ["dist", "node_modules"], digest: digest), "exact preview accepts canonical exclusions")
    check(draft.confirmed?.digest == digest, "confirmation pins actual digest")
    draft.exclusionsText = "node_modules"
    check(draft.confirmed == nil, "editing exclusions invalidates prior confirmation")
    check(draft.acceptPreview(source: draft.source, exclusions: ["node_modules"], digest: digest), "new exact preview accepted")
    draft.source = "/private/new-source"
    check(draft.confirmed == nil, "changing source invalidates preview")
    check(!draft.acceptPreview(source: draft.source, exclusions: ["node_modules"], digest: "bad"), "malformed pin cannot be imported")
    let suite = "org.boxwarden.test.pending." + UUID().uuidString
    let defaults = UserDefaults(suiteName: suite)!
    defer { defaults.removePersistentDomain(forName: suite) }
    let pending = ClipboardPending(defaults: defaults)
    check(pending.begin(config: "/private/a", metadata: ["request": "a", "project": "one"]), "new exact clipboard intent recorded")
    pending.complete(config: "/private/a", requestID: "a", succeeded: false)
    check(ClipboardPending(defaults: defaults).request(config: "/private/a") != nil, "failed transfer preserves uncertainty across frontend reopen")
    check(!pending.begin(config: "/private/a", metadata: ["request": "duplicate"]), "unacknowledged clipboard outcome cannot be overwritten")
    check(pending.begin(config: "/private/b", metadata: ["request": "b", "project": "two"]), "independent configuration has its own marker")
    pending.acknowledge(config: "/private/b")
    check(pending.request(config: "/private/a") != nil, "acknowledging another configuration preserves original uncertainty")
    pending.complete(config: "/private/a", requestID: "stale", succeeded: true)
    check(pending.request(config: "/private/a") != nil, "stale completion cannot clear exact marker")
    pending.complete(config: "/private/a", requestID: "a", succeeded: true)
    check(pending.request(config: "/private/a") == nil, "exact confirmed success clears its marker")
    print("PASS: selection, stale refresh, repeated actions and preview pinning")
  }
}
