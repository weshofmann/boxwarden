// Render the real host strip offscreen; no VM or clipboard is accessed.
import Cocoa
import SwiftUI

@main struct StripTests {
  @MainActor static func main() {
    _ = NSApplication.shared
    let target = ClipboardTarget(cli: "/opt/boxwarden", config: "/opt/config", domain: "alpha",
      session: "clipboardprobe20260927r1", sessionID: "11111111-1111-4111-8111-111111111111",
      backendObject: "bw-probe", generation: "22222222-2222-4222-8222-222222222222")
    let controller = ClipboardWindowController(target: target, invoker: ClipboardProcessInvoker())
    let hosting = NSHostingView(rootView: ClipboardStrip(controller: controller))
    hosting.frame = NSRect(x: 0, y: 0, width: 1024, height: 100)
    hosting.layoutSubtreeIfNeeded()
    let height = hosting.fittingSize.height
    guard height > 0 && height <= 52 else {
      fputs("FAIL: one-row clipboard strip measured \(height) points\n", stderr)
      exit(1)
    }
    print("PASS: one-row clipboard strip fits within 52 points")
  }
}
