import AppKit
import Foundation

private final class MenuChoice: NSObject {
  let target: MenuTarget
  init(_ target: MenuTarget) { self.target = target }
}

#if !MENU_TEST
@main
#endif
final class ClipboardMenuApp: NSObject, NSApplicationDelegate, NSMenuDelegate {
  private let state = MenuState()
  private var statusItem: NSStatusItem!
  private var menu: NSMenu!
  private var client: CLIClient!
  private var child: RunningProcess?
  private var configPath = ""
  private var quitting = false

  static func main() {
    let application = NSApplication.shared
    let delegate = ClipboardMenuApp()
    application.delegate = delegate
    application.setActivationPolicy(.accessory)
    application.run()
  }

  static func makeMenu() -> NSMenu {
    let menu = NSMenu(title: "Boxwarden Clipboard")
    menu.autoenablesItems = false
    return menu
  }

  func applicationDidFinishLaunching(_ notification: Notification) {
    let arguments = Array(ProcessInfo.processInfo.arguments.dropFirst())
    let defaultConfig = FileManager.default.homeDirectoryForCurrentUser
      .appendingPathComponent("Library/Application Support/boxwarden/config.json").path
    guard arguments.isEmpty || (arguments.count == 2 && arguments[0] == "--config") else {
      NSApp.terminate(nil); return
    }
    configPath = arguments.isEmpty ? defaultConfig : arguments[1]
    let cliPath = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/boxwarden").path
    guard CLIClient.validPath(configPath), CLIClient.validPath(cliPath) else { NSApp.terminate(nil); return }
    client = CLIClient(executable: cliPath, config: configPath)
    statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    statusItem.button?.title = "Boxwarden"
    menu = Self.makeMenu()
    menu.delegate = self
    statusItem.menu = menu
    render()
    refresh()
  }

  func menuNeedsUpdate(_ menu: NSMenu) {
    render()
    refresh()
  }

  private func refresh() {
    guard !quitting && !state.refreshing else { return }
    let id = state.beginRefresh()
    render()
    let client = self.client!
    let config = configPath
    DispatchQueue.global(qos: .userInitiated).async { [weak self] in
      do {
        let result = try client.discoverConfiguredDomains(from: Data(contentsOf: URL(fileURLWithPath: config)))
        DispatchQueue.main.async { [weak self] in
          self?.state.acceptRefresh(id, targets: result.targets, failedDomains: result.failedDomains)
          self?.render()
        }
      } catch {
        DispatchQueue.main.async { [weak self] in
          self?.state.failRefresh(id)
          self?.render()
        }
      }
    }
  }

  private func render() {
    guard let menu else { return }
    menu.removeAllItems()
    let heading = NSMenuItem(title: "Boxwarden", action: nil, keyEquivalent: "")
    heading.isEnabled = false
    menu.addItem(heading)
    let selection = state.selected.map { "\($0.domain) / \($0.session)" } ?? "None"
    let selector = NSMenuItem(title: "Sandbox: \(selection)", action: nil, keyEquivalent: "")
    let choices = NSMenu(title: "Sandboxes")
    for target in state.targets {
      let label = "\(target.domain) / \(target.session)" + (target.available ? "" : " (unavailable)")
      let item = NSMenuItem(title: label, action: #selector(selectSandbox(_:)), keyEquivalent: "")
      item.target = self
      item.representedObject = MenuChoice(target)
      item.state = state.selected?.key == target.key ? .on : .off
      choices.addItem(item)
    }
    if state.targets.isEmpty {
      let none = NSMenuItem(title: "No sandboxes found", action: nil, keyEquivalent: "")
      none.isEnabled = false
      choices.addItem(none)
    }
    selector.submenu = choices
    menu.addItem(selector)
    menu.addItem(.separator())
    let push = NSMenuItem(title: "HOST -> GUEST", action: #selector(pushClipboard(_:)), keyEquivalent: "")
    push.target = self; push.isEnabled = state.canTransfer
    menu.addItem(push)
    let pull = NSMenuItem(title: "GUEST -> HOST", action: #selector(pullClipboard(_:)), keyEquivalent: "")
    pull.target = self; pull.isEnabled = state.canTransfer
    menu.addItem(pull)
    menu.addItem(.separator())
    let explanation = NSMenuItem(title: "Nothing transfers automatically.", action: nil, keyEquivalent: "")
    explanation.isEnabled = false
    menu.addItem(explanation)
    let status = NSMenuItem(title: "Status: \(state.status)", action: nil, keyEquivalent: "")
    status.isEnabled = false
    menu.addItem(status)
    let reload = NSMenuItem(title: "Refresh Sandbox Status", action: #selector(refreshStatus(_:)), keyEquivalent: "")
    reload.target = self
    menu.addItem(reload)
    menu.addItem(.separator())
    let quit = NSMenuItem(title: "Quit Clipboard Utility", action: #selector(quit(_:)), keyEquivalent: "")
    quit.target = self
    menu.addItem(quit)
  }

  @objc private func selectSandbox(_ sender: NSMenuItem) {
    guard let choice = sender.representedObject as? MenuChoice else { return }
    state.select(choice.target)
    refresh()
  }
  @objc private func pushClipboard(_ sender: Any?) { start(.push) }
  @objc private func pullClipboard(_ sender: Any?) { start(.pull) }
  @objc private func refreshStatus(_ sender: Any?) { refresh() }

  private func start(_ direction: TransferDirection) {
    guard !quitting else { return }
    guard let request = state.beginTransfer(direction) else { return }
    render()
    child = client.transfer(request) { [weak self] result in
      DispatchQueue.main.async { [weak self] in
        self?.state.finishTransfer(request.id, result: result)
        self?.child = nil
        self?.render()
      }
    }
    if child == nil { state.finishTransfer(request.id, result: .failed); render() }
  }

  @objc private func quit(_ sender: Any?) {
    guard !quitting else { return }
    quitting = true
    client.shutdown {
      DispatchQueue.main.async { NSApp.terminate(nil) }
    }
  }
}
