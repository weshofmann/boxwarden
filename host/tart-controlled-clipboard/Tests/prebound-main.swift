// The viewer must reuse the display object bound before VM start. A newly
// created NSView at window appearance would miss that early binding.
import Cocoa
import SwiftUI
import Virtualization

@main struct PreboundViewTests {
  @MainActor static func main() {
    _ = NSApplication.shared
    let prepared = VZVirtualMachineView()
    let hosting = NSHostingView(rootView: PreparedVirtualMachineView(view: prepared))
    hosting.frame = NSRect(x: 0, y: 0, width: 1024, height: 768)
    hosting.layoutSubtreeIfNeeded()

    func contains(_ root: NSView, target: NSView) -> Bool {
      root === target || root.subviews.contains { contains($0, target: target) }
    }
    guard contains(hosting, target: prepared) else {
      fputs("FAIL: hosted VM display replaced the prebound view\n", stderr)
      exit(1)
    }
    print("PASS: hosted VM display reuses the prebound view")
  }
}
