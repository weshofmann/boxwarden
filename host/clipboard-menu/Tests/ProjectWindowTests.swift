import AppKit

@main struct ProjectWindowTests {
  static func main() throws {
    _ = NSApplication.shared
    func check(_ value: @autoclosure () throws -> Bool, _ message: String) {
      guard (try! value()) else { fputs("FAIL: \(message)\n", stderr); exit(1) }
    }
    func activity(_ name: String, _ status: ProjectActivityStatus) -> ProjectActivity {
      ProjectActivity(id: UUID(), directory: URL(fileURLWithPath: "/private/tmp"), operation: "project.open", projectName: name, startedAt: Date(), status: status, message: "synthetic")
    }
    let candidate = ProjectWindowController(executable: "/synthetic/boxwarden", networkPolicy: "n1candidate")
    check(candidate.limitations.stringValue.contains("N1 candidate") && candidate.limitations.stringValue.contains("not host-qualified") && !candidate.limitations.stringValue.contains("Stock networking"), "candidate is identified without claiming live qualification")
    let stock = ProjectWindowController(executable: "/synthetic/boxwarden", networkPolicy: "stock")
    check(stock.limitations.stringValue.contains("gateway services"), "stock still explains gateway exposure")
    let controller = ProjectWindowController(executable: "/synthetic/boxwarden", networkPolicy: "unknown")
    check(controller.limitations.stringValue.contains("Network policy unidentified"), "missing or unknown policy cannot imply containment")
    check(controller.window!.minSize.width <= 780 && controller.window!.minSize.height <= 540, "project window supports a smaller usable Mac window")
    check(controller.setupLabel.stringValue.contains("setup"), "fresh launch explains the setup step in ordinary language")
    func setupInspection(_ status: String, recipePreparationAvailable: Bool) throws -> SetupInspection {
      let json = """
      {"version":1,"scope":"alpha_project_setup","status":"\(status)","configPath":"/synthetic/config.json","configValid":true,"selectionAcceptable":true,"guidance":"Ready","nextActions":[],"recipePreparationAvailable":\(recipePreparationAvailable)}
      """
      return try JSONDecoder().decode(SetupInspection.self, from: Data(json.utf8))
    }
    controller.window!.setContentSize(NSSize(width: 760, height: 540))
    controller.setupInspection = try setupInspection("ready", recipePreparationAvailable: true)
    controller.render()
    controller.window!.contentView!.layoutSubtreeIfNeeded()
    let rootStack = controller.window!.contentView!.subviews.first as! NSStackView
    let setupActions = rootStack.arrangedSubviews[3]
    let body = rootStack.arrangedSubviews[4]
    let readyGap = controller.setupLabel.frame.minY - body.frame.maxY
    check(setupActions.isHidden && abs(readyGap - 8) < 1 && body.frame.height >= 235,
      "ready setup collapses its empty action row and keeps the project body usable at minimum window size")
    controller.setupInspection = try setupInspection("project_setup_missing", recipePreparationAvailable: true)
    controller.render()
    controller.window!.contentView!.layoutSubtreeIfNeeded()
    let missingGap = setupActions.frame.minY - body.frame.maxY
    check(!setupActions.isHidden && abs(missingGap - 8) < 1 && body.frame.height >= 235,
      "missing setup keeps preparation actions adjacent to a usable project body at minimum window size")
    controller.setupInspection = nil
    controller.window!.setContentSize(NSSize(width: 960, height: 660))
    controller.render()
    controller.window!.contentView!.layoutSubtreeIfNeeded()
    let menu = NSMenu(title: "File")
    let newProject = NSMenuItem(title: "New Project", action: #selector(ProjectWindowController.createProject(_:)), keyEquivalent: "n")
    newProject.target = controller; menu.addItem(newProject); menu.update()
    check(!newProject.isEnabled, "native menu validation preserves missing-configuration refusal")
    controller.presentation.chooseConfiguration("/synthetic/config.json")
    let refresh = controller.presentation.beginRefresh()!
    controller.presentation.finishRefresh(refresh, names: ["one", "two"])
    func project(_ state: String, actions: [String] = ["open", "stop"], imported: Bool = false) -> ProjectRecord {
      ProjectRecord(name: "one", base: "base", sessionId: "session", backendObject: "backend", state: state, managementReady: false, diagnostic: "", observedState: state, backendRunning: state == "running",
        workspace: ProjectWorkspace(id: "workspace", filesystemUuid: "filesystem", sizeBytes: 64 << 20, mountPath: "/workspace", initialized: true),
        software: ProjectSoftware(intentDigest: "intent", status: "complete", actions: []), importState: ProjectImport(status: imported ? "complete" : "not_imported", id: imported ? "transaction" : "", guestPath: imported ? "/workspace/import" : ""), replacementPending: false, availableActions: actions)
    }
    let layoutController = controller
    let originalLayoutState = (layoutController.window!.contentView!.frame.size, layoutController.setupInspection, layoutController.setup, layoutController.projects, layoutController.snapshotAvailable, layoutController.recoveredBusy, layoutController.statusLabel.stringValue, layoutController.presentation.selectedName)
    func descendants(of view: NSView) -> [NSView] {
      view.subviews.flatMap { [$0] + descendants(of: $0) }
    }
    func assertRightColumnContained(_ state: String, size: NSSize, inspection: SetupInspection?, selected: ProjectRecord?) {
      layoutController.window!.setContentSize(size)
      layoutController.setupInspection = inspection
      layoutController.setup = inspection?.status == "ready" ? ProjectSetup(status: "ready", guidance: "") : nil
      layoutController.projects = selected.map { [$0] } ?? []
      layoutController.snapshotAvailable = inspection != nil
      layoutController.recoveredBusy = state.contains("recovered")
      if layoutController.recoveredBusy { layoutController.statusLabel.stringValue = "preparation: qualifying" }
      layoutController.presentation.select(selected?.name)
      let content = layoutController.window!.contentView!
      let root = content.subviews.first as! NSStackView
      let body = root.arrangedSubviews[4] as! NSStackView
      let right = body.arrangedSubviews[1] as! NSStackView
      for _ in 0..<3 { layoutController.render(); content.layoutSubtreeIfNeeded() }
      if selected?.importState.status == "complete" {
        let guidance = layoutController.detail.stringValue.components(separatedBy: "\n").dropFirst().first ?? ""
        check(guidance.contains("export") && !guidance.localizedCaseInsensitiveContains("import"),
          "stopped completed-import guidance only describes its available export action")
      }
      if state.contains("recovered") {
        let recoveryActions = right.arrangedSubviews.last!
        check(recoveryActions.isHidden,
          "recovered busy state removes the empty retry-action row from the right column")
      }
      let views = [right] + descendants(of: right)
      let tolerance: CGFloat = 1
      for (index, view) in views.enumerated() {
        let bodyFrame = view.convert(view.bounds, to: body)
        let contentFrame = view.convert(view.bounds, to: content)
        check(body.bounds.insetBy(dx: -tolerance, dy: -tolerance).contains(bodyFrame),
          "\(state) right-column view \(index) frame \(NSStringFromRect(bodyFrame)) escapes body \(NSStringFromRect(body.bounds)) at \(Int(size.width))×\(Int(size.height))")
        check(content.bounds.insetBy(dx: -tolerance, dy: -tolerance).contains(contentFrame),
          "\(state) right-column view \(index) frame \(NSStringFromRect(contentFrame)) escapes content \(NSStringFromRect(content.bounds)) at \(Int(size.width))×\(Int(size.height))")
      }
    }
    layoutController.activity = ProjectActivity(id: UUID(), directory: URL(fileURLWithPath: "/private/tmp"), operation: "project.create", projectName: "synthetic-new-project", startedAt: Date(), status: .running, message: "preparation: installing")
    layoutController.recoveredBusy = true
    layoutController.render()
    check(layoutController.setupLabel.stringValue.contains("Creating synthetic-new-project") && !layoutController.setupLabel.stringValue.contains("Create a project"),
      "creation progress replaces the contradictory create-another-project guidance")
    layoutController.activity = nil; layoutController.recoveredBusy = false
    let readyInspection = try setupInspection("ready", recipePreparationAvailable: true)
    for size in [NSSize(width: 960, height: 660), NSSize(width: 760, height: 540)] {
      assertRightColumnContained("fresh", size: size, inspection: nil, selected: nil)
      assertRightColumnContained("ready-empty", size: size, inspection: readyInspection, selected: nil)
      assertRightColumnContained("ready-empty-recovered", size: size, inspection: readyInspection, selected: nil)
      assertRightColumnContained("selected", size: size, inspection: readyInspection,
        selected: project("stopped", actions: ["open", "stop", "export"], imported: true))
    }
    layoutController.setupInspection = originalLayoutState.1
    layoutController.setup = originalLayoutState.2
    layoutController.projects = originalLayoutState.3
    layoutController.snapshotAvailable = originalLayoutState.4
    layoutController.recoveredBusy = originalLayoutState.5
    layoutController.statusLabel.stringValue = originalLayoutState.6
    layoutController.presentation.select(originalLayoutState.7)
    layoutController.window!.setContentSize(originalLayoutState.0)
    layoutController.render()
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
    let inspectScript = """
    if [ "$3" = setup ] && [ "$4" = inspect ]; then
      printf '{"version":1,"scope":"alpha_project_setup","status":"ready","config_path":"%s","config_valid":true,"selection_acceptable":true,"guidance":"Ready","next_actions":[],"recipe_preparation_available":true}\\n' "$2"
      exit 0
    fi
    """
    func feedbackScript(_ body: String) throws {
      try Data(("#!/bin/sh\n" + inspectScript + "\n" + body).utf8).write(to: feedbackCLI)
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
      while (feedback.switchingConfiguration || feedback.client?.config != feedback.presentation.configPath || feedback.presentation.refreshing) && Date() < deadline { RunLoop.current.run(until: Date().addingTimeInterval(0.02)) }
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
    spinUntil { !race.presentation.refreshing && (try? race.client?.recoverActivities().contains(where: { $0.operation == "project.list" && $0.status == .running })) == false }
    RunLoop.current.run(until: Date().addingTimeInterval(0.2))
    check(race.snapshotAvailable && race.statusLabel.stringValue == "synthetic effect failed" && !race.progressLines.contains(where: { $0.contains("late inventory failure") }), "late background query cannot overwrite completed operation outcome or validated inventory")
    let recordedMutations = try String(contentsOf: mutationCalls, encoding: .utf8)
    check(recordedMutations == "open\n", "repeated effect click during refresh starts exactly one command")
    let switchCLI = root.appendingPathComponent("switch-cli"), switchStarted = root.appendingPathComponent("switch-started")
    let switchScript = """
    #!/bin/sh
    \(inspectScript)
    if [ "$2" = /synthetic/old.json ]; then
      /usr/bin/touch '\(switchStarted.path)'
      trap '' TERM
      exec /bin/sleep 2
    fi
    if [ "$6" = targets ]; then printf '%s\\n' '{"targets":[]}'; exit 0; fi
    printf '%s\\n' '\(successEvent)'
    """
    try Data(switchScript.utf8).write(to: switchCLI)
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: switchCLI.path)
    let switching = ProjectWindowController(executable: switchCLI.path, activityDirectory: root.appendingPathComponent("switch-activity"), defaults: feedbackDefaults)
    switching.useConfiguration("/synthetic/old.json")
    spinUntil { FileManager.default.fileExists(atPath: switchStarted.path) }
    let oldClient = switching.client!
    for index in 0..<25 { switching.useConfiguration("/synthetic/new-\(index).json") }
    spinUntil { switching.snapshotAvailable }
    let oldStillRunning = try oldClient.recoverActivities().contains { $0.status == .running }
    // Let a red-run fixture naturally end before leaving disposable storage.
    if oldStillRunning { RunLoop.current.run(until: Date().addingTimeInterval(2.2)) }
    check(!oldStillRunning, "configuration replacement waits for old query reap before latest inventory starts")
    check(switching.client?.config == "/synthetic/new-24.json", "rapid configuration choices activate only the latest desired path")
    let switchStore = try ProjectActivityStore(root: root.appendingPathComponent("switch-activity"))
    check(try switchStore.records().filter { $0.operation != "setup.inspect" }.allSatisfy { ["/synthetic/old.json", "/synthetic/new-24.json"].contains($0.config) }, "coalescing never starts intermediate configuration queries")
    try FileManager.default.removeItem(at: switchStarted)
    switching.useConfiguration("/synthetic/old.json")
    spinUntil { FileManager.default.fileExists(atPath: switchStarted.path) }
    let retiring = switching.client!
    switching.refreshTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { _ in switching.refreshProjects(nil) }
    switching.activityTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { _ in switching.pollActivity() }
    switching.useConfiguration("/synthetic/should-not-start.json")
    var quitDrained = false
    switching.shutdownQueries { quitDrained = true }
    for _ in 0..<10 { switching.refreshProjects(nil); switching.pollActivity(); switching.useConfiguration("/synthetic/also-refused.json") }
    check(switching.closing && switching.refreshTimer?.isValid == false && switching.activityTimer?.isValid == false && !switching.chooseButton.isEnabled, "quit invalidates timers and disables admission while drain stays asynchronous")
    spinUntil { quitDrained }
    check(switching.client == nil && !switching.snapshotAvailable, "quit during configuration retirement cannot activate a replacement or stale snapshot")
    check(try retiring.recoverActivities().allSatisfy { $0.status != .running }, "quit completion follows owned query reap")
    check(try switchStore.records().allSatisfy { !["/synthetic/should-not-start.json", "/synthetic/also-refused.json"].contains($0.config) }, "timer and config work cannot restart query children during quit")
    print("PASS: native activity controls, refresh feedback, config coalescing and asynchronous query shutdown")
  }
}
