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
    func project(_ state: String) -> ProjectRecord {
      ProjectRecord(name: "one", base: "base", sessionId: "session", backendObject: "backend", state: state, managementReady: false, diagnostic: "", observedState: state, backendRunning: state == "running",
        workspace: ProjectWorkspace(id: "workspace", filesystemUuid: "filesystem", sizeBytes: 64 << 20, mountPath: "/workspace", initialized: true),
        software: ProjectSoftware(intentDigest: "intent", status: "complete", actions: []), importState: ProjectImport(status: "not_imported", id: "", guestPath: ""), replacementPending: false, availableActions: ["open", "stop"])
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
    controller.recoveredActivities = []; controller.recoveredBusy = false
    controller.run(.export(name: "one", destination: "/synthetic/new-export"))
    check(controller.cancelButton.isEnabled, "first live export immediately enables explicit interruption")
    let end = Date().addingTimeInterval(4)
    while controller.presentation.busy && Date() < end { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
    check(!controller.presentation.busy, "synthetic command completed")
    controller.allowUnknownRetry = true
    controller.run(.open(name: "one"))
    check(!controller.cancelButton.isEnabled, "previous export cannot enable interruption of another operation")
    let secondEnd = Date().addingTimeInterval(4)
    while controller.presentation.busy && Date() < secondEnd { RunLoop.current.run(until: Date().addingTimeInterval(0.05)) }
    print("PASS: native activity selection and export interruption controls")
  }
}
