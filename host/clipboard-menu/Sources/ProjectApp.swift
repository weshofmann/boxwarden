import AppKit

#if !PROJECT_APP_TEST
@main
#endif
final class ProjectManagerApp: NSObject, NSApplicationDelegate {
  private var controller: ProjectWindowController!
  private var statusItem: NSStatusItem!
  private var quitting = false
  static func main() {
    let app = NSApplication.shared
    let delegate = ProjectManagerApp()
    app.delegate = delegate
    app.setActivationPolicy(.regular)
    app.run()
  }
  func applicationDidFinishLaunching(_ notification: Notification) {
    let args = Array(ProcessInfo.processInfo.arguments.dropFirst())
    var config: String?
    var privateBoard: String?
    var index = 0
    while index < args.count {
      guard index + 1 < args.count else { NSApp.terminate(nil); return }
      switch args[index] {
      case "--config": config = args[index + 1]
      case "--private-pasteboard":
        let name = args[index + 1]
        guard CLIClient.validPrivatePasteboard(name) else { NSApp.terminate(nil); return }
        privateBoard = name
      default: NSApp.terminate(nil); return
      }
      index += 2
    }
    let cli = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/boxwarden").path
    let bundleID = Bundle.main.bundleIdentifier ?? "org.boxwarden.project-manager"
    let activityDirectory = FileManager.default.homeDirectoryForCurrentUser
      .appendingPathComponent("Library/Application Support/" + bundleID + "/CommandActivity")
    controller = ProjectWindowController(executable: cli, privatePasteboard: privateBoard, activityDirectory: activityDirectory)
    makeMenus()
    controller.showWindow(nil)
    NSApp.activate(ignoringOtherApps: true)
    controller.restoreConfiguration(override: config)
  }
  private func makeMenus() {
    let main = NSMenu()
    let appItem = NSMenuItem(); main.addItem(appItem)
    let appMenu = NSMenu(title: "Boxwarden")
    appMenu.addItem(withTitle: "About Boxwarden", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
    appMenu.addItem(.separator())
    appMenu.addItem(withTitle: "Quit Boxwarden", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
    appItem.submenu = appMenu
    let fileItem = NSMenuItem(); main.addItem(fileItem)
    let file = NSMenu(title: "File")
    let choose = file.addItem(withTitle: "Choose Configuration…", action: #selector(ProjectWindowController.chooseConfiguration(_:)), keyEquivalent: "o"); choose.target = controller
    let create = file.addItem(withTitle: "New Project…", action: #selector(ProjectWindowController.createProject(_:)), keyEquivalent: "n"); create.target = controller
    let refresh = file.addItem(withTitle: "Refresh", action: #selector(ProjectWindowController.refreshProjects(_:)), keyEquivalent: "r"); refresh.target = controller
    fileItem.submenu = file
    let editItem = NSMenuItem(); main.addItem(editItem)
    let edit = NSMenu(title: "Edit")
    edit.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
    edit.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
    edit.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
    edit.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
    edit.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
    editItem.submenu = edit
    NSApp.mainMenu = main
    statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    statusItem.button?.title = "Boxwarden"
    let menu = NSMenu()
    let show = menu.addItem(withTitle: "Show Projects", action: #selector(showProjects(_:)), keyEquivalent: ""); show.target = self
    menu.addItem(withTitle: "Quit Boxwarden", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "")
    statusItem.menu = menu
  }
  @objc private func showProjects(_ sender: Any?) { controller.showWindow(nil); NSApp.activate(ignoringOtherApps: true) }
  func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool { showProjects(nil); return true }
  func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
    guard !quitting else { return .terminateLater }
    if controller?.hasActiveOperation == true {
      let alert = NSAlert()
      alert.messageText = "Quit while work continues?"
      alert.informativeText = "Guests keep running. Project commands continue independently; reopen to rediscover their progress and state. An unfinished clipboard transfer remains an unknown outcome to review, without retaining its text. Quitting does not cancel the request."
      alert.addButton(withTitle: "Quit Boxwarden"); alert.addButton(withTitle: "Keep Open")
      if alert.runModal() != .alertFirstButtonReturn { return .terminateCancel }
    }
    guard let controller else { return .terminateNow }
    quitting = true
    controller.shutdownQueries { sender.reply(toApplicationShouldTerminate: true) }
    return .terminateLater
  }
}
