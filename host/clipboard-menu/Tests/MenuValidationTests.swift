import AppKit

private final class ActionTarget: NSObject {
  @objc func action(_ sender: NSMenuItem) {}
}

@main struct MenuValidationTests {
  static func main() {
    _ = NSApplication.shared
    let target = ActionTarget()
    let defaultMenu = NSMenu(title: "AppKit default")
    let defaultTransfer = NSMenuItem(title: "HOST -> GUEST", action: #selector(ActionTarget.action(_:)), keyEquivalent: "")
    defaultTransfer.target = target
    defaultTransfer.isEnabled = false
    defaultMenu.addItem(defaultTransfer)
    defaultMenu.update()
    guard defaultTransfer.isEnabled else { fputs("FAIL: AppKit validation precondition not reproduced\n", stderr); exit(1) }
    let menu = ClipboardMenuApp.makeMenu()
    let transfer = NSMenuItem(title: "HOST -> GUEST", action: #selector(ActionTarget.action(_:)), keyEquivalent: "")
    transfer.target = target
    transfer.isEnabled = false
    menu.addItem(transfer)
    menu.update()
    guard !transfer.isEnabled else { fputs("FAIL: AppKit re-enabled unavailable transfer\n", stderr); exit(1) }
    print("PASS: AppKit menu validation preserves disabled transfers")
  }
}
