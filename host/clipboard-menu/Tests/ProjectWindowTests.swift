import AppKit

@main struct ProjectWindowTests {
  static func main() throws {
    _ = NSApplication.shared
    func check(_ value: @autoclosure () -> Bool, _ message: String) {
      guard value() else { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    func activity(_ name: String, _ status: ProjectActivityStatus) -> ProjectActivity {
      ProjectActivity(id: UUID(), directory: URL(fileURLWithPath: "/private/tmp"), operation: "project.open", projectName: name, startedAt: Date(), status: status, message: "synthetic")
    }
    let controller = ProjectWindowController(executable: "/synthetic/boxwarden")
    let menu = NSMenu(title: "File")
    let newProject = NSMenuItem(title: "New Project", action: #selector(ProjectWindowController.createProject(_:)), keyEquivalent: "n")
    newProject.target = controller; menu.addItem(newProject); menu.update()
    check(!newProject.isEnabled, "native menu validation preserves missing-configuration refusal")
    controller.presentation.chooseConfiguration("/synthetic/config.json")
    let refresh = controller.presentation.beginRefresh()!
    controller.presentation.finishRefresh(refresh, names: ["one", "two"])
    func project(_ state: String, actions: [String] = ["open", "stop"]) -> ProjectRecord {
      ProjectRecord(name: "one", base: "base", sessionId: "session", backendObject: "backend", state: state, managementReady: false, diagnostic: "", observedState: state, backendRunning: state == "running",
        workspace: ProjectWorkspace(id: "workspace", filesystemUuid: "filesystem", sizeBytes: 64 << 20, mountPath: "/workspace", initialized: true),
        software: ProjectSoftware(intentDigest: "intent", status: "complete", actions: []), importState: ProjectImport(status: "not_imported", id: "", guestPath: ""), replacementPending: false, availableActions: actions)
    }
    controller.projects = [project("running")]
    check(!controller.canReplaceSelectedProject, "running project must be stopped before replacement")
    controller.projects = [project("unknown")]
    check(!controller.canReplaceSelectedProject, "unknown observation cannot enable replacement")
    controller.projects = [project("stopped")]
    check(controller.canReplaceSelectedProject, "exact stopped observation exposes confirmed replacement")
    let unknown = activity("one", .unknown), complete = activity("two", .succeeded), running = activity("one", .running)
    controller.recoveredActivities = [complete, unknown]
    check(controller.chooseVisibleActivity()?.id == unknown.id, "selected project exposes its older unknown receipt despite another project's success")
    controller.presentation.select("two")
    check(controller.chooseVisibleActivity()?.id == complete.id, "completed activity follows selected project")
    controller.recoveredActivities = [complete, running]
    check(controller.chooseVisibleActivity()?.id == running.id, "a live recovered operation remains visible and blocks effects")
    controller.acceptExport(ProjectExportReceipt(transaction: UUID().uuidString, phase: "aborted", published: "", projectFiles: ""))
    controller.render()
    check(controller.lastExport == nil && !controller.revealButton.isEnabled && controller.statusLabel.stringValue.contains("aborted"), "aborted export exposes its phase without offering an unrelated Finder directory")
    let root = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("bw-window-test-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
    defer { try? FileManager.default.removeItem(at: root) }
    let cli = root.appendingPathComponent("synthetic-cli")
    try Data("#!/bin/sh\n/bin/sleep 1\nexit 1\n".utf8).write(to: cli)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: cli.path)
    controller.client = try ProjectClient(executable: cli.path, config: "/synthetic/config.json", activityDirectory: root.appendingPathComponent("activity"))
    controller.recoveredActivities = []; controller.recoveredBusy = false; controller.snapshotAvailable = true
    controller.run(.export(name: "one", destination: "/synthetic/new-export"))
    check(controller.cancelButton.isEnabled, "first live export immediately enables explicit interruption")
    let end = Date().addingTimeInterval(4)
    while controller.presentation.busy && Date() < end { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
    check(!controller.presentation.busy, "synthetic command completed")
    controller.allowUnknownRetry = true; controller.snapshotAvailable = true
    controller.run(.open(name: "one"))
    check(controller.presentation.busy, "synthetic second operation actually started from refreshed inventory")
    check(!controller.cancelButton.isEnabled, "previous export cannot enable interruption of another operation")
    let secondEnd = Date().addingTimeInterval(4)
    while controller.presentation.busy && Date() < secondEnd { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
    let feedbackCLI = root.appendingPathComponent("feedback-cli")
    func feedbackScript(_ body: String) throws {
      try Data(("#!/bin/sh\n" + body).utf8).write(to: feedbackCLI)
      try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: feedbackCLI.path)
    }
    let failureEvent = #"{"version":1,"type":"error","operation":"project.list","message":"synthetic storage missing","data":{"uncertain":false}}"#
    let successEvent = #"{"version":1,"type":"result","operation":"project.list","data":{"setup":{"status":"ready","guidance":""},"projects":[]}}"#
    try feedbackScript("printf '%s\\n' '\(failureEvent)'\nexit 3\n")
    let defaultsName = "org.boxwarden.test.refresh-" + UUID().uuidString
    let feedbackDefaults = UserDefaults(suiteName: defaultsName)!
    defer { feedbackDefaults.removePersistentDomain(forName: defaultsName) }
    let feedback = ProjectWindowController(executable: feedbackCLI.path, activityDirectory: root.appendingPathComponent("feedback-activity"), defaults: feedbackDefaults)
    feedback.presentation.chooseConfiguration("/synthetic/broken.json")
    feedback.client = try ProjectClient(executable: feedbackCLI.path, config: "/synthetic/broken.json", activityDirectory: root.appendingPathComponent("feedback-activity"))
    func waitForFeedback() {
      let deadline = Date().addingTimeInterval(5)
      while feedback.presentation.refreshing && Date() < deadline { RunLoop.current.run(until: Date().addingTimeInterval(0.02)) }
      check(!feedback.presentation.refreshing, "synthetic feedback refresh completed")
    }
    feedback.refreshProjects(nil); waitForFeedback()
    let firstError = feedback.statusLabel.stringValue
    check(!feedback.snapshotAvailable && feedback.progressLines.count == 1, "first refresh failure visible once")
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.statusLabel.stringValue == firstError && feedback.progressLines.count == 1, "identical automatic refresh errors do not flood progress")
    try feedbackScript("printf '%s\\n' '\(successEvent)'\n")
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.snapshotAvailable && feedback.statusLabel.stringValue != firstError && feedback.progressLines.last?.contains("available") == true, "successful inventory clears current refresh error and reports recovery")
    feedback.report("Stopped synthetic project.")
    let outcomeLogCount = feedback.progressLines.count
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.statusLabel.stringValue == "Stopped synthetic project." && feedback.progressLines.count == outcomeLogCount, "routine successful refresh preserves operation outcome without new log noise")
    try feedbackScript("printf '%s\\n' '\(failureEvent)'\nexit 3\n")
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.progressLines.count == outcomeLogCount + 1, "same error after recovery represents a new outage")
    feedback.report("Explicit export completed.")
    try feedbackScript("printf '%s\\n' '\(successEvent)'\n")
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.statusLabel.stringValue == "Explicit export completed." && feedback.progressLines.last?.contains("available") == true, "recovery preserves a newer operation outcome while recording inventory recovery")
    try feedbackScript("printf '%s\\n' '\(failureEvent)'\nexit 3\n")
    feedback.refreshProjects(nil); waitForFeedback()
    check(feedback.statusLabel.stringValue == firstError, "real refresh failure present before switching configuration")
    try feedbackScript("printf '%s\\n' '\(successEvent)'\n")
    feedback.useConfiguration("/synthetic/valid.json")
    check(feedback.progressLines.isEmpty && feedback.progressText.string.isEmpty && !feedback.statusLabel.stringValue.contains("unavailable"), "config switch immediately clears obsolete errors and old progress")
    waitForFeedback()
    check(feedback.snapshotAvailable && feedback.statusLabel.stringValue.contains("refreshed") && feedback.progressLines.isEmpty, "valid config inventory replaces loading status without restoring old errors")
    check(feedbackDefaults.string(forKey: "SelectedConfiguration") == "/synthetic/valid.json", "real config activation remembers only the isolated selected config")
    let actions = ProjectWindowController(executable: feedbackCLI.path, activityDirectory: root.appendingPathComponent("actions-activity"), defaults: feedbackDefaults)
    actions.presentation.chooseConfiguration("/synthetic/actions.json")
    let initial = actions.presentation.beginRefresh()!
    actions.render()
    check(!actions.createButton.isEnabled && !actions.importButton.isEnabled && !actions.openButton.isEnabled, "initial refresh cannot enable effects without inventory")
    actions.presentation.finishRefresh(initial, names: ["one"])
    actions.projects = [project("stopped", actions: ["open", "stop", "import", "export"])]
    actions.setup = ProjectSetup(status: "ready", guidance: "")
    let background = actions.presentation.beginRefresh()!
    actions.render()
    check(!actions.createButton.isEnabled && !actions.importButton.isEnabled && !actions.openButton.isEnabled, "unavailable snapshot remains disabled despite old project fields")
    actions.snapshotAvailable = true; actions.render()
    check(actions.createButton.isEnabled && actions.importButton.isEnabled && actions.openButton.isEnabled && actions.stopButton.isEnabled && actions.replaceButton.isEnabled, "validated snapshot keeps create/import/lifecycle/replacement available during background refresh")
    menu.removeItem(newProject); newProject.target = actions; menu.addItem(newProject); menu.update()
    check(newProject.isEnabled, "native menu accepts validated snapshot during background refresh")
    actions.client = try ProjectClient(executable: feedbackCLI.path, config: "/synthetic/actions.json", activityDirectory: root.appendingPathComponent("actions-activity"))
    func dismissActionSheet() {
      if let sheet = actions.window?.attachedSheet { actions.window?.endSheet(sheet, returnCode: .cancel) }
      let deadline = Date().addingTimeInterval(2)
      while actions.window?.attachedSheet != nil && Date() < deadline { RunLoop.current.run(until: Date().addingTimeInterval(0.02)) }
      check(actions.window?.attachedSheet == nil, "synthetic action sheet dismissed")
    }
    actions.createProject(nil)
    check(actions.window?.attachedSheet != nil, "create handler opens its sheet during background refresh")
    dismissActionSheet()
    actions.importProject(nil)
    check(actions.sheetController is ImportProjectController && actions.window?.attachedSheet != nil, "import handler opens exact selected-project sheet during background refresh")
    actions.chooseConfiguration(nil)
    check(actions.sheetController is ImportProjectController, "configuration chooser cannot replace an active import sheet")
    dismissActionSheet()
    actions.replaceSystem(nil)
    check(actions.window?.attachedSheet != nil, "replacement handler opens its confirmation during background refresh")
    dismissActionSheet()
    actions.presentation.finishRefresh(background, names: [])
    actions.projects = []; actions.snapshotAvailable = false; actions.render()
    actions.createProject(nil); actions.importProject(nil)
    check(actions.window?.attachedSheet == nil && !actions.createButton.isEnabled && !actions.importButton.isEnabled, "missing inventory refuses effect handlers and controls")

    actions.run(.open(name: "one"))
    check(!actions.presentation.busy && actions.activity == nil, "direct lifecycle handler refuses effects without a validated snapshot")

    let raceCLI = root.appendingPathComponent("race-cli")
    let firstQuery = root.appendingPathComponent("first-query"), releaseQuery = root.appendingPathComponent("release-query"), queryEnded = root.appendingPathComponent("query-ended"), mutationCalls = root.appendingPathComponent("mutation-calls")
    let lateFailure = #"{"version":1,"type":"error","operation":"project.list","message":"late inventory failure","data":{"uncertain":false}}"#
    let effectFailure = #"{"version":1,"type":"error","operation":"project.open","message":"synthetic effect failed","data":{"uncertain":false}}"#
    let raceScript = """
    #!/bin/sh
    if [ "$6" = list ]; then
      if [ ! -f '\(firstQuery.path)' ]; then
        /usr/bin/touch '\(firstQuery.path)'
        while [ ! -f '\(releaseQuery.path)' ]; do /bin/sleep 0.02; done
        printf '%s\\n' '\(lateFailure)'
        /usr/bin/touch '\(queryEnded.path)'
        exit 3
      fi
      printf '%s\\n' '\(successEvent)'
    else
      printf '%s\\n' open >> '\(mutationCalls.path)'
      printf '%s\\n' '\(effectFailure)'
      exit 3
    fi
    """
    try Data(raceScript.utf8).write(to: raceCLI)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: raceCLI.path)
    let race = ProjectWindowController(executable: raceCLI.path, activityDirectory: root.appendingPathComponent("race-activity"), defaults: feedbackDefaults)
    race.presentation.chooseConfiguration("/synthetic/race.json")
    race.presentation.finishRefresh(race.presentation.beginRefresh()!, names: ["one"])
    race.projects = [project("stopped")]; race.snapshotAvailable = true
    race.client = try ProjectClient(executable: raceCLI.path, config: "/synthetic/race.json", activityDirectory: root.appendingPathComponent("race-activity"))
    func spinUntil(_ condition: () -> Bool) {
      let deadline = Date().addingTimeInterval(5)
      while !condition() && Date() < deadline { RunLoop.current.run(until: Date().addingTimeInterval(0.02)) }
      check(condition(), "controlled synthetic process reached expected checkpoint")
    }
    race.refreshProjects(nil); spinUntil { FileManager.default.fileExists(atPath: firstQuery.path) }
    race.run(.open(name: "one")); race.run(.open(name: "one"))
    spinUntil { !race.presentation.busy }
    try Data().write(to: releaseQuery)
    spinUntil { FileManager.default.fileExists(atPath: queryEnded.path) && !race.presentation.refreshing }
    RunLoop.current.run(until: Date().addingTimeInterval(0.2))
    check(race.snapshotAvailable && race.statusLabel.stringValue == "synthetic effect failed" && !race.progressLines.contains(where: { $0.contains("late inventory failure") }), "late background query cannot overwrite completed operation outcome or validated inventory")
    let recordedMutations = try String(contentsOf: mutationCalls, encoding: .utf8)
    check(recordedMutations == "open\n", "repeated effect click during refresh starts exactly one command")
    print("PASS: native activity controls, refresh feedback and background action availability")
  }
}
