import AppKit

@main struct ProjectOnboardingTests {
  static func main() throws {
    _ = NSApplication.shared
    func check(_ value: @autoclosure () throws -> Bool, _ message: String) {
      guard (try! value()) else { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    let root = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("bw-onboarding-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("cli")
    let delay = root.appendingPathComponent("delay")
    let started = root.appendingPathComponent("started")
    let readiness = root.appendingPathComponent("readiness.json")
    let ready = #"{"version":1,"scope":"alpha_project_setup","status":"ready","config_path":"/synthetic/good.json","config_valid":true,"selection_acceptable":true,"guidance":"Ready","next_actions":[],"recipe_preparation_available":true}"#
    let bad = #"{"version":1,"scope":"alpha_project_setup","status":"config_invalid","config_path":"/synthetic/bad.json","config_valid":false,"selection_acceptable":false,"guidance":"Choose a valid configuration","next_actions":["choose_configuration"]}"#
    let list = #"{"version":1,"type":"result","operation":"project.list","data":{"setup":{"status":"ready","guidance":""},"projects":[]}}"#
    try Data("#!/bin/sh\nif [ \"$3\" = setup ]; then\n if [ -f '\(delay.path)' ]; then /usr/bin/touch '\(started.path)'; /bin/sleep 1; fi\n if [ \"$2\" = /synthetic/bad.json ]; then printf '%s\\n' '\(bad)'; elif [ -f '\(readiness.path)' ]; then /bin/cat '\(readiness.path)'; else printf '%s\\n' '\(ready)'; fi\nelse printf '%s\\n' '\(list)'; fi\n".utf8).write(to: cli)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    let suite = "org.boxwarden.test.onboarding-" + UUID().uuidString
    let defaults = UserDefaults(suiteName: suite)!
    defer { defaults.removePersistentDomain(forName: suite) }
    let window = ProjectWindowController(executable: cli.path, activityDirectory: root.appendingPathComponent("activity"), defaults: defaults)
    func buttons(_ view: NSView) -> [NSButton] {
      (view as? NSButton).map { [$0] } ?? view.subviews.flatMap(buttons)
    }
    check(buttons(window.window!.contentView!).contains { $0.title == "Set up Boxwarden…" && $0.isEnabled }, "fresh launch offers configuration creation through native setup")
    check(buttons(window.window!.contentView!).contains { $0.title == "Use Existing Configuration…" }, "fresh launch distinguishes existing setup from first setup")
    window.setUpBoxwarden(nil)
    check(window.preparationCompletion != nil, "native setup opens the bounded two-selection wizard")
    window.preparationCompletion?.cancel(nil)
    check(defaults.string(forKey: "PendingFirstRunConfiguration") == nil && defaults.string(forKey: "SelectedConfiguration") == nil, "cancelling first setup never publishes a candidate preference")
    check(window.window?.attachedSheet == nil, "cancellation closes the setup wizard")
    window.useConfiguration("/synthetic/good.json")
    func wait(_ predicate: () -> Bool) {
      let end = Date().addingTimeInterval(8)
      while !predicate() && Date() < end { RunLoop.current.run(until: Date().addingTimeInterval(0.03)) }
      check(predicate(), "onboarding query completed")
    }
    wait { defaults.string(forKey: "SelectedConfiguration") == "/synthetic/good.json" && window.snapshotAvailable }
    window.useConfiguration("/synthetic/bad.json")
    wait { window.chooseButton.isEnabled && window.statusLabel.stringValue.contains("Choose a valid configuration") }
    check(defaults.string(forKey: "SelectedConfiguration") == "/synthetic/good.json", "invalid selection cannot replace saved valid configuration")
    check(window.presentation.configPath == "/synthetic/good.json" && window.client?.config == "/synthetic/good.json", "invalid selection preserves active configuration and client")
    try Data((ready.replacingOccurrences(of: "\"recipe_preparation_available\":true", with: "\"recipe_preparation_available\":false") + "\n").utf8).write(to: readiness)
    window.useConfiguration("/synthetic/good.json")
    wait { !window.switchingConfiguration && !window.presentation.refreshing && window.setupInspection?.recipePreparationAvailable == false }
    check(!window.createButton.isEnabled && !window.helpButton.isHidden, "legacy setup keeps update guidance and refuses recipes")
    try FileManager.default.removeItem(at: readiness)
    window.refreshProjects(nil)
    wait { window.setupInspection?.recipePreparationAvailable == true && window.createButton.isEnabled }
    check(window.snapshotAvailable, "Refresh recovers newly admitted recipe assets without reopening")
    let store = try ProjectActivityStore(root: root.appendingPathComponent("activity"))
    let before = try store.records().filter { $0.operation == "setup.inspect" }.count
    try Data().write(to: delay)
    window.useConfiguration("/synthetic/good.json")
    wait { FileManager.default.fileExists(atPath: started.path) }
    window.useConfiguration("/synthetic/bad.json")
    window.useConfiguration("/synthetic/good.json")
    wait { !window.switchingConfiguration }
    check(try store.records().filter { $0.operation == "setup.inspect" }.count >= before + 2, "A to B to A validates the final selection after reaping the cancelled first check")
    check(!window.statusLabel.stringValue.contains("Unable to check this configuration"), "final A is accepted rather than consuming a cancelled earlier A")
    defaults.set("/synthetic/bad.json", forKey: "PendingFirstRunConfiguration")
    let restored = ProjectWindowController(executable: cli.path, activityDirectory: root.appendingPathComponent("restored-activity"), defaults: defaults)
    restored.restoreConfiguration(override: nil)
    wait { restored.presentation.configPath == "/synthetic/good.json" && restored.snapshotAvailable }
    check(defaults.string(forKey: "PendingFirstRunConfiguration") == "/synthetic/bad.json", "invalid pending candidate remains inspectable without replacing usable prior selection")
    var restoredDrained = false
    restored.shutdownQueries { restoredDrained = true }
    wait { restoredDrained }
    var drained = false
    window.shutdownQueries { drained = true }
    wait { drained }
    print("Project onboarding tests passed")
  }
}
